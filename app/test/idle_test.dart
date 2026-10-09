import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/pages/home_page.dart';
import 'package:coreshift/ui/theme.dart';

/// An idle window draws nothing: no animation goes on for as long as the
/// VPN is on, and a window hidden in the tray or minimized draws nothing at
/// all, the traffic and the clock included. On Linux the window used about
/// a third of a core all along before.
void main() {
  const desktop = Size(1400, 900);

  Future<AppState> pumpApp(WidgetTester tester, {bool connect = true}) async {
    tester.view.physicalSize = desktop;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final backend = DemoBackend();
    final state = AppState(backend);
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
      if (connect) await state.connect();
      // The status the connection's events ask for comes a moment later.
      await Future.delayed(const Duration(milliseconds: 300));
      // Only the events a test sends: the demo's own come in real time,
      // at any moment of the test.
      backend.quiet();
    });
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    return state;
  }

  Future<void> finish(WidgetTester tester, AppState state) async {
    if (state.status.active) await tester.runAsync(() => state.disconnect());
    await tester.pump(const Duration(seconds: 6));
    await tester.pump(const Duration(seconds: 1));
  }

  /// Lets what the start set going end: a frame a second for 12 seconds,
  /// which the ripples of a fresh connection take (3 × 2.4 s).
  Future<void> settle(WidgetTester tester) async {
    await tester.pump(const Duration(seconds: 30));
    for (var i = 0; i < 12; i++) {
      await tester.pump(const Duration(seconds: 1));
    }
  }

  /// Elements rebuilt while [d] passes, a frame every 16 ms as a screen
  /// asks for them.
  Future<int> rebuildsDuring(WidgetTester tester, Duration d, {void Function(int frame)? each}) async {
    var n = 0;
    debugOnRebuildDirtyWidget = (_, _) => n++;
    try {
      for (var i = 0; i < d.inMilliseconds ~/ 16; i++) {
        each?.call(i);
        await tester.pump(const Duration(milliseconds: 16));
      }
    } finally {
      debugOnRebuildDirtyWidget = null;
    }
    return n;
  }

  testWidgets('connected and left alone, the window asks for no frames', (tester) async {
    final state = await pumpApp(tester);
    expect(state.status.state, ConnState.connected);
    // The ripples of a fresh connection go out, then the rings rest.
    await settle(tester);
    expect(tester.binding.hasScheduledFrame, isFalse, reason: 'an animation keeps drawing');
    // The connection's clock still ticks, by its timer: it reads the real
    // clock, which a test's pumps do not keep in step with, so its pace is
    // left to connected_time_test.dart.
    await finish(tester, state);
  });

  testWidgets('disconnected and left alone, the window asks for no frames', (tester) async {
    final state = await pumpApp(tester, connect: false);
    await settle(tester);
    expect(tester.binding.hasScheduledFrame, isFalse, reason: 'an animation keeps drawing');
    expect(await rebuildsDuring(tester, const Duration(seconds: 2)), 0);
    await finish(tester, state);
  });

  // One theme for every pump: a new one would animate the change.
  final theme = buildTheme(Brightness.dark);
  Widget button(ConnState st, {bool ticking = true}) => MaterialApp(
    theme: theme,
    home: Scaffold(
      body: Center(
        child: TickerMode(
          enabled: ticking,
          child: ConnectButton(state: st, enabled: true, onTap: () {}),
        ),
      ),
    ),
  );

  testWidgets('the rings move while connecting, ripple a few times once connected, then rest', (tester) async {
    await tester.pumpWidget(button(ConnState.idle));
    await tester.pump(const Duration(seconds: 1));
    expect(tester.binding.hasScheduledFrame, isFalse, reason: 'idle, the rings move');

    await tester.pumpWidget(button(ConnState.connecting));
    await tester.pump(const Duration(seconds: 10));
    expect(tester.binding.hasScheduledFrame, isTrue, reason: 'connecting, the arc stands still');
    // In a hidden window (its tickers off) not even that.
    await tester.pumpWidget(button(ConnState.connecting, ticking: false));
    await tester.pump(const Duration(milliseconds: 100));
    expect(tester.binding.hasScheduledFrame, isFalse, reason: 'a hidden window keeps drawing');
    await tester.pumpWidget(button(ConnState.connecting));
    await tester.pump(const Duration(milliseconds: 100));
    expect(tester.binding.hasScheduledFrame, isTrue);

    await tester.pumpWidget(button(ConnState.connected));
    // The ripples: 2.4 s each, frame by frame.
    Future<void> frames(int ms) async {
      for (var t = 0; t < ms; t += 100) {
        await tester.pump(const Duration(milliseconds: 100));
      }
    }

    await frames(2400 * ConnectButton.ripples - 500);
    expect(tester.binding.hasScheduledFrame, isTrue, reason: 'the ripples stopped early');
    await frames(1000);
    expect(tester.binding.hasScheduledFrame, isFalse, reason: 'the ripples go on for as long as the VPN is on');

    // The network lost: the arc turns again.
    await tester.pumpWidget(button(ConnState.noNetwork));
    await tester.pump(const Duration(milliseconds: 100));
    expect(tester.binding.hasScheduledFrame, isTrue);
    // Off: once the colours have blended (350 ms), nothing moves.
    await tester.pumpWidget(button(ConnState.idle));
    await frames(500);
    expect(tester.binding.hasScheduledFrame, isFalse);
  });

  testWidgets('hidden, the traffic and the clock wait; shown, they catch up', (tester) async {
    final state = await pumpApp(tester);
    await settle(tester);
    var trafficTicks = 0, speedTicks = 0;
    void onTraffic() => trafficTicks++;
    void onSpeed() => speedTicks++;
    state.traffic.addListener(onTraffic);
    state.speedNow.addListener(onSpeed);

    state.setShown(false);
    await tester.pump();
    final samples = state.speed.length;
    final hidden = await rebuildsDuring(
      tester,
      const Duration(seconds: 3),
      each: (i) {
        if (i % 60 == 0) state.injectEvent(Event(time: DateTime.now(), kind: 'traffic', up: 1000 * i, down: 9000 * i, upRate: 1000, downRate: 4321000));
      },
    );
    expect(hidden, 0, reason: 'a hidden window rebuilt $hidden elements');
    expect(trafficTicks, 0);
    expect(speedTicks, greaterThan(0), reason: "the tray's tooltip must still follow the speed");
    // The graph keeps the latest of the seldom samples, not all of them.
    expect(state.speed.length, lessThanOrEqualTo(samples + 1));
    expect(state.speed.last.$2, 4321000);

    state.setShown(true);
    await tester.pump();
    expect(trafficTicks, 1, reason: 'shown again, the speed is brought up to date at once');
    expect(find.textContaining('34.6 Мбит/с'), findsOneWidget);
    state.traffic.removeListener(onTraffic);
    state.speedNow.removeListener(onSpeed);
    await finish(tester, state);
  });
}
