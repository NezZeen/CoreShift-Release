import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';

void main() {
  Future<void> pumpRules(WidgetTester tester, Size size) async {
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
  }

  // The hint says what ad blocking cannot reach, so it must be readable
  // under the switch on a phone too, where the other explanations hide
  // behind an «i».
  for (final (name, size) in [('phone', Size(390, 844)), ('desktop', Size(1400, 900))]) {
    testWidgets('the ad blocking hint sits under the switch ($name)', (tester) async {
      await pumpRules(tester, size);
      final title = find.text('Блокировать рекламу');
      expect(title, findsOneWidget);
      final hint = find.textContaining('Рекламу внутри YouTube, VK, Instagram');
      await tester.ensureVisible(hint);
      await tester.pump();
      expect(hint, findsOneWidget, reason: 'shown at $size');
      expect(tester.getTopLeft(hint).dy, greaterThan(tester.getTopLeft(title).dy), reason: 'under the title at $size');
      expect(tester.takeException(), isNull, reason: 'no overflow at $size');
      await tester.pump(const Duration(seconds: 6));
    });
  }
}
