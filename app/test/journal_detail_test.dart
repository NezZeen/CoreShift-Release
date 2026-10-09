import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';

/// The journal tells each connection whole: with what it connected, and,
/// when it ends, how long it lasted and how much went through it; which
/// network it runs over; what a check for updates found, also nothing.
void main() {
  Future<AppState> pumpApp(WidgetTester tester, {Size size = const Size(1400, 900)}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = AppState(DemoBackend());
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

  // The demo replays a session of hours ago first: only what came since t.
  late DateTime since;
  Iterable<String> lines(AppState s) => s.logs.where((l) => !l.time.isBefore(since)).map((l) => l.message);

  testWidgets('a connection says with what it connected, and sums itself up when it ends', (tester) async {
    final state = await pumpApp(tester);
    final t0 = since = DateTime.now();
    state.injectEvent(Event(time: t0, kind: 'state', state: 'connecting'));
    state.injectEvent(Event(time: t0.add(const Duration(milliseconds: 1400)), kind: 'state', state: 'connected', core: 'sing-box'));
    final connected = lines(state).lastWhere((l) => l.startsWith('подключено'));
    expect(connected, startsWith('подключено за 1.4 с: «${state.selection.name}»'));
    expect(connected, contains('sing-box'));
    expect(connected, contains('через VPN'));

    state.injectEvent(Event(time: t0.add(const Duration(minutes: 30)), kind: 'traffic', up: 120000000, down: 1800000000));
    state.injectEvent(Event(time: t0.add(const Duration(hours: 2, minutes: 15)), kind: 'state', state: 'idle'));
    final summary = lines(state).lastWhere((l) => l.startsWith('сессия:'));
    expect(summary, 'сессия: 2 ч 14 мин · ↓ 1.8 ГБ ↑ 120 МБ · «${state.selection.name}», sing-box');
    // Summed up once: another idle says nothing more.
    state.injectEvent(Event(time: t0.add(const Duration(hours: 3)), kind: 'state', state: 'idle'));
    expect(lines(state).where((l) => l.startsWith('сессия:')), hasLength(1));
    // The event stream reconnected: the service replays the connection and
    // its end, with the traffic it kept; still one summary.
    state.injectEvent(Event(time: t0.add(const Duration(milliseconds: 1400)), kind: 'state', state: 'connected', core: 'sing-box'));
    state.injectEvent(Event(time: t0.add(const Duration(hours: 2)), kind: 'traffic', up: 130000000, down: 1900000000));
    state.injectEvent(Event(time: t0.add(const Duration(hours: 2, minutes: 15)), kind: 'state', state: 'idle'));
    expect(lines(state).where((l) => l.startsWith('сессия:')), hasLength(1));
    await tester.pump(const Duration(seconds: 30));
  });

  testWidgets('a reconnect sums up the connection before it; held for a network it goes on', (tester) async {
    final state = await pumpApp(tester);
    final t0 = since = DateTime.now();
    state.injectEvent(Event(time: t0, kind: 'state', state: 'connected', core: 'xray'));
    state.injectEvent(Event(time: t0.add(const Duration(minutes: 5)), kind: 'state', state: 'no-network'));
    expect(lines(state).where((l) => l.startsWith('сессия:')), isEmpty);
    state.injectEvent(Event(time: t0.add(const Duration(minutes: 6)), kind: 'state', state: 'connecting'));
    expect(lines(state).where((l) => l.startsWith('сессия: 6 мин')), hasLength(1));
    await tester.pump(const Duration(seconds: 30));
  });

  testWidgets('the network a connection runs over, and a move to another, are in the journal', (tester) async {
    final state = await pumpApp(tester);
    final t0 = DateTime.now();
    state.injectEvent(Event(time: t0, kind: 'netinfo', line: 'сеть: Wi-Fi (wlan0) · IPv6 есть · DNS сети: 2'));
    state.injectEvent(
      Event(time: t0.add(const Duration(seconds: 1)), kind: 'netinfo', reason: 'changed', line: 'сеть сменилась: Wi-Fi (wlan0) → мобильная сеть (rmnet0)'),
    );
    final net = state.logs.where((l) => l.source == 'сеть').toList();
    expect(net.map((l) => l.message), contains('сеть: Wi-Fi (wlan0) · IPv6 есть · DNS сети: 2'));
    expect(net.last.level, LogLevel.swap);
    await tester.pump(const Duration(seconds: 30));
  });

  test('a check of the cores says what it found, also nothing', () {
    expect(
      AppStateActions.coreCheckText(const [
        CoreUpdate(kind: 'xray', current: '26.3.27', latest: '26.3.27'),
        CoreUpdate(kind: 'sing-box', current: '1.14.3', latest: '1.14.3'),
      ]),
      'проверено: новых версий нет (Xray-core 26.3.27, sing-box 1.14.3)',
    );
    expect(
      AppStateActions.coreCheckText(const [
        CoreUpdate(kind: 'sing-box', current: '1.14.3', latest: '1.14.4', available: true),
        CoreUpdate(kind: 'mihomo', current: '1.19.32', latest: '1.19.32'),
      ]),
      'найдены новые версии: sing-box 1.14.4 (сейчас 1.14.3)',
    );
    // Nothing checked: the failures are told elsewhere.
    expect(AppStateActions.coreCheckText(const [CoreUpdate(kind: 'xray', error: 'no network')]), isNull);
  });

  testWidgets('«Подробно» turns the verbose journal on and off', (tester) async {
    final state = await pumpApp(tester);
    await tester.tap(find.text('Журнал').first);
    await tester.pump(const Duration(milliseconds: 300));
    expect(state.setting('log.verbose', true), isFalse);
    await tester.tap(find.text('Подробно'));
    for (var i = 0; i < 5; i++) {
      await tester.pump(const Duration(milliseconds: 100));
    }
    expect(state.setting('log.verbose', false), isTrue);
    expect(find.textContaining('Подробный журнал включён'), findsOneWidget);
    await tester.pump(const Duration(seconds: 30));
  });
}
