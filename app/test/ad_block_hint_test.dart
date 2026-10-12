import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';

void main() {
  Future<AppState> pumpRules(WidgetTester tester, Size size) async {
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
    final phone = find.byType(NavigationBar).evaluate().isNotEmpty;
    await tester.tap(phone ? find.descendant(of: find.byType(NavigationBar), matching: find.text('Правила')) : find.text('Правила').first);
    await tester.pump();
    return state;
  }

  // The ad block has no switch for now: it is on, and the rules page shows
  // no row for it in either mode, nor an empty panel of ready-made sets.
  for (final (name, size) in [('phone', Size(390, 844)), ('desktop', Size(1400, 900))]) {
    testWidgets('the ad block has no switch, and it is on ($name)', (tester) async {
      final state = await pumpRules(tester, size);
      expect(state.setting('routing.block_ads', false), isTrue);
      expect(find.text('Блокировать рекламу'), findsNothing);
      expect(find.textContaining('Рекламу внутри YouTube'), findsNothing);
      expect(find.text('Российские сайты напрямую'), findsOneWidget);
      await tester.runAsync(() => state.updateSettings((s) => s['routing']['mode'] = 'selected'));
      await tester.pump(const Duration(seconds: 1));
      expect(find.text('Блокировать рекламу'), findsNothing);
      expect(find.text('Готовые наборы'), findsNothing, reason: 'no empty panel in the «only selected» mode');
      expect(tester.takeException(), isNull, reason: 'no overflow at $size');
      await tester.pump(const Duration(seconds: 6));
    });
  }
}
