import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/state/errors.dart';
import 'package:coreshift/state/leak.dart';
import 'package:coreshift/version.dart';

void main() {
  Future<AppState> pumpApp(WidgetTester tester, {Size size = const Size(1400, 900), AppState? custom}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = custom ?? AppState(DemoBackend());
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    return state;
  }

  const pages = ['Серверы', 'Исключения', 'Настройки', 'Ядра', 'Журнал', 'Главная'];

  testWidgets('every page renders on demo data', (tester) async {
    final state = await pumpApp(tester);
    expect(state.loaded, isTrue);
    expect(find.text('Отключено'), findsWidgets);
    expect(find.text('Amsterdam'), findsOneWidget);

    for (final page in pages) {
      await tester.tap(find.text(page).first);
      await tester.pump();
      expect(tester.takeException(), isNull, reason: page);
    }
    await tester.tap(find.text('Серверы').first);
    await tester.pump();
    expect(find.text('NorthLink Premium'), findsWidgets);
    expect(find.text('Helsinki'), findsOneWidget);
    // The selected server offers to connect without hovering.
    expect(find.text('Подключить'), findsOneWidget);
    expect(state.latencyAutoTested, isTrue);
    // Let the latency test the page started finish.
    await tester.pump(const Duration(seconds: 30));
  });

  testWidgets('every page fits the smallest window', (tester) async {
    // The window's minimum size less the title bar.
    await pumpApp(tester, size: const Size(960, 606));
    for (final page in pages) {
      await tester.tap(find.text(page).first);
      await tester.pump();
      expect(tester.takeException(), isNull, reason: page);
    }
    await tester.pump(const Duration(seconds: 30));
  });

  testWidgets('connects the selected node', (tester) async {
    final state = await pumpApp(tester);
    await tester.runAsync(() => state.connect());
    await tester.pump();
    expect(state.status.core, 'xray');
    expect(find.text('Подключено'), findsWidgets);
    expect(find.text('Xray-core'), findsWidgets);
    await tester.runAsync(() => state.disconnect());
    await tester.pump();
    expect(find.text('Отключено'), findsWidgets);
  });

  testWidgets('without subscriptions the home page explains the first steps', (tester) async {
    final state = await pumpApp(tester);
    await tester.runAsync(() async {
      for (final s in state.subscriptions.toList()) {
        await state.removeSubscription(s.id);
      }
    });
    await tester.pump();
    expect(find.text('Добро пожаловать в CoreShift'), findsOneWidget);
    expect(find.text('Добавить подписку'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  test('daemon errors read as Russian', () {
    expect(humanError('the panel sent a message instead of servers: Приложение не поддерживается'), contains('«Приложение не поддерживается»'));
    expect(humanError('fetch subscription: server returned 404 Not Found'), contains('не найдена'));
    expect(humanError('fetch subscription: Get "https://x/sub/…": dial tcp: lookup x: no such host'), contains('адрес панели'));
    expect(humanError('this subscription is already added'), 'Эта подписка уже добавлена.');
    expect(
      humanError(
        'every compatible core failed: xray: listen tcp 127.0.0.1:17890: bind: An attempt was made to access a socket in a way forbidden by its access permissions.',
      ),
      contains('Порт 17890'),
    );
    expect(humanError('every compatible core failed: sing-box: listen address 127.0.0.1:17890 is already in use'), contains('занят'));
    expect(humanError('something new'), 'something new');
  });

  testWidgets('speed graph fills while connected', (tester) async {
    final state = await pumpApp(tester);
    await tester.runAsync(() async {
      await state.connect();
      await Future.delayed(const Duration(milliseconds: 3300));
    });
    await tester.pump();
    expect(state.speed.length, greaterThanOrEqualTo(2));
    expect(state.sessionDown, greaterThan(0));
    expect(find.text('Скорость'), findsOneWidget);
    expect(find.byTooltip('Скачано и отправлено за это подключение'), findsOneWidget);
    expect(find.textContaining('бит/с'), findsWidgets);
    expect(tester.takeException(), isNull);
    await tester.runAsync(() => state.disconnect());
    await tester.pump();
    expect(state.speed, isEmpty);
    expect(find.byTooltip('Скачано и отправлено за это подключение'), findsNothing);
  });

  testWidgets('returns to the primary core on request', (tester) async {
    final state = await pumpApp(tester);
    final alerts = <Alert>[];
    state.alerts.listen(alerts.add);
    await tester.runAsync(() async {
      await state.connect();
      // The demo's first core "crashes" after 25 s.
      for (var i = 0; i < 300 && state.status.core == 'xray'; i++) {
        await Future.delayed(const Duration(milliseconds: 100));
      }
    });
    await tester.pump();
    expect(state.status.core, 'sing-box');
    expect(alerts.single.title, 'CoreShift сменил ядро');
    expect(alerts.single.body, contains('процесс завершился'));
    final back = find.textContaining('Вернуть ');
    expect(back, findsOneWidget);

    await tester.runAsync(() async {
      await state.returnToPrimary();
      // The swap arrives as an event, after the response.
      for (var i = 0; i < 40 && alerts.length < 2; i++) {
        await Future.delayed(const Duration(milliseconds: 50));
      }
    });
    await tester.pump();
    expect(state.status.core, 'xray');
    expect(alerts.last.title, 'Основное ядро снова работает');
    expect(find.textContaining('Вернуть '), findsNothing);
    await tester.runAsync(() => state.disconnect());
  }, timeout: const Timeout(Duration(minutes: 1)));

  testWidgets('core versions and updates', (tester) async {
    final state = await pumpApp(tester);
    await tester.tap(find.text('Ядра').first);
    await tester.pump();
    expect(find.text('версия 1.14.2'), findsOneWidget);
    await tester.runAsync(() => state.checkCoreUpdates());
    await tester.pump();
    expect(find.text('Доступна 1.14.3 · 33 МБ'), findsOneWidget);
    expect(find.text('Последняя версия'), findsNWidgets(2));
    await tester.runAsync(() => state.updateCore('sing-box'));
    await tester.pump();
    expect(state.info.versionOf('sing-box'), '1.14.3');
    expect(find.text('версия 1.14.3'), findsOneWidget);
    expect(find.textContaining('Доступна'), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('apps picked from the running ones bypass the tunnel', (tester) async {
    final state = await pumpApp(tester);
    await tester.tap(find.text('Исключения').first);
    await tester.pump();
    expect(find.text('Программы без VPN'), findsOneWidget);
    expect(find.text('qbittorrent.exe'), findsOneWidget);
    await tester.tap(find.text('Выбрать из запущенных'));
    // The dialog opens, then loads the list on the test's clock.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    final dialog = find.byType(Dialog);
    expect(find.descendant(of: dialog, matching: find.text('steam.exe')), findsOneWidget);
    // qBittorrent is on the list already.
    expect(find.descendant(of: dialog, matching: find.text('без VPN')), findsOneWidget);
    final steamRow = find
        .ancestor(
          of: find.descendant(of: dialog, matching: find.text('steam.exe')),
          matching: find.byType(Row),
        )
        .first;
    await tester.tap(find.descendant(of: steamRow, matching: find.text('Добавить')));
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump();
    expect(state.setting<List>('routing.direct_apps', const []), ['qbittorrent.exe', 'steam.exe']);
    expect(find.descendant(of: dialog, matching: find.text('без VPN')), findsNWidgets(2));
    await tester.tap(find.text('Готово'));
    await tester.pump();
    await tester.pump(const Duration(seconds: 1));

    // Typed by hand: a path is cut to the program's name.
    await tester.enterText(find.widgetWithText(TextField, 'steam.exe'), r'C:\Games\Game.exe');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump();
    expect(state.setting<List>('routing.direct_apps', const []), ['qbittorrent.exe', 'steam.exe', 'Game.exe']);
    expect(find.text('Game.exe'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('DNS leak check', (tester) async {
    final state = await pumpApp(tester);
    await tester.tap(find.text('Настройки').first);
    await tester.pump();
    expect(find.text('Подключитесь в режиме «Все приложения», чтобы проверить'), findsOneWidget);
    await tester.runAsync(() async {
      await state.connect();
      await state.runLeakTest();
    });
    await tester.pump();
    expect(state.leakReport?.leaks, isFalse);
    expect(find.text('Утечки нет'), findsOneWidget);
    expect(find.textContaining('Netherlands'), findsWidgets);
    expect(tester.takeException(), isNull);
    await tester.runAsync(() => state.disconnect());
  });

  test('leak report flags the ISP resolver', () {
    Map<String, dynamic> e(String type, String ip, String cc, String org) => {'type': type, 'ip': ip, 'country': cc, 'country_name': cc, 'asn': org};
    final ok = LeakReport.fromEntries([
      e('ip', '203.0.113.1', 'de', 'AS1 Hosting'),
      e('dns', '198.51.100.1', 'de', 'AS1 Hosting'),
      e('dns', '172.253.0.1', 'us', 'AS15169 Google LLC'),
      e('dns', '172.253.0.1', 'us', 'AS15169 Google LLC'),
    ]);
    expect(ok.leaks, isFalse);
    expect(ok.dns.length, 2);
    final leak = LeakReport.fromEntries([
      e('ip', '203.0.113.1', 'de', 'AS1 Hosting'),
      e('dns', '77.88.8.8', 'ru', 'AS13238 YANDEX LLC'),
      e('dns', '162.158.1.1', 'de', 'AS13335 CloudFlare Inc'),
    ]);
    expect(leak.leaks, isTrue);
    expect(leak.suspicious.single.ip, '77.88.8.8');
  });

  test('new daemon errors read as Russian', () {
    expect(humanError('already on the primary core'), 'Уже работает основное ядро.');
    expect(humanError('sing-box: download: checksum mismatch'), contains('контрольная сумма'));
    expect(humanError('xray: check for updates: GitHub answered 403 Forbidden'), contains('ограничил'));
    expect(humanError('mihomo: the new version does not start, kept the old one: exit status 1'), contains('прежняя'));
    expect(AppState.reasonText('health-check'), 'нет связи');
    expect(AppState.reasonText('return-to-primary'), 'возврат к основному');
  });

  test('notification preference is kept', () {
    Map<String, dynamic>? saved;
    final state = AppState(DemoBackend(), prefs: {}, savePrefs: (p) async => saved = Map.of(p));
    expect(state.systemNotifications, isTrue);
    state.setPref('notifications', false);
    expect(state.systemNotifications, isFalse);
    expect(saved, {'notifications': false});
  });

  testWidgets('new panels fit the smallest window', (tester) async {
    final state = await pumpApp(tester, size: const Size(960, 606));
    await tester.runAsync(() async {
      await state.connect();
      await state.checkCoreUpdates();
      await state.runLeakTest();
      await Future.delayed(const Duration(milliseconds: 2300));
    });
    await tester.pump();
    for (final page in pages) {
      await tester.tap(find.text(page).first);
      await tester.pump();
      expect(tester.takeException(), isNull, reason: page);
    }
    await tester.tap(find.text('Исключения').first);
    await tester.pump();
    await tester.ensureVisible(find.text('Выбрать из запущенных'));
    await tester.tap(find.text('Выбрать из запущенных'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    expect(find.text('Telegram.exe'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.tap(find.text('Готово'));
    await tester.pump(const Duration(seconds: 6));
    await tester.runAsync(() => state.disconnect());
  });

  testWidgets('own lists: addresses, always through the VPN, blocked', (tester) async {
    final state = await pumpApp(tester);
    await tester.tap(find.text('Исключения').first);
    await tester.pump();
    List<String> list(String key) => state.setting<List>('routing.$key', const []).cast<String>();
    Future<void> add(String hint, String text) async {
      final field = find.widgetWithText(TextField, hint);
      await tester.ensureVisible(field);
      await tester.enterText(field, text);
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pump(const Duration(milliseconds: 300));
      await tester.pump();
    }

    await add('gosuslugi.ru, 10.8.0.0/16', 'bank.example 10.8.0.0/16, 2001:db8::/32');
    expect(list('direct_domains'), ['bank.example']);
    expect(list('direct_ips'), ['10.8.0.0/16', '2001:db8::/32']);
    expect(find.text('10.8.0.0/16'), findsOneWidget);

    await add('blocked.ru', 'blocked.ru');
    expect(list('proxy_domains'), ['blocked.ru']);
    await add('ads.example', 'ads.example');
    expect(list('block_domains'), ['ads.example']);

    // A mistake is explained in Russian and nothing is saved.
    await add('gosuslugi.ru, 10.8.0.0/16', '198.18.0.1');
    expect(find.textContaining('служебными адресами VPN'), findsWidgets);
    expect(list('direct_ips'), ['10.8.0.0/16', '2001:db8::/32']);

    // Removing an address keeps the sites.
    await tester.tap(
      find.descendant(
        of: find.ancestor(of: find.text('2001:db8::/32'), matching: find.byType(Row)).first,
        matching: find.byIcon(Icons.close),
      ),
    );
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump();
    expect(list('direct_ips'), ['10.8.0.0/16']);
    expect(list('direct_domains'), ['bank.example']);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('only selected services go through the VPN', (tester) async {
    final state = await pumpApp(tester);
    await tester.tap(find.text('Исключения').first);
    await tester.pump();
    List<String> list(String key) => state.setting<List>('routing.$key', const []).cast<String>();
    expect(find.text('Популярные сервисы'), findsNothing);

    await tester.tap(find.text('Только выбранное'));
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump();
    expect(state.setting('routing.mode', ''), 'selected');
    expect(find.text('Популярные сервисы'), findsOneWidget);
    // The direct lists have nothing to do in this mode.
    expect(find.text('Российские сайты напрямую'), findsNothing);
    expect(find.text('Программы без VPN'), findsNothing);

    await tester.tap(find.text('Telegram'));
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump();
    expect(list('proxy_domains'), contains('telegram.org'));
    expect(list('proxy_ips'), containsAll(['91.108.4.0/22', '149.154.160.0/20', '2001:67c:4e8::/48']));
    await tester.tap(find.text('Discord'));
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump();
    expect(list('proxy_apps'), ['Discord.exe']);
    expect(find.text('Discord.exe'), findsOneWidget);

    final field = find.widgetWithText(TextField, 'youtube.com, 91.108.4.0/22');
    await tester.enterText(field, 'example.com');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump();
    expect(list('proxy_domains'), contains('example.com'));

    // Turning a service off takes out its entries only.
    await tester.tap(find.text('Telegram'));
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump();
    expect(list('proxy_domains'), isNot(contains('telegram.org')));
    expect(list('proxy_domains'), contains('example.com'));
    expect(list('proxy_ips'), isEmpty);
    expect(list('proxy_apps'), ['Discord.exe']);

    await tester.tap(find.text('Всё через VPN'));
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pump();
    expect(state.setting('routing.mode', ''), 'all');
    expect(find.text('Российские сайты напрямую'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 6));
  });

  test('settings errors read as Russian', () {
    expect(
      humanError('routing.direct_domains: "bad domain" is not a domain\nrouting.direct_ips: "300.1.1.1" is not an address or subnet'),
      '«bad domain» — не похоже на адрес сайта.\n«300.1.1.1» — не похоже на IP-адрес или подсеть.',
    );
    expect(humanError('routing.proxy_ips: at most 1000 entries'), 'В списке может быть не больше 1000 записей.');
  });

  test('versions order by number, then by build', () {
    const v = BuildVersion('0.2.0', 14, 'abc1234');
    expect(v.compareTo(const BuildVersion('0.1.0')), greaterThan(0));
    expect(v.compareTo(const BuildVersion('0.10.0', 1)), lessThan(0));
    expect(v.compareTo(const BuildVersion('0.2.0', 15)), lessThan(0));
    expect(v.compareTo(const BuildVersion('0.2.0', 14, 'other')), 0);
    expect(BuildVersion.parseKey(v.key).same(v), isTrue);
    expect(v.label, '0.2.0 (сборка 14)');
    expect(const BuildVersion('dev').known, isFalse);
  });

  testWidgets('says when the version changed since the last run', (tester) async {
    for (final (prev, want) in [
      ('0.1.0+0+', 'CoreShift обновлён: 0.1.0 → 0.2.0 (сборка 14)'),
      ('0.2.1+20+ffff000', 'Установлена более ранняя версия CoreShift: 0.2.1 (сборка 20) → 0.2.0 (сборка 14)'),
      ('0.2.0+14+0000aaa-dirty', 'CoreShift пересобран: 0.2.0 (сборка 14) → 0.2.0 (сборка 14)'),
    ]) {
      final prefs = <String, dynamic>{'last_version': prev};
      final state = AppState(DemoBackend(), prefs: prefs, version: const BuildVersion('0.2.0', 14, 'abc1234'));
      await pumpApp(tester, custom: state);
      expect(state.updateNotice, want);
      expect(prefs['last_version'], '0.2.0+14+abc1234');
      expect(find.text(want), findsWidgets);
      expect(state.logs.any((l) => l.message == want), isTrue);
      await tester.pump(const Duration(seconds: 6));
    }

    // The same version again: nothing to say.
    final state = AppState(DemoBackend(), prefs: {'last_version': '0.2.0+14+abc1234'}, version: const BuildVersion('0.2.0', 14, 'abc1234'));
    await pumpApp(tester, custom: state);
    expect(state.updateNotice, isEmpty);
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('warns when the service is another version', (tester) async {
    // The demo service reports 0.2.0 without a build.
    final state = AppState(DemoBackend(), version: const BuildVersion('0.2.0', 14, 'abc1234'));
    await pumpApp(tester, custom: state);
    expect(state.versionMismatch, isTrue);
    await tester.tap(find.text('Настройки').first);
    await tester.pump();
    await tester.ensureVisible(find.text('Версия службы'));
    expect(find.text('0.2.0 (сборка 14)'), findsOneWidget);
    expect(find.textContaining('Переустановите CoreShift целиком'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 6));
  });
}
