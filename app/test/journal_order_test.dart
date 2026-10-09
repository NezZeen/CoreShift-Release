import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/version.dart';

void main() {
  Future<AppState> pumpJournal(WidgetTester tester, AppState state, {Size size = const Size(1400, 900)}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    final phone = find.byType(NavigationBar).evaluate().isNotEmpty;
    await tester.tap(phone ? find.descendant(of: find.byType(NavigationBar), matching: find.text('Журнал')) : find.text('Журнал').first);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 600));
    return state;
  }

  for (final (form, size) in [('desktop', const Size(1400, 900)), ('phone', const Size(390, 844))]) {
    testWidgets('the update notice sits at its time, after what the service replays ($form)', (tester) async {
      // The app notes the update as it starts; the service then replays the
      // session of three hours ago (the demo's), which came first.
      final state = AppState(DemoBackend(), prefs: {'last_version': '0.6.8+1+abc'}, version: const BuildVersion('0.7.0', 2, 'def'));
      await pumpJournal(tester, state, size: size);

      final times = state.logs.map((l) => l.time).toList();
      for (var i = 1; i < times.length; i++) {
        expect(times[i].isBefore(times[i - 1]), isFalse, reason: 'line $i is older than the one above it');
      }
      expect(state.logs.last.message, 'CoreShift обновлён: 0.6.8 → 0.7.0');

      // On the page: the top of the journal is the earlier session, and
      // the notice, where it is laid out at all, is below it.
      tester.state<ScrollableState>(find.byType(Scrollable).last).position.jumpTo(0);
      await tester.pump();
      final journal = find.byType(SelectionArea);
      final earlier = find.descendant(of: journal, matching: find.textContaining('подключение…', findRichText: true));
      final update = find.descendant(of: journal, matching: find.textContaining('CoreShift обновлён', findRichText: true));
      expect(earlier, findsWidgets);
      if (update.evaluate().isNotEmpty) expect(tester.getTopLeft(update.first).dy, greaterThan(tester.getTopLeft(earlier.first).dy));
      await tester.pump(const Duration(seconds: 6));
    });
  }

  test('an event replayed again is in the journal once', () {
    final state = AppState(DemoBackend());
    addTearDown(state.dispose);
    final t = DateTime(2026, 10, 4, 4, 1, 55, 123, 456);
    final e = Event(time: t, kind: 'log', source: 'tun', line: 'ERROR network: missing default interface');
    state.injectEvent(e);
    state.injectEvent(Event(time: t.add(const Duration(seconds: 1)), kind: 'state', state: 'connected', core: 'xray'));
    // The event stream reconnected: the service replays what it keeps.
    state.injectEvent(e);
    state.injectEvent(Event(time: t.add(const Duration(seconds: 1)), kind: 'state', state: 'connected', core: 'xray'));
    expect(state.logs, hasLength(2));
    expect(state.logs.first.message, 'ERROR network: missing default interface');
    expect(state.logs.last.message, startsWith('подключено: '));
    // Another line at the same moment is another line.
    state.injectEvent(Event(time: t, kind: 'log', source: 'mihomo', line: 'ERROR network: missing default interface'));
    expect(state.logs, hasLength(3));
  });

  testWidgets('the cores\' errors and warnings are on the page, their other output is not', (tester) async {
    final state = await pumpJournal(tester, AppState(DemoBackend()));
    final t = DateTime.now();
    void log(String source, String line) => state.injectEvent(Event(time: t, kind: 'log', source: source, line: line));
    log(
      'sing-box',
      'ERROR [902621915 4ms] connection: open connection to cp.cloudflare.com:80 using outbound/vless[proxy]: dial tcp 203.0.113.181:443: refused',
    );
    log('mihomo', 'WARN [TCP] dial proxy (match Match/) 127.0.0.1:51790 --> cp.cloudflare.com:80 error: connect error');
    log('xray', 'INFO app/dispatcher: taking detour [proxy]');
    log('xray', 'WARN core: Xray 26.3.27 started');
    // From an older service, as the cores printed them.
    log('xray', '2026/10/03 12:26:51.000000 [Warning] transport/internet/splithttp: slow response');
    log('mihomo', 'time="2026-10-04T04:02:05+03:00" level=error msg="[TCP] broken"');
    state.injectEvent(Event(time: t, kind: 'state', state: 'idle')); // redraws now
    for (var i = 0; i < 6; i++) {
      await tester.pump(const Duration(milliseconds: 16));
    }

    LogLevel level(String part) => state.logs.lastWhere((l) => l.message.contains(part)).level;
    expect(level('open connection to cp.cloudflare.com'), LogLevel.err);
    expect(level('dial proxy (match Match/)'), LogLevel.warn);
    expect(level('taking detour'), LogLevel.info);
    expect(level('Xray 26.3.27 started'), LogLevel.info);
    expect(level('slow response'), LogLevel.warn);
    expect(level('[TCP] broken'), LogLevel.err);

    for (final shown in ['open connection to cp.cloudflare.com', 'dial proxy (match Match/)', 'slow response', '[TCP] broken']) {
      expect(find.textContaining(shown), findsOneWidget, reason: shown);
    }
    for (final hidden in ['taking detour', 'Xray 26.3.27 started']) {
      expect(find.textContaining(hidden), findsNothing, reason: hidden);
    }
    // The errors filter has them too.
    await tester.tap(find.text('Ошибки'));
    for (var i = 0; i < 6; i++) {
      await tester.pump(const Duration(milliseconds: 16));
    }
    expect(find.textContaining('open connection to cp.cloudflare.com'), findsOneWidget);
    expect(find.textContaining('dial proxy (match Match/)'), findsOneWidget);
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('a full journal follows its end', (tester) async {
    final state = await pumpJournal(tester, AppState(DemoBackend()));
    final t0 = DateTime.now().subtract(const Duration(hours: 1));
    for (var i = 0; i < AppState.logsKeep + 500; i++) {
      state.injectEvent(
        Event(
          time: t0.add(Duration(seconds: i)),
          kind: 'log',
          source: 'tun',
          line: 'ERROR line $i ${'x' * (i % 7 * 30)}',
        ),
      );
    }
    state.injectEvent(Event(time: DateTime.now(), kind: 'state', state: 'idle'));
    expect(state.logs, hasLength(AppState.logsKeep));
    for (var i = 0; i < 6; i++) {
      await tester.pump(const Duration(milliseconds: 16));
    }
    expect(find.textContaining('отключено'), findsWidgets);
    final pos = tester.state<ScrollableState>(find.byType(Scrollable).last).position;
    expect(pos.pixels, pos.maxScrollExtent);
    await tester.pump(const Duration(seconds: 6));
  });
}
