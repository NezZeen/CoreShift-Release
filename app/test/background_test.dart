import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';

/// Without a desktop window (Android) the app in the background counts as
/// hidden: the traffic waits, nothing animates, and the service is told so
/// it leaves the speed out of the app's events. Back in front, the speed
/// is brought up to date at once.
void main() {
  testWidgets('in the background the app is hidden; back, it is shown and catches up', (tester) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final backend = DemoBackend();
    final state = AppState(backend);
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
      await state.connect();
      await Future.delayed(const Duration(milliseconds: 300));
      backend.quiet();
    });
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    expect(state.shown.value, isTrue);

    var trafficTicks = 0;
    void onTraffic() => trafficTicks++;
    state.traffic.addListener(onTraffic);

    final binding = tester.binding;
    for (final s in [AppLifecycleState.inactive, AppLifecycleState.hidden, AppLifecycleState.paused]) {
      binding.handleAppLifecycleStateChanged(s);
    }
    await tester.pump();
    expect(state.shown.value, isFalse, reason: 'the app in the background is still shown');

    for (var i = 1; i <= 3; i++) {
      state.injectEvent(Event(time: DateTime.now(), kind: 'traffic', up: 1000 * i, down: 9000 * i, upRate: 1000, downRate: 4321000));
    }
    await tester.pump();
    expect(trafficTicks, 0, reason: 'the traffic rebuilt the hidden app');

    for (final s in [AppLifecycleState.hidden, AppLifecycleState.inactive, AppLifecycleState.resumed]) {
      binding.handleAppLifecycleStateChanged(s);
    }
    await tester.pump();
    expect(state.shown.value, isTrue);
    expect(trafficTicks, 1, reason: 'back in front, the speed is brought up to date at once');

    state.traffic.removeListener(onTraffic);
    await tester.runAsync(() => state.disconnect());
    await tester.pump(const Duration(seconds: 6));
    await tester.pump(const Duration(seconds: 1));
  });
}
