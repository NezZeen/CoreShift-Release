import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';

/// The traffic arrives every second while connected: it must redraw what
/// shows it, not every page.
void main() {
  const phone = Size(390, 844);
  const desktop = Size(1400, 900);

  Future<AppState> pumpApp(WidgetTester tester, Size size) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = AppState(DemoBackend());
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
      await state.connect();
    });
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    return state;
  }

  Future<void> finish(WidgetTester tester, AppState state) async {
    await tester.runAsync(() => state.disconnect());
    await tester.pump(const Duration(seconds: 6));
    // The traffic history the home page loads after disconnecting.
    await tester.pump(const Duration(seconds: 1));
  }

  /// Elements rebuilt by one traffic event, less those a frame without
  /// one rebuilds (an animation, such as a lamp's glow).
  Future<int> rebuildsPerTick(WidgetTester tester, AppState state) async {
    const frames = 10;
    Future<int> count(bool tick) async {
      var n = 0;
      debugOnRebuildDirtyWidget = (_, _) => n++;
      try {
        for (var i = 0; i < frames; i++) {
          if (tick) {
            state.injectEvent(Event(time: DateTime.now(), kind: 'traffic', up: 1000 * i, down: 9000 * i, upRate: 1000, downRate: 9000 + i));
          }
          await tester.pump(const Duration(milliseconds: 16));
        }
      } finally {
        debugOnRebuildDirtyWidget = null;
      }
      return n;
    }

    final idle = await count(false);
    final ticked = await count(true);
    return (ticked - idle) ~/ frames;
  }

  for (final (name, size) in [('desktop', desktop), ('phone', phone)]) {
    // Before the traffic had a notifier of its own, a tick rebuilt 387
    // (the desktop's home page) to 1048 (the phone's servers) elements.
    for (final (page, limit) in [('Главная', 70), ('Серверы', 0), ('Правила', 0), ('Настройки', 0)]) {
      testWidgets('a traffic tick on $page redraws only the traffic ($name)', (tester) async {
        final state = await pumpApp(tester, size);
        final nav = find.text(page);
        await tester.tap(nav.evaluate().length > 1 ? nav.last : nav.first);
        await tester.pump(const Duration(seconds: 30));
        final n = await rebuildsPerTick(tester, state);
        expect(n, lessThanOrEqualTo(limit), reason: '$n elements rebuilt per traffic event');
        await finish(tester, state);
      });
    }

    testWidgets('the speed follows the traffic ($name)', (tester) async {
      final state = await pumpApp(tester, size);
      await tester.pump(const Duration(seconds: 1));
      state.injectEvent(Event(time: DateTime.now(), kind: 'traffic', up: 1 << 20, down: 5 << 20, upRate: 1000, downRate: 4321000));
      await tester.pump();
      expect(find.textContaining('34.6 Мбит/с'), findsOneWidget);
      if (size == desktop) expect(find.byTooltip('Скачано и отправлено за это подключение'), findsOneWidget);
      state.injectEvent(Event(time: DateTime.now(), kind: 'traffic', up: 2 << 20, down: 9 << 20, upRate: 1000, downRate: 1234000));
      await tester.pump();
      expect(find.textContaining('34.6 Мбит/с'), findsNothing);
      expect(find.textContaining('9.9 Мбит/с'), findsOneWidget);
      await finish(tester, state);
    });
  }
}
