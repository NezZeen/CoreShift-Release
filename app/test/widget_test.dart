import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/backend.dart';
import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/state/errors.dart';
import 'package:coreshift/state/leak.dart';
import 'package:coreshift/ui/countries.dart';
import 'package:coreshift/ui/pages/android_apps.dart';
import 'package:coreshift/ui/qr.dart';
import 'package:coreshift/ui/support.dart';
import 'package:coreshift/ui/theme.dart';
import 'package:coreshift/ui/widgets.dart';
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

  const pages = ['Серверы', 'Правила', 'Настройки', 'Ядра', 'Журнал', 'Главная'];

  /// Opens [page]: a main page from the navigation, the cores from their
  /// card in the settings.
  Future<void> open(WidgetTester tester, String page) async {
    final phone = find.byType(NavigationBar).evaluate().isNotEmpty;
    Finder nav(String p) => phone ? find.descendant(of: find.byType(NavigationBar), matching: find.text(p)) : find.text(p).first;
    final from = page == 'Ядра' ? 'Настройки' : null;
    if (from == null) {
      await tester.tap(nav(page));
      return;
    }
    await tester.tap(nav(from));
    await tester.pump();
    await tester.ensureVisible(find.text(page).last);
    await tester.tap(find.text(page).last);
  }

  testWidgets('every page renders on demo data', (tester) async {
    final state = await pumpApp(tester);
    expect(state.loaded, isTrue);
    expect(find.text('Отключено'), findsWidgets);
    expect(find.text('Amsterdam'), findsWidgets);

    for (final page in pages) {
      await open(tester, page);
      await tester.pump();
      expect(tester.takeException(), isNull, reason: page);
      expect(
        find.descendant(of: find.byType(PageHeader), matching: find.text(page)),
        page == 'Главная' ? findsNothing : findsOneWidget,
        reason: page,
      );
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
      await open(tester, page);
      await tester.pump();
      expect(tester.takeException(), isNull, reason: page);
      expect(
        find.descendant(of: find.byType(PageHeader), matching: find.text(page)),
        page == 'Главная' ? findsNothing : findsOneWidget,
        reason: page,
      );
    }
    await tester.pump(const Duration(seconds: 30));
  });

  testWidgets('connects the selected node', (tester) async {
    final state = await pumpApp(tester);
    await tester.runAsync(() => state.connect());
    await tester.pump();
    expect(state.status.core, 'xray');
    expect(find.text('Подключено'), findsWidgets);
    await tester.runAsync(() => state.disconnect());
    await tester.pump();
    expect(find.text('Отключено'), findsWidgets);
    // The home page's traffic history is on its way.
    await tester.pump(const Duration(seconds: 1));
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

    // The first subscription picks its first server: the button works at once.
    await tester.runAsync(() => state.addSubscription(source: 'vless://id@203.0.113.9:443?security=tls#Pasted'));
    await tester.pump();
    final sub = state.subscriptions.single;
    expect(sub.displayName, 'Мои серверы');
    expect(state.selection.subscription, sub.id);
    expect(find.text('Нажмите, чтобы подключиться'), findsOneWidget);

    // A second pasted server joins the same list rather than a second
    // "Мои серверы"; pasting it again says so.
    await tester.runAsync(() => state.addSubscription(source: 'trojan://pw@203.0.113.10:443#Second'));
    expect(state.subscriptions.single.nodes.length, 2);
    final again = await tester.runAsync(() => state.addSubscription(source: 'trojan://pw@203.0.113.10:443#Second'));
    expect(again, 'Эти серверы уже есть в списке.');
    await tester.pump(const Duration(seconds: 6));
  });

  test('a failed speed test is not read as a core update', () {
    expect(speedTestError('download: Get "https://speed.cloudflare.com/__down?bytes=25000000": context deadline exceeded'), contains('загрузку'));
    expect(speedTestError('download: 429 Too Many Requests'), contains('ограничил'));
    expect(speedTestError('upload: nothing went through'), contains('отдачу'));
    expect(speedTestError('download: EOF'), isNot(contains('ядра')));
    expect(humanError('xray: download: EOF'), contains('ядра'));
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
    // The home page's traffic history is on its way.
    await tester.pump(const Duration(seconds: 1));
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
    await tester.pump();
    // The home page's traffic history is on its way.
    await tester.pump(const Duration(seconds: 1));
  }, timeout: const Timeout(Duration(minutes: 1)));

  testWidgets('cores update themselves while the VPN is off', (tester) async {
    final state = await pumpApp(tester);
    // Looked for and installed by the app itself, without a word.
    await tester.runAsync(() async {
      for (var i = 0; i < 200 && state.info.versionOf('sing-box') != '1.14.3'; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
      await Future.delayed(const Duration(milliseconds: 100));
    });
    await tester.pump();
    expect(state.info.versionOf('sing-box'), '1.14.3');
    expect(state.logs.any((l) => l.source == 'sing-box' && l.message == 'обновлено до 1.14.3'), isTrue);
    expect(state.toasts, isEmpty);
    await open(tester, 'Ядра');
    await tester.pump();
    for (final gone in ['Проверить обновления', 'Обновить']) {
      expect(find.text(gone), findsNothing, reason: gone);
    }
    expect(find.textContaining('Доступна'), findsNothing);
    expect(find.text('версия 1.14.3'), findsOneWidget);

    // Found while connected: it waits for the VPN to be off.
    await tester.runAsync(() => state.connect());
    state.coreUpdates = [const CoreUpdate(kind: 'xray', current: '26.3.27', latest: '26.4.1', available: true)];
    await tester.runAsync(() => state.installCoreUpdates());
    await tester.pump();
    expect(state.coreUpdatesWaiting, ['xray']);
    expect(find.text('Версия 26.4.1 установится, когда VPN будет выключен'), findsOneWidget);
    await tester.runAsync(() async {
      await state.disconnect();
      for (var i = 0; i < 200 && state.coreUpdatesWaiting.isNotEmpty; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    await tester.pump();
    expect(state.coreUpdatesWaiting, isEmpty);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('apps picked from the running ones bypass the tunnel', (tester) async {
    final state = await pumpApp(tester);
    await tester.tap(find.text('Правила').first);
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
    // In the settings, beside the switch that guards against leaks.
    await open(tester, 'Настройки');
    await tester.pump();
    await tester.ensureVisible(find.text('Проверка утечки DNS'));
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
    // The home page's traffic history is on its way.
    await tester.pump(const Duration(seconds: 1));
    await tester.pump(const Duration(seconds: 1));
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

  test('a stopped service is started and said to be starting', () async {
    var starts = 0;
    final state = AppState(
      _OfflineBackend(),
      prefs: {},
      daemonStarter: () {
        starts++;
        return true;
      },
    );
    state.start();
    await Future.delayed(const Duration(milliseconds: 1000));
    // Started once, then looked for often without starting again.
    expect(starts, 1);
    expect(state.daemonStarting, isTrue);
    expect(state.offlineReason, 'Запускаем службу CoreShift');
    state.dispose();

    // Windows refused: the button with administrator rights is the way.
    final refused = AppState(_OfflineBackend(), prefs: {}, daemonStarter: () => false);
    refused.start();
    await Future.delayed(const Duration(milliseconds: 100));
    expect(refused.daemonStarting, isFalse);
    expect(refused.daemonStartRefused, isTrue);
    expect(refused.offlineReason, 'Служба CoreShift не запущена');
    refused.dispose();
  });

  testWidgets('the phone chooses the apps in the VPN', (tester) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = AppState(DemoBackend());
    await tester.runAsync(() async {
      state.start();
      while (!state.loaded) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    addTearDown(state.dispose);
    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(Brightness.dark),
        home: Scaffold(
          body: ListenableBuilder(
            listenable: state,
            builder: (_, _) => SingleChildScrollView(child: AndroidAppsPanel(state: state)),
          ),
        ),
      ),
    );
    expect(find.text('Все приложения'), findsOneWidget);
    expect(find.text('Выбрать'), findsNothing);

    await tester.tap(find.text('Только выбранные'));
    for (var i = 0; i < 5; i++) {
      await tester.pump(const Duration(milliseconds: 300));
      await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 30)));
    }
    expect(state.setting('routing.app_filter', ''), 'only');
    // Nothing chosen yet: said, as every app still uses the VPN.
    expect(find.text('Ничего не выбрано: пока через VPN идут все'), findsOneWidget);
    expect(find.text('Выбрать'), findsOneWidget);

    await tester.runAsync(() => state.updateSettings((s) => s['routing']['filter_apps'] = ['org.telegram.messenger', 'com.android.chrome']));
    await tester.pump();
    expect(find.text('Выбрано: 2'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  test('autostart follows its setting', () async {
    final calls = <bool>[];
    final state = AppState(
      DemoBackend(),
      prefs: {},
      autostartSetter: (on) {
        calls.add(on);
        return true;
      },
    );
    state.start();
    while (!state.loaded) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    // Set once at start, so a moved app's path is corrected.
    expect(calls, [false]);
    await state.updateSettings((s) => s['auto_connect'] = true);
    await state.updateSettings((s) => s['auto_connect'] = true);
    expect(calls, [false, true]);
    await state.updateSettings((s) => s['auto_connect'] = false);
    expect(calls, [false, true, false]);
    state.dispose();
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
      await open(tester, page);
      await tester.pump();
      expect(tester.takeException(), isNull, reason: page);
      expect(
        find.descendant(of: find.byType(PageHeader), matching: find.text(page)),
        page == 'Главная' ? findsNothing : findsOneWidget,
        reason: page,
      );
    }
    await tester.tap(find.text('Правила').first);
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
    await tester.tap(find.text('Правила').first);
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

    // The lists few people need are folded away.
    for (final fold in ['Всегда через VPN', 'Блокировать']) {
      await tester.ensureVisible(find.text(fold));
      await tester.tap(find.text(fold));
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
    await tester.tap(find.text('Правила').first);
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
    expect(v.label, '0.2.0');
    expect(const BuildVersion('dev').known, isFalse);
  });

  testWidgets('says when the version changed since the last run', (tester) async {
    for (final (prev, want) in [
      ('0.1.0+0+', 'CoreShift обновлён: 0.1.0 → 0.2.0'),
      ('0.2.1+20+ffff000', 'Установлена более ранняя версия CoreShift: 0.2.1 → 0.2.0'),
      ('0.2.0+14+0000aaa-dirty', 'CoreShift пересобран: 0.2.0'),
      ('0.2.0+12+1111bbb', 'CoreShift пересобран: 0.2.0'),
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

  testWidgets('opens the support chat the panel names, and only web or Telegram links', (tester) async {
    final opened = <String>[];
    final state = AppState(
      DemoBackend(),
      linkOpener: (url) async {
        opened.add(url);
        return true;
      },
    );
    await pumpApp(tester, custom: state);
    // On every subscription's card, not on the home page.
    expect(find.byTooltip('Написать в поддержку в Telegram'), findsNothing);
    await tester.tap(find.text('Серверы').first);
    await tester.pump();
    await tester.tap(find.byTooltip('Написать в поддержку в Telegram').first);
    await tester.pump();
    expect(opened, ['https://t.me/example_support']);

    await state.openLink('file:///C:/Windows/System32/calc.exe');
    await state.openLink('javascript:alert(1)');
    await tester.pump();
    expect(opened, hasLength(1));
    expect(find.text('Панель прислала ссылку, которую нельзя открыть'), findsOneWidget);
    await tester.pump(const Duration(seconds: 6));
  });

  test('support links show the messenger they open', () {
    for (final (url, kind) in [
      ('https://t.me/Leikaccit', SupportKind.telegram),
      ('tg://resolve?domain=support', SupportKind.telegram),
      ('https://vk.com/club1', SupportKind.vk),
      ('https://vk.me/support', SupportKind.vk),
      ('https://m.vk.com/support', SupportKind.vk),
      ('https://wa.me/79990000000', SupportKind.whatsapp),
      ('https://discord.gg/abc', SupportKind.discord),
      ('mailto:help@example.com', SupportKind.email),
      ('https://notvk.com/x', SupportKind.other),
      ('https://example.com/help', SupportKind.other),
    ]) {
      expect(SupportKind.of(url), kind, reason: url);
    }
  });

  testWidgets('warns when the service is another version', (tester) async {
    // The demo service reports 0.2.0 without a build.
    final state = AppState(DemoBackend(), version: const BuildVersion('0.2.0', 14, 'abc1234'));
    await pumpApp(tester, custom: state);
    expect(state.versionMismatch, isTrue);
    await tester.tap(find.text('Настройки').first);
    await tester.pump();
    await tester.ensureVisible(find.text('Версия службы'));
    expect(find.text('0.2.0'), findsWidgets);
    expect(find.textContaining('Переустановите CoreShift целиком'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('finds, offers and installs an update of CoreShift', (tester) async {
    final state = await pumpApp(tester);
    await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 300)));
    expect(state.appUpdate.state, 'idle');
    // Connected: the update waits for the VPN, so it is offered.
    await tester.runAsync(() => state.connect());
    await tester.pump();
    await tester.tap(find.text('Настройки').first);
    await tester.pump();
    await tester.ensureVisible(find.text('Обновления'));
    expect(find.text('Устанавливать обновления автоматически'), findsOneWidget);

    await tester.tap(find.text('Проверить сейчас'));
    // The check runs on the test's clock, the events on the real one.
    for (var i = 0; i < 10; i++) {
      await tester.pump(const Duration(milliseconds: 200));
      await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 100)));
    }
    await tester.pump();
    expect(state.appUpdate.state, 'ready');
    expect(find.textContaining('Скачана версия 0.3.0.'), findsOneWidget);

    // The window offering it.
    expect(find.text('Доступно обновление'), findsOneWidget);
    await tester.tap(find.text('Обновить'));
    // The check runs on the test's clock, the events on the real one.
    for (var i = 0; i < 5; i++) {
      await tester.pump(const Duration(milliseconds: 200));
      await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 100)));
    }
    await tester.pump();
    expect(state.appUpdate.state, 'installing');
    expect(find.textContaining('CoreShift перезапустится сам'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 6));
  });

  test('update errors are explained', () {
    expect(humanError('check for updates: the releases token is invalid or expired (401)'), contains('Доступ к обновлениям истёк'));
    expect(humanError('update signature does not match the release key'), contains('подпись'));
    expect(humanError('check for updates: dial tcp: i/o timeout'), contains('нет связи с GitHub'));
  });

  test('settings changes are described for the journal', () {
    final before = {
      'tun': true,
      'cores': {
        'mode': 'auto',
        'priority': ['xray', 'sing-box'],
      },
      'routing': {
        'russia_direct': false,
        'direct_domains': ['a.ru'],
      },
    };
    final after = {
      'tun': false,
      'cores': {
        'mode': 'manual',
        'priority': ['xray', 'sing-box'],
      },
      'routing': {
        'russia_direct': true,
        'direct_domains': ['a.ru', 'b.ru'],
      },
    };
    expect(settingsChanges(before, after), [
      'tun: да → нет',
      'cores.mode: auto → manual',
      'routing.russia_direct: нет → да',
      'routing.direct_domains: 1 записей → 2 записей',
    ]);
    expect(settingsChanges(before, before), isEmpty);
  });

  testWidgets('the copied journal starts with versions and mode', (tester) async {
    final state = await pumpApp(tester);
    final header = state.diagnosticsHeader();
    expect(header[0], startsWith('CoreShift: приложение'));
    expect(header[2], contains('все приложения (TUN)'));
    expect(header.join('\n'), isNot(contains('https://')));
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('the desktop home page shows the address sites see', (tester) async {
    final state = await pumpApp(tester);
    Future<void> settle() async {
      for (var i = 0; i < 4; i++) {
        await tester.pump(const Duration(seconds: 1));
        await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 100)));
      }
      await tester.pump();
    }

    await settle();
    expect(find.text('Ваш IP-адрес'), findsOneWidget);
    expect(find.text('95.31.18.119'), findsOneWidget);
    expect(find.text('Россия'), findsOneWidget);
    expect(find.textContaining('настоящий адрес'), findsOneWidget);

    await tester.runAsync(() async {
      await state.connect();
      await Future.delayed(const Duration(milliseconds: 2300));
    });
    await settle();
    expect(find.text('185.23.41.7'), findsOneWidget);
    expect(find.text('Германия'), findsOneWidget);
    expect(find.text('Сайты видят адрес VPN-сервера'), findsOneWidget);

    // Hidden for screenshots.
    await tester.tap(find.byTooltip('Скрыть адрес, например для скриншота'));
    await tester.pump();
    expect(find.text('185.23.•.•'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.runAsync(() => state.disconnect());
    await tester.pump(const Duration(seconds: 6));
    // The home page's traffic history is on its way.
    await tester.pump(const Duration(seconds: 1));
  });

  testWidgets('the home page keeps to the connection', (tester) async {
    for (final size in [const Size(390, 844), const Size(1400, 900)]) {
      final state = await pumpApp(tester, size: size);
      expect(find.text('Отключено'), findsWidgets);
      expect(find.text('Amsterdam'), findsWidgets);
      // The subscription is on its card, the cores on their page, the mode
      // in the settings.
      // No tab of tests either: the speed test and the traffic are a line
      // each under the route.
      for (final t in ['Очередь ядер', 'Через VPN', 'Смен ядра', 'АВТОСВАП', 'Xray-core', 'подписка ещё', 'Проверка']) {
        expect(find.textContaining(t), findsNothing, reason: '$t at $size');
      }
      await tester.runAsync(() async {
        await state.connect();
        await Future.delayed(const Duration(milliseconds: 2300));
      });
      await tester.pump();
      expect(find.text('Подключено'), findsWidgets);
      expect(find.text('Загрузка'), findsWidgets);
      expect(tester.takeException(), isNull);
      await tester.runAsync(() => state.disconnect());
      await tester.pump(const Duration(seconds: 1));
    }
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('the home page says when the server does not answer', (tester) async {
    for (final size in [const Size(390, 844), const Size(1400, 900)]) {
      final state = await pumpApp(tester, size: size);
      await tester.runAsync(() async {
        await state.connect();
        await Future.delayed(const Duration(milliseconds: 2300));
      });
      await tester.pump();
      const warning = 'Сервер не отвечает';
      Event check(String error) => Event(time: DateTime.now(), kind: 'health', core: state.status.core, error: error);
      expect(state.status.core, isNotEmpty);
      expect(find.textContaining(warning), findsNothing, reason: 'working at $size');

      // One failed check is often a blip.
      state.injectEvent(check('timeout'));
      await tester.pump();
      expect(find.textContaining(warning), findsNothing, reason: 'one failure at $size');

      // Still failing: the page says so.
      state.injectEvent(check('timeout'));
      await tester.pump();
      expect(state.serverUnresponsive, isTrue);
      expect(find.textContaining(warning), findsOneWidget, reason: 'two failures at $size');
      expect(tester.takeException(), isNull);

      // It works again: the warning goes.
      state.injectEvent(check(''));
      await tester.pump();
      expect(find.textContaining(warning), findsNothing, reason: 'recovered at $size');

      // Failures while disconnected are not the page's business.
      state.injectEvent(check('timeout'));
      state.injectEvent(check('timeout'));
      await tester.runAsync(() => state.disconnect());
      await tester.pump(const Duration(seconds: 1));
      expect(state.serverUnresponsive, isFalse);
      expect(find.textContaining(warning), findsNothing, reason: 'disconnected at $size');
    }
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('the panel automatic selection is known and the switch is told in the journal', (tester) async {
    final state = await pumpApp(tester);
    final sub = Subscription.fromJson({
      'id': 'p',
      'display_name': 'Panel',
      'auto': ['fp1', 'fp2'],
      'nodes': [],
      'info': {},
    });
    expect(sub.auto, ['fp1', 'fp2']);
    // The demo's subscription has no automatic selection.
    expect(state.autoSwitching, isFalse);
    state.subscriptions = [sub];
    state.selection = const Selection(subscription: 'p', fingerprint: 'fp2');
    expect(state.autoSwitching, isTrue);
    state.selection = const Selection(subscription: 'p', fingerprint: 'other');
    expect(state.autoSwitching, isFalse);

    state.injectEvent(Event(time: DateTime.now(), kind: 'failover', from: 'Amsterdam', line: 'Frankfurt'));
    state.injectEvent(Event(time: DateTime.now(), kind: 'failover', from: 'Frankfurt', error: 'no other server of the automatic selection answers'));
    final lines = state.logs.map((l) => l.message).toList();
    expect(lines, contains('сервер «Amsterdam» не отвечает, подключаюсь к «Frankfurt»'));
    expect(lines, contains('ни один другой сервер подписки не отвечает'));
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('a rule switched while connected says it waits for reconnecting', (tester) async {
    final state = await pumpApp(tester, size: const Size(390, 844));
    await tester.runAsync(() => state.connect());
    await tester.pump();
    await tester.tap(find.text('Правила').first);
    await tester.pump();
    expect(find.text('Изменения применятся после переподключения'), findsNothing);
    await tester.runAsync(() async {
      await tester.tap(find.byType(Switch).first);
      await Future.delayed(const Duration(milliseconds: 300));
    });
    await tester.pump();
    expect(find.text('Изменения применятся после переподключения'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.runAsync(() async {
      await tester.tap(find.text('Применить'));
      await Future.delayed(const Duration(milliseconds: 1500));
    });
    await tester.pump();
    expect(find.text('Изменения применятся после переподключения'), findsNothing);
    await tester.runAsync(() => state.disconnect());
    await tester.pump(const Duration(seconds: 1));
  });

  testWidgets('an update that installs by itself is not offered', (tester) async {
    // Automatic updates on and the VPN off: the service installs it at once.
    final state = await pumpApp(tester);
    await tester.runAsync(() async {
      await state.checkAppUpdate();
      await Future.delayed(const Duration(milliseconds: 1200));
    });
    await tester.pump();
    await tester.pump();
    expect(state.appUpdate.state, 'ready');
    expect(find.text('Доступно обновление'), findsNothing);
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('a downloaded update is offered in a window, once', (tester) async {
    final state = await pumpApp(tester, size: const Size(390, 844));
    await tester.runAsync(() => state.connect());
    await tester.pump();
    await tester.runAsync(() async {
      await state.checkAppUpdate();
      await Future.delayed(const Duration(milliseconds: 1200));
    });
    await tester.pump();
    await tester.pump();
    expect(find.text('Доступно обновление'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.tap(find.text('Позже'));
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.text('Доступно обновление'), findsNothing);

    // Not again for the same version; the settings still offer it.
    await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 100)));
    await tester.pump();
    expect(find.text('Доступно обновление'), findsNothing);
    await tester.tap(find.descendant(of: find.byType(NavigationBar), matching: find.text('Настройки')));
    await tester.pump();
    expect(find.text('Установить сейчас'), findsOneWidget);
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('the add dialog fits a phone', (tester) async {
    await pumpApp(tester, size: const Size(390, 844));
    await tester.tap(find.descendant(of: find.byType(NavigationBar), matching: find.text('Серверы')));
    await tester.pump();
    await tester.tap(find.text('Добавить'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.text('Добавить подписку'), findsOneWidget);
    expect(tester.takeException(), isNull);
    // Let the ping test the page started finish.
    await tester.pump(const Duration(seconds: 30));
  });

  testWidgets('a phone hides explanations behind an icon', (tester) async {
    await pumpApp(tester, size: const Size(390, 844));
    await tester.tap(find.descendant(of: find.byType(NavigationBar), matching: find.text('Настройки')));
    await tester.pump();
    const text = 'IPv6-трафик тоже идёт через VPN';
    expect(find.textContaining(text), findsNothing);
    final row = find.ancestor(of: find.text('IPv6 через туннель'), matching: find.byType(Row)).first;
    await tester.tap(find.descendant(of: row, matching: find.byIcon(Icons.info_outline)));
    await tester.pump(const Duration(milliseconds: 600));
    expect(find.textContaining(text), findsOneWidget);
    await tester.pump(const Duration(seconds: 1));
  });

  for (final width in [390.0, 360.0]) {
    testWidgets('every page fits a phone ${width.round()} wide', (tester) async {
      final state = await pumpApp(tester, size: Size(width, 800));
      expect(find.byType(NavigationBar), findsOneWidget);
      // Five tabs, the journal one of them.
      expect(find.byType(NavigationDestination), findsNWidgets(5));
      final problems = <String>[];
      Future<void> check(String page) async {
        await tester.pump();
        for (Object? e = tester.takeException(); e != null; e = tester.takeException()) {
          problems.add('$page: ${'$e'.split('\n').first}');
        }
      }

      for (final page in ['Серверы', 'Правила', 'Журнал', 'Настройки', 'Главная']) {
        await tester.tap(find.descendant(of: find.byType(NavigationBar), matching: find.text(page)));
        await check(page);
        // Scroll through the page, so rows further down are laid out too.
        final scrollable = find.byType(Scrollable);
        if (scrollable.evaluate().isNotEmpty) {
          await tester.drag(scrollable.first, const Offset(0, -2000));
          await check('$page (scrolled)');
        }
      }
      // The cores open from the settings.
      await open(tester, 'Ядра');
      await tester.pump();
      expect(find.descendant(of: find.byType(PageHeader), matching: find.text('Ядра')), findsOneWidget);
      await check('Ядра');
      expect(problems, isEmpty);
      expect(state.loaded, isTrue);
      await tester.pump(const Duration(seconds: 30));
    });
  }

  testWidgets('the speed test shows its figures on the desktop and the phone', (tester) async {
    for (final size in [const Size(1400, 900), const Size(390, 844)]) {
      final state = await pumpApp(tester, size: size);
      // The cores' own update, which runs on the real clock, done first:
      // the test steps the speed test on the test's clock.
      await tester.runAsync(() async {
        for (var i = 0; i < 200 && (state.checkingUpdates || state.updatingCore.isNotEmpty || state.coreUpdatesWaiting.isNotEmpty); i++) {
          await Future.delayed(const Duration(milliseconds: 20));
        }
      });
      // The speed test is a line on the home page.
      await tester.pump(const Duration(seconds: 1));
      await tester.ensureVisible(find.text('Проверить'));
      await tester.pump();
      expect(find.textContaining('Скорость вашего интернета без VPN'), findsOneWidget, reason: '$size');
      await tester.tap(find.text('Проверить'));
      for (var i = 0; i < 6; i++) {
        await tester.pump(const Duration(milliseconds: 250));
      }
      // Live: the download is measured.
      expect(state.speedTest.running, isTrue);
      expect(state.speedTest.phase, SpeedPhase.download);
      expect(find.textContaining('Мбит/с'), findsWidgets);
      expect(find.textContaining('Проверяем загрузку без VPN'), findsOneWidget);
      for (var i = 0; i < 16; i++) {
        await tester.pump(const Duration(milliseconds: 250));
      }
      expect(state.speedTest.phase, SpeedPhase.done);
      expect(state.speedTest.downBps, greaterThan(state.speedTest.upBps));
      expect(find.textContaining('без VPN, только что'), findsOneWidget, reason: '$size');
      expect(find.text('Ещё раз'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pump(const Duration(seconds: 6));
    }
  });

  testWidgets('a link from a panel or the clipboard is added only once the user agrees', (tester) async {
    for (final size in [const Size(1400, 900), const Size(390, 844)]) {
      final state = await pumpApp(tester, size: size);
      const url = 'https://sub.example.com/AbCdEf1234567890';
      state.offerImport('happ://add/$url#Дом', ImportFrom.link);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));
      expect(find.text('Добавить подписку?'), findsOneWidget, reason: '$size');
      expect(find.text('Дом'), findsOneWidget);
      expect(find.text('sub.example.com'), findsOneWidget);
      // The link, with its token, is not shown.
      expect(find.textContaining('AbCdEf'), findsNothing);
      expect(tester.takeException(), isNull);
      await tester.tap(find.text('Добавить').last);
      for (var i = 0; i < 6; i++) {
        await tester.pump(const Duration(milliseconds: 200));
      }
      expect(find.text('Добавить подписку?'), findsNothing);
      expect(state.subscriptions.where((s) => s.url == url), hasLength(1));

      // Again: already there, nothing to ask.
      state.offerImport(url, ImportFrom.link);
      expect(state.pendingImport, isNull);
      expect(state.toasts.last.message, 'Эта подписка уже добавлена');

      // The clipboard: only links that look like subscriptions, each once.
      state.offerImport('https://www.youtube.com/watch?v=x', ImportFrom.clipboard);
      expect(state.pendingImport, isNull);
      state.offerImport('https://panel.example.org/sub/7f3c9a1e', ImportFrom.clipboard);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));
      expect(find.text('В буфере обмена ссылка на подписку.'), findsOneWidget);
      await tester.tap(find.text('Не добавлять'));
      await tester.pump(const Duration(milliseconds: 300));
      expect(state.pendingImport, isNull);
      state.offerImport('https://panel.example.org/sub/7f3c9a1e', ImportFrom.clipboard);
      expect(state.pendingImport, isNull);
      expect(state.prefs['clipboard_offered'], isNot(contains('example')));
      await tester.pump(const Duration(seconds: 6));
    }
  });

  testWidgets('a subscription shows as a QR code on the desktop and the phone', (tester) async {
    for (final size in [const Size(1400, 900), const Size(390, 844)]) {
      await pumpApp(tester, size: size);
      final nav = size.width > 600 ? find.text('Серверы').first : find.descendant(of: find.byType(NavigationBar), matching: find.text('Серверы'));
      await tester.tap(nav);
      await tester.pump();
      await tester.tap(find.byTooltip('Действия').first);
      for (var i = 0; i < 5; i++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      await tester.tap(find.text('QR-код для телефона'));
      for (var i = 0; i < 8; i++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      expect(find.byType(QrView), findsOneWidget, reason: '$size');
      expect(find.textContaining('ключом доступа'), findsOneWidget);
      expect(tester.takeException(), isNull, reason: '$size');
      await tester.tap(find.text('Готово'));
      for (var i = 0; i < 5; i++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      expect(find.byType(QrView), findsNothing);
      // Let the ping test the page started finish.
      await tester.pump(const Duration(seconds: 30));
    }
  });

  testWidgets('a plain http subscription is added with a warning on the desktop and the phone', (tester) async {
    for (final size in [const Size(1400, 900), const Size(390, 844)]) {
      final state = await pumpApp(tester, size: size);
      final added = state.addSubscription(source: 'http://panel.example.org/sub/AbCdEf1234567890');
      for (var i = 0; i < 10; i++) {
        await tester.pump(const Duration(milliseconds: 200));
      }
      expect(await added, isNull, reason: '$size');
      expect(state.toasts.map((t) => t.message), contains(insecureLinkWarning), reason: '$size');
      expect(find.text(insecureLinkWarning), findsOneWidget, reason: '$size');
      expect(tester.takeException(), isNull, reason: '$size');
      // The same link again, as the daemon lists it without the token.
      state.offerImport('http://panel.example.org/sub/AbCdEf1234567890', ImportFrom.link);
      expect(state.pendingImport, isNull);
      await tester.pump(const Duration(seconds: 30));
    }
  });

  testWidgets('subscriptions fold away in the server list and stay folded', (tester) async {
    for (final size in [const Size(1400, 900), const Size(390, 844)]) {
      final state = await pumpApp(tester, size: size);
      expect(state.subscriptions.length, greaterThan(1));
      final sub = state.subscriptions.first;
      final node = cleanNodeName(sub.nodes.first.name);
      final nav = size.width > 600 ? find.text('Серверы').first : find.descendant(of: find.byType(NavigationBar), matching: find.text('Серверы'));
      final home = size.width > 600 ? find.text('Главная').first : find.descendant(of: find.byType(NavigationBar), matching: find.text('Главная'));
      await tester.tap(nav);
      await tester.pump();
      final shown = find.text(node).evaluate().length;
      expect(shown, greaterThan(0));

      await tester.ensureVisible(find.byIcon(Icons.expand_less).first);
      await tester.tap(find.byIcon(Icons.expand_less).first);
      await tester.pump();
      expect(find.text(node).evaluate().length, lessThan(shown), reason: '$size');
      expect(state.prefs['servers_folded'], contains('s:${sub.id}'));

      // Still folded after another page.
      await tester.tap(home);
      await tester.pump();
      await tester.tap(nav);
      await tester.pump();
      expect(find.text(node).evaluate().length, lessThan(shown), reason: '$size');

      // A search shows what it finds, folded or not.
      await tester.enterText(find.byType(TextField).first, node);
      await tester.pump();
      expect(find.text(node), findsWidgets);
      await tester.enterText(find.byType(TextField).first, '');
      await tester.pump();

      await tester.ensureVisible(find.byIcon(Icons.expand_more).first);
      await tester.tap(find.byIcon(Icons.expand_more).first);
      await tester.pump();
      expect(find.text(node).evaluate().length, shown);
      expect(state.prefs['servers_folded'], isEmpty);
      expect(tester.takeException(), isNull, reason: '$size');
      await tester.pump(const Duration(seconds: 30));
    }
  });

  testWidgets('a subscription running out is shown and notified once per step', (tester) async {
    for (final size in [const Size(1400, 900), const Size(390, 844)]) {
      final state = await pumpApp(tester, size: size);
      final base = state.subscriptions.first;
      Subscription withInfo(SubInfo info) => Subscription(
        id: base.id,
        name: base.name,
        displayName: base.displayName,
        url: base.url,
        userAgent: '',
        info: info,
        format: base.format,
        nodes: base.nodes,
        skipped: const [],
      );
      state.subscriptions = [
        withInfo(
          SubInfo(
            expire: DateTime.now().add(const Duration(hours: 10)),
            total: 100 << 30,
            upload: 1 << 30,
            download: 95 << 30,
            webPageUrl: 'https://example.com/me',
          ),
        ),
      ];
      state.checkSubscriptions();
      await tester.pump();
      final warnings = state.subscriptionWarnings;
      expect(warnings.map((w) => w.level), [2, 1]);
      expect(state.toasts.map((t) => t.message), containsAll([warnings[0].title, warnings[1].title]));
      // The most urgent one is on the home page, with the way to renew.
      expect(find.text(warnings[0].title), findsWidgets);
      expect(find.text('Продлить'), findsOneWidget);
      expect(tester.takeException(), isNull, reason: '$size');

      // Checked again: nothing new to say.
      state.toasts.clear();
      state.checkSubscriptions();
      expect(state.toasts, isEmpty);
      // Over: the next step is told.
      state.subscriptions = [withInfo(SubInfo(expire: DateTime.now().subtract(const Duration(hours: 1))))];
      state.checkSubscriptions();
      expect(state.toasts.single.message, contains('закончилась'));
      // Renewed: the warning and what was told go.
      state.subscriptions = [withInfo(SubInfo(expire: DateTime.now().add(const Duration(days: 30))))];
      state.checkSubscriptions();
      expect(state.subscriptionWarnings, isEmpty);
      expect(state.prefs['sub_warned'], isEmpty);
      await tester.pump(const Duration(seconds: 6));
    }
  });

  testWidgets('the cores: one list, the manual mode is one core switched on', (tester) async {
    final state = await pumpApp(tester);
    await tester.runAsync(
      () => state.updateSettings((s) {
        s['cores']['mode'] = 'manual';
        s['cores']['manual'] = 'xray';
      }),
    );
    await open(tester, 'Ядра');
    await tester.pump();
    for (final gone in ['Автосвап', 'Вручную', 'Совместимость', 'Проверить обновления']) {
      expect(find.text(gone), findsNothing, reason: gone);
    }
    expect(find.textContaining('Включено одно ядро: Xray-core'), findsOneWidget);
    // Switching another core on goes back to the automatic order.
    // The name's own row, then the core's.
    final singBox = find.ancestor(of: find.text('sing-box'), matching: find.byType(Row)).at(1);
    await tester.runAsync(() async {
      await tester.tap(find.descendant(of: singBox, matching: find.byType(Switch)));
      await Future.delayed(const Duration(milliseconds: 300));
    });
    await tester.pump();
    expect(state.setting('cores.mode', ''), 'auto');
    expect(state.setting<List>('cores.priority', const []), ['xray', 'sing-box']);
    // Only the way back is in sight; the rest is under «Дополнительно».
    expect(find.text('Возвращаться к основному ядру'), findsOneWidget);
    expect(find.text('Адрес проверки связи'), findsNothing);
    await tester.ensureVisible(find.text('Дополнительно'));
    await tester.tap(find.text('Дополнительно'));
    await tester.pump();
    expect(find.text('Адрес проверки связи'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('the servers have one search and no duplicate controls', (tester) async {
    final state = await pumpApp(tester);
    await open(tester, 'Серверы');
    await tester.pump();
    for (final gone in ['Все протоколы', 'Самый быстрый']) {
      expect(find.text(gone), findsNothing, reason: gone);
    }
    expect(find.byTooltip('Как проверять пинг'), findsNothing);
    // The search finds a protocol by its name or its link scheme.
    await tester.enterText(find.byType(TextField).first, 'hy2');
    await tester.pump();
    expect(find.text('Helsinki'), findsOneWidget);
    expect(find.text('Rotterdam'), findsNothing);
    // The subscription's menu keeps one way to its page.
    await tester.tap(find.byTooltip('Действия').first);
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.text('Копировать адрес страницы'), findsNothing);
    expect(state.latencyAutoTested, isTrue);
    await tester.pump(const Duration(seconds: 30));
  });

  testWidgets('a forced test through the core goes back to the automatic one', (tester) async {
    final state = await pumpApp(tester);
    await tester.runAsync(() async {
      await state.updateSettings((s) => s['cores']['latency_test'] = 'proxy');
      await state.testLatency();
    });
    expect(state.setting('cores.latency_test', ''), 'ping');
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('one switch guards against every DNS leak', (tester) async {
    final state = await pumpApp(tester);
    await open(tester, 'Настройки');
    await tester.pump();
    expect(find.text('Обновлять каждые, ч'), findsNothing);
    expect(find.text('DNS браузеров только через VPN'), findsNothing);
    final row = find.ancestor(of: find.text('Защита от утечек DNS'), matching: find.byType(Row)).first;
    await tester.ensureVisible(row);
    await tester.runAsync(() async {
      await tester.tap(find.descendant(of: row, matching: find.byType(Switch)));
      await Future.delayed(const Duration(milliseconds: 300));
    });
    await tester.pump();
    for (final g in ['dns.block_browser_doh', 'dns.block_dot', 'dns.strict']) {
      expect(state.setting(g, false), isTrue, reason: g);
    }
    // The expert settings are folded away.
    expect(find.text('User-Agent'), findsNothing);
    await tester.ensureVisible(find.text('Дополнительно'));
    await tester.tap(find.text('Дополнительно'));
    await tester.pump();
    expect(find.text('User-Agent'), findsOneWidget);
    expect(find.text('DNS через VPN'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 6));
  });

  test('tinted text reads on the light theme', () {
    double contrast(Color a, Color b) {
      final la = a.computeLuminance(), lb = b.computeLuminance();
      return (max(la, lb) + .05) / (min(la, lb) + .05);
    }

    const p = Palette.light;
    final colors = [
      for (final pr in ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'wireguard', 'anytls', 'other']) protocolColor(pr),
      for (final k in allCores) coreStyle(k).color,
      accent,
      okColor,
      warnColor,
      errColor,
      swapColor,
    ];
    for (final c in colors) {
      // On a chip: the panel with the colour's tint over it.
      final chip = Color.alphaBlend(c.withValues(alpha: .14), p.surface);
      expect(contrast(p.ink(c), chip), greaterThanOrEqualTo(4.5), reason: '$c');
    }
    // The dark theme keeps its colours.
    expect(Palette.dark.ink(protocolColor('hysteria2')), protocolColor('hysteria2'));
  });

  testWidgets('the journal: all or errors, and a copy for support', (tester) async {
    final state = await pumpApp(tester);
    await open(tester, 'Журнал');
    await tester.pump();
    // A main page: no way back to another one.
    expect(find.byIcon(Icons.arrow_back), findsNothing);
    expect(find.text('Все'), findsOneWidget);
    expect(find.text('Ошибки'), findsOneWidget);
    for (final gone in ['Автосвап', 'Ядра', 'Очистить']) {
      expect(find.text(gone), findsNothing, reason: gone);
    }
    // The demo's earlier session: a switch of cores and an error.
    expect(find.textContaining('Xray-core → sing-box'), findsOneWidget);
    // The cores' own output is kept for a search, not shown on the page.
    expect(find.text('Xray 26.3.27 started'), findsNothing);
    expect(state.logs.any((l) => l.message == 'Xray 26.3.27 started'), isTrue);
    await tester.tap(find.text('Ошибки'));
    await tester.pump();
    expect(find.textContaining('Xray-core → sing-box'), findsNothing);
    expect(find.textContaining('502'), findsOneWidget);
    expect(find.text('Копировать'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 6));
  });
}

/// A daemon that is not running.
class _OfflineBackend implements Backend {
  @override
  Future<dynamic> call(String method, String path, [Object? body]) async => throw const DaemonOffline('Служба CoreShift не запущена');

  @override
  Stream<Event> events() => const Stream.empty();

  @override
  String get description => 'test';
}
