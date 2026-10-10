import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/pages/proxy_mode.dart';
import 'package:coreshift/ui/theme.dart';

Future<AppState> _loaded(WidgetTester tester) async {
  final state = AppState(DemoBackend());
  await tester.runAsync(() async {
    state.start();
    for (var i = 0; i < 50 && !state.loaded; i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
  });
  return state;
}

Future<void> _setTun(WidgetTester tester, AppState state, bool on) async {
  await tester.runAsync(() => state.updateSettings((s) => s['tun'] = on));
  await tester.pump();
}

void main() {
  // The desktop: «Системный прокси» shows once «Все приложения через VPN»
  // is off, and turns the setting on.
  testWidgets('the system proxy switch on a computer', (tester) async {
    tester.view.physicalSize = const Size(1400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = await _loaded(tester);
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    await tester.tap(find.text('Настройки').first);
    await tester.pump();
    expect(find.text('Системный прокси'), findsNothing, reason: 'with TUN on it means nothing');

    await _setTun(tester, state, false);
    final row = find.ancestor(of: find.text('Системный прокси'), matching: find.byType(Row)).first;
    expect(find.textContaining('CoreShift сам пропишет прокси в системе'), findsOneWidget);
    final sw = find.descendant(of: row, matching: find.byType(Switch));
    expect(tester.widget<Switch>(sw).value, isFalse);
    await tester.tap(sw);
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
    expect(state.setting('proxy.system', false), isTrue);
    expect(tester.widget<Switch>(sw).value, isTrue);
    expect(tester.takeException(), isNull);
  });

  Future<AppState> pumpPhone(WidgetTester tester, {required bool tun}) async {
    tester.view.physicalSize = const Size(375, 812);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = await _loaded(tester);
    await tester.runAsync(() => state.updateSettings((s) => s['tun'] = tun));
    await tester.pumpWidget(
      MaterialApp(
        theme: buildTheme(Brightness.dark),
        home: Scaffold(
          body: ListenableBuilder(
            listenable: state,
            builder: (_, _) => ListView(
              padding: const EdgeInsets.all(12),
              children: [
                ProxyOnlyRow(state: state, first: true),
                if (!state.setting('tun', true)) ProxyAccessCard(state: state),
              ],
            ),
          ),
        ),
      ),
    );
    await tester.pump();
    return state;
  }

  // Android, the VPN as it was: the switch is off and nothing else shows.
  testWidgets('the proxy without the VPN on a phone, off', (tester) async {
    final state = await pumpPhone(tester, tun: true);
    expect(find.text('Прокси без VPN'), findsOneWidget);
    expect(tester.widget<Switch>(find.byType(Switch)).value, isFalse);
    expect(find.byKey(const ValueKey('proxy-access')), findsNothing);
    await tester.tap(find.byType(Switch));
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
    expect(state.setting('tun', true), isFalse, reason: 'the proxy without the VPN is TUN off');
    expect(find.byKey(const ValueKey('proxy-access')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  // On: what apps need, the password hidden until asked for, Telegram's
  // button, and the HTTP door behind its warning.
  testWidgets('the proxy without the VPN on a phone, on', (tester) async {
    final state = await pumpPhone(tester, tun: false);
    expect(find.byKey(const ValueKey('proxy-access')), findsOneWidget);
    expect(find.text('127.0.0.1'), findsOneWidget);
    expect(find.text('17890'), findsOneWidget);
    expect(find.text('csdemo7'), findsOneWidget);
    expect(find.text('Demo7Passw0rdNotReal'), findsNothing, reason: 'the password is hidden');
    await tester.tap(find.byTooltip('Показать'));
    await tester.pump();
    expect(find.text('Demo7Passw0rdNotReal'), findsOneWidget);
    expect(find.text('Открыть в Telegram'), findsOneWidget);
    expect(find.textContaining('только HTTP без пароля'), findsOneWidget);
    expect(find.byTooltip('Скопировать'), findsNWidgets(4));

    // The card's own switch, after «Прокси без VPN».
    final http = find.byType(Switch).last;
    expect(tester.widget<Switch>(http).value, isFalse);
    await tester.ensureVisible(http);
    await tester.tap(http);
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
    expect(state.setting('proxy.open_http', false), isTrue);
    expect(find.textContaining('любое приложение на телефоне может пользоваться прокси'), findsOneWidget);

    // New credentials replace both.
    await tester.ensureVisible(find.text('Новый пароль'));
    await tester.tap(find.text('Новый пароль'));
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
    expect(state.setting('proxy.user', ''), isNot('csdemo7'));
    expect(state.setting('proxy.pass', ''), hasLength(20));
    expect(tester.takeException(), isNull);
  });

  test('the Telegram link carries the proxy', () {
    expect(telegramProxyLink(port: 17890, user: 'csab12cd', pass: 'Pa55+/&='), 'tg://socks?server=127.0.0.1&port=17890&user=csab12cd&pass=Pa55%2B%2F%26%3D');
    final (u, p) = newProxyCredentials();
    expect(u, matches(RegExp(r'^cs[a-z0-9]{6}$')));
    expect(p, matches(RegExp(r'^[A-Za-z2-9]{20}$')));
  });
}
