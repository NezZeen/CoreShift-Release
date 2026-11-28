import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/widgets.dart';

void main() {
  test('the engine\'s blocked state is an active connection the button can end', () {
    final st = Status.fromJson({'state': 'blocked', 'tun': true, 'error': 'every compatible core failed'});
    expect(st.state, ConnState.blocked);
    expect(st.active, isTrue);
  });

  testWidgets('«Блокировать трафик при сбое» is a switch in the settings that the engine stores', (tester) async {
    tester.view.physicalSize = const Size(1400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = AppState(DemoBackend());
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 750 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    expect(state.setting('kill_switch', true), isFalse, reason: 'off until the user turns it on');

    await tester.tap(find.text('Настройки').first);
    await tester.pump(const Duration(seconds: 1));
    final title = find.text('Блокировать трафик при сбое');
    await tester.ensureVisible(title);
    // Brought to the very top it sits under the page's header: scroll back a little.
    await tester.drag(title, const Offset(0, 300));
    await tester.pump(const Duration(seconds: 1));
    final row = find.ancestor(of: title, matching: find.byType(SettingRow)).first;
    await tester.tap(find.descendant(of: row, matching: find.byType(Switch)));
    // The demo daemon answers after a pause of its own, on the test's clock.
    await tester.pump(const Duration(seconds: 2));
    expect(state.setting('kill_switch', false), isTrue);
    expect(tester.takeException(), isNull);
  });
}
