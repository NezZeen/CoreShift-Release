import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/backend.dart';
import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/pages/dns_check.dart';

/// The demo service, recording what the app asks of it; [status] adds to
/// the status it answers, and [noCheck] plays a service that predates the
/// DNS check.
class _Backend extends DemoBackend {
  final calls = <(String, String, Object?)>[];
  Json status = {};
  bool noCheck = false;

  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    calls.add((method, path, body));
    if (noCheck && path == '/v1/dnscheck') throw const ApiError(404, 'not found');
    final r = await super.call(method, path, body);
    if (method == 'GET' && path == '/v1/status' && status.isNotEmpty) return {...(r as Json), ...status};
    return r;
  }

  List<Object?> checks() => [
    for (final c in calls)
      if (c.$2 == '/v1/dnscheck') c.$3,
  ];
}

const phone = Size(390, 844);
const desktop = Size(1400, 900);

const doh = 'https://94.140.14.14/dns-query';
const blocked = 'не отвечает в этой сети: соединение отклонено — провайдер, похоже, блокирует этот DNS';

void main() {
  Future<(AppState, _Backend)> pump(WidgetTester tester, Size size) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final backend = _Backend();
    final state = AppState(backend);
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    return (state, backend);
  }

  /// Moves the test's clock on, through the demo service's delays.
  Future<void> settle(WidgetTester tester) async {
    for (var i = 0; i < 12; i++) {
      await tester.pump(const Duration(milliseconds: 100));
    }
  }

  Finder nav(String page) {
    final onPhone = find.byType(NavigationBar).evaluate().isNotEmpty;
    return onPhone ? find.descendant(of: find.byType(NavigationBar), matching: find.text(page)) : find.text(page).first;
  }

  Future<void> openDnsSettings(WidgetTester tester) async {
    await tester.tap(nav('Настройки'));
    await tester.pump();
    await tester.ensureVisible(find.text('Дополнительно'));
    await tester.tap(find.text('Дополнительно'));
    await tester.pump();
  }

  /// Types [text] in the field and submits it, as the user does.
  Future<void> type(WidgetTester tester, Finder field, String text) async {
    await tester.ensureVisible(field);
    await tester.enterText(field, text);
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pump();
  }

  Finder direct() => find.widgetWithText(TextField, 'системный');
  Finder remote() => find.widgetWithText(TextField, 'https://1.1.1.1/dns-query');

  Future<void> connect(WidgetTester tester, AppState state) async {
    await tester.runAsync(() async {
      await state.connect();
      await Future.delayed(const Duration(milliseconds: 2300));
    });
    await tester.pump();
    expect(state.status.state, ConnState.connected);
  }

  Future<void> disconnect(WidgetTester tester, AppState state) async {
    await tester.runAsync(() => state.disconnect());
    await tester.pump(const Duration(seconds: 1));
    await tester.pump(const Duration(seconds: 6));
  }

  /// Lets what the test left pending run out.
  Future<void> drain(WidgetTester tester) => tester.pump(const Duration(seconds: 6));

  testWidgets('the hint is above the DNS fields, in full, on a phone and on a desktop', (tester) async {
    for (final size in [phone, desktop]) {
      final (state, _) = await pump(tester, size);
      await openDnsSettings(tester);
      await tester.ensureVisible(find.byKey(const ValueKey('dns-hint')));
      expect(find.byKey(const ValueKey('dns-hint')), findsOneWidget, reason: '$size');
      expect(find.text(dnsHintText), findsOneWidget, reason: '$size');
      expect(
        dnsHintText,
        'Свой DNS с блокировкой рекламы (например, AdGuard) лучше указывать в поле «через VPN»: туда провайдер не дотянется. '
        'В поле «напрямую» его может заблокировать провайдер или белый список.',
      );
      // The fields the hint names are there, by those names.
      expect(find.text('DNS через VPN'), findsOneWidget, reason: '$size');
      expect(find.text('DNS напрямую'), findsOneWidget, reason: '$size');
      expect(tester.takeException(), isNull, reason: '$size');
      expect(state.loaded, isTrue);
      await tester.pump(const Duration(seconds: 6));
    }
  });

  testWidgets('a changed DNS is tried at once and the result is under the field', (tester) async {
    for (final size in [phone, desktop]) {
      final (state, backend) = await pump(tester, size);
      await openDnsSettings(tester);

      // Nothing typed for the direct one: the system's is used, and there is
      // nothing to try. The tunnel's has not been tried yet.
      expect(find.byKey(const ValueKey('dns-check-direct')).evaluate(), isEmpty, reason: 'direct, before, $size');
      expect(
        find.descendant(of: find.byKey(const ValueKey('dns-check-remote')), matching: find.text('ещё не проверен')),
        findsOneWidget,
        reason: '$size',
      );

      // The reported case: AdGuard's DoH by address as the direct DNS.
      await type(tester, direct(), doh);
      await settle(tester);
      expect(backend.checks(), [
        {'direct': doh},
      ], reason: 'what was asked at $size');
      expect(state.setting('dns.direct', ''), doh);
      final row = find.byKey(const ValueKey('dns-check-direct'));
      await tester.ensureVisible(row);
      expect(
        find.descendant(of: row, matching: find.text(blocked)),
        findsOneWidget,
        reason: 'blocked at $size',
      );
      expect(state.logs.any((l) => l.message.contains('DNS напрямую ($doh): $blocked')), isTrue);

      // Another value: the old result is not shown for it, the new one is.
      await type(tester, direct(), '1.1.1.1');
      await settle(tester);
      expect(find.text(blocked), findsNothing, reason: 'replaced at $size');
      expect(
        find.descendant(of: find.byKey(const ValueKey('dns-check-direct')), matching: find.text('отвечает (35 мс)')),
        findsOneWidget,
        reason: '$size',
      );

      // The one that lives in the tunnel waits for the VPN.
      await type(tester, remote(), 'tls://dns.google');
      await settle(tester);
      expect(backend.checks().last, {'remote': 'tls://dns.google'});
      expect(find.text('через VPN проверю, когда он подключён'), findsOneWidget, reason: 'idle at $size');

      // Connected, it can be tried again from the row.
      await connect(tester, state);
      final again = find.descendant(of: find.byKey(const ValueKey('dns-check-remote')), matching: find.text('Проверить ещё раз'));
      await tester.ensureVisible(again);
      await tester.tap(again);
      await settle(tester);
      expect(find.text('отвечает (48 мс)'), findsOneWidget, reason: 'connected at $size');
      expect(tester.takeException(), isNull, reason: '$size');
      await disconnect(tester, state);
    }
  });

  testWidgets('a server the tunnel cannot reach and one nobody answers for say so', (tester) async {
    final (state, backend) = await pump(tester, phone);
    await connect(tester, state);
    await openDnsSettings(tester);
    await type(tester, remote(), 'tls://203.0.113.9');
    await settle(tester);
    expect(find.text('не отвечает через VPN: нет ответа'), findsOneWidget);
    await type(tester, direct(), 'quic://94.140.14.14');
    await settle(tester);
    expect(find.text('этот вид DNS (QUIC, HTTP/3) не проверяется'), findsOneWidget);
    expect(backend.checks(), hasLength(2));
    expect(tester.takeException(), isNull);
    await disconnect(tester, state);
  });

  testWidgets('a service without the check shows no rows and is not asked again', (tester) async {
    final (state, backend) = await pump(tester, desktop);
    backend.noCheck = true;
    await openDnsSettings(tester);
    await type(tester, direct(), doh);
    await settle(tester);
    expect(state.dnsCheckUnsupported, isTrue);
    expect(find.byKey(const ValueKey('dns-check-direct')), findsNothing);
    expect(find.byKey(const ValueKey('dns-check-remote')), findsNothing);
    await type(tester, direct(), '1.1.1.1');
    await settle(tester);
    expect(backend.checks(), hasLength(1));
    expect(state.setting('dns.direct', ''), '1.1.1.1', reason: 'the setting is saved all the same');
    expect(tester.takeException(), isNull);
    await drain(tester);
  });

  const title = 'DNS напрямую недоступен';
  Event down() => Event(
    time: DateTime.now(),
    kind: 'dns-direct',
    reason: 'down',
    line: 'DNS напрямую (94.140.14.14) недоступен в этой сети (соединение отклонено) — использую DNS сети',
    code: 'dns.direct_down',
    args: {
      'addr': '94.140.14.14',
      'why': {'code': 'dns.why.refused'},
    },
  );

  testWidgets('the home page says the direct DNS is out of reach and leads to its settings', (tester) async {
    for (final size in [phone, desktop]) {
      final (state, backend) = await pump(tester, size);
      await connect(tester, state);
      expect(find.text(title), findsNothing, reason: 'before at $size');

      state.injectEvent(down());
      await tester.pump();
      expect(find.text(title), findsOneWidget, reason: 'after the event at $size');
      expect(find.textContaining('DNS напрямую (94.140.14.14) недоступен в этой сети (соединение отклонено) — использую DNS сети.'), findsOneWidget);
      expect(find.text('Настройки DNS'), findsOneWidget);
      expect(state.logs.last.message, startsWith('DNS напрямую (94.140.14.14) недоступен'));
      expect(tester.takeException(), isNull, reason: 'at $size');

      // «Не сейчас» puts it off without changing a thing.
      backend.calls.clear();
      await tester.ensureVisible(find.text('Не сейчас'));
      await tester.tap(find.text('Не сейчас'));
      await tester.pump();
      expect(find.text(title), findsNothing, reason: 'put off at $size');
      expect(backend.calls.where((c) => c.$1 != 'GET'), isEmpty);

      // When it answers again and fails again, it is told again.
      state.injectEvent(Event(time: DateTime.now(), kind: 'dns-direct', reason: 'restored', line: 'DNS напрямую (94.140.14.14) снова отвечает'));
      state.injectEvent(down());
      await tester.pump();
      expect(find.text(title), findsOneWidget, reason: 'again at $size');

      // The button opens the settings with the DNS fields in view.
      await tester.ensureVisible(find.text('Настройки DNS'));
      await tester.tap(find.text('Настройки DNS'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));
      expect(find.text('Настройки'), findsWidgets);
      expect(find.byKey(const ValueKey('dns-hint')), findsOneWidget, reason: 'the DNS fields are open at $size');
      expect(find.text('DNS напрямую'), findsOneWidget);
      expect(tester.takeException(), isNull, reason: 'settings at $size');

      // Back on the home page the restored resolver takes the note away.
      await tester.tap(nav('Главная'));
      await tester.pump();
      expect(find.text(title), findsOneWidget);
      state.injectEvent(Event(time: DateTime.now(), kind: 'dns-direct', reason: 'restored', line: 'DNS напрямую (94.140.14.14) снова отвечает'));
      await tester.pump();
      expect(find.text(title), findsNothing, reason: 'restored at $size');
      await disconnect(tester, state);
    }
  });

  testWidgets('the settings page opens folded when nobody sent the user to the DNS', (tester) async {
    final (state, _) = await pump(tester, desktop);
    await tester.tap(nav('Настройки'));
    await tester.pump();
    expect(find.byKey(const ValueKey('dns-hint')), findsNothing);
    expect(state.dnsSettingsRequested, isFalse);
    await drain(tester);
  });

  testWidgets('the status flag shows the note when the app opens on such a connection; a new connection starts without it', (tester) async {
    final (state, backend) = await pump(tester, phone);
    await connect(tester, state);
    backend.status = {
      'direct_dns': '94.140.14.14',
      'direct_dns_why': {'code': 'dns.why.timeout'},
    };
    state.injectEvent(Event(time: DateTime.now(), kind: 'options'));
    await settle(tester);
    expect(state.status.directDns, '94.140.14.14');
    expect(find.text(title), findsOneWidget);
    expect(find.textContaining('(нет ответа)'), findsOneWidget);

    backend.status = {};
    state.injectEvent(Event(time: DateTime.now(), kind: 'state', state: 'connecting'));
    state.injectEvent(Event(time: DateTime.now(), kind: 'options'));
    await settle(tester);
    expect(state.dnsFallbackHint, isFalse);
    await disconnect(tester, state);
  });

  testWidgets('on the demo network the blocked DNS comes up by itself a while after connecting', (tester) async {
    final (state, _) = await pump(tester, phone);
    await tester.runAsync(() => state.updateSettings((s) => s['dns']['direct'] = doh));
    expect(find.text(title), findsNothing);
    await connect(tester, state);
    // The demo says so two seconds after the connection came up.
    await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 600)));
    await tester.pump();
    expect(find.text(title), findsOneWidget);
    expect(find.textContaining('DNS напрямую (94.140.14.14) недоступен в этой сети (соединение отклонено)'), findsWidgets);
    expect(tester.takeException(), isNull);
    await disconnect(tester, state);
  });
}
