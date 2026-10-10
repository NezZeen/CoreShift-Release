import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/countries.dart';

/// The home page's quick pick and the servers list with many servers: they
/// scroll by a finger on a row, by the wheel and (desktop) by a dragged
/// mouse, and picking a server in the quick pick connects to it.
void main() {
  const phone = Size(390, 844);
  const desktop = Size(1400, 900);

  Future<AppState> pumpApp(WidgetTester tester, {required Size size, bool stars = true}) async {
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
    if (stars) {
      for (final sub in state.subscriptions) {
        for (final n in sub.nodes) {
          state.toggleFavorite(sub, n);
        }
      }
      await tester.pump();
    }
    return state;
  }

  Future<void> openPick(WidgetTester tester, AppState state) async {
    await tester.tap(find.text(cleanNodeName(state.selection.name)).last);
    await tester.pumpAndSettle();
    expect(find.text('Выбор сервера'), findsOneWidget);
  }

  ScrollableState pickScroll(WidgetTester tester) =>
      tester.state<ScrollableState>(find.ancestor(of: find.text('Выбор сервера'), matching: find.byType(Scrollable)).first);

  Future<void> wheel(WidgetTester tester, Offset at, double dy) async {
    final mouse = TestPointer(1, PointerDeviceKind.mouse);
    await tester.sendEventToBinding(mouse.hover(at));
    await tester.sendEventToBinding(mouse.scroll(Offset(0, dy)));
    await tester.pump();
  }

  for (final (name, size) in [('phone', phone), ('desktop', desktop)]) {
    final isPhone = size == phone;
    final kind = isPhone ? PointerDeviceKind.touch : PointerDeviceKind.mouse;
    group(name, () {
      testWidgets('the quick pick scrolls by the wheel, and by a drag on a row', (tester) async {
        final state = await pumpApp(tester, size: size);
        await openPick(tester, state);
        final scroll = pickScroll(tester);
        expect(scroll.position.maxScrollExtent, greaterThan(100), reason: 'the favourites do not fit');
        final start = scroll.position.pixels;

        final bar = tester.widget<Scrollbar>(find.ancestor(of: find.text('Выбор сервера'), matching: find.byType(Scrollbar)).first);
        expect(bar.thumbVisibility, !isPhone, reason: 'the desktop shows the bar at once');
        await wheel(tester, tester.getCenter(find.text('Выбор сервера')) + const Offset(0, 200), 120);
        expect(scroll.position.pixels, greaterThan(start), reason: 'the wheel over the list scrolls it');

        final before = scroll.position.pixels;
        final row = find.text('Frankfurt');
        expect(row, findsOneWidget);
        await tester.drag(row, const Offset(0, -150), kind: kind);
        await tester.pump();
        expect(scroll.position.pixels, greaterThan(before), reason: 'a drag on a row scrolls the list');
        expect(tester.takeException(), isNull);
      });

      testWidgets('a server picked in the quick pick is connected to, and switched to while connected', (tester) async {
        final state = await pumpApp(tester, size: size);
        expect(state.status.state, ConnState.idle);
        expect(state.online, isTrue);
        await openPick(tester, state);
        await tester.ensureVisible(find.text('Tokyo'));
        await tester.pump();
        await tester.tap(find.text('Tokyo'));
        await tester.pumpAndSettle();
        expect(find.text('Выбор сервера'), findsNothing, reason: 'the pick closes');
        for (var i = 0; i < 100 && state.status.state != ConnState.connected; i++) {
          await tester.pump(const Duration(milliseconds: 100));
        }
        // The daemon tells the app of the selection a moment after.
        await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 300)));
        await tester.pump(const Duration(seconds: 1));
        expect(state.status.state, ConnState.connected, reason: 'the pick connects');
        expect(cleanNodeName(state.status.node), 'Tokyo');
        expect(cleanNodeName(state.selection.name), 'Tokyo');

        await openPick(tester, state);
        await tester.ensureVisible(find.text('London'));
        await tester.pump();
        await tester.tap(find.text('London'));
        await tester.pumpAndSettle();
        for (var i = 0; i < 100 && cleanNodeName(state.status.node) != 'London'; i++) {
          await tester.pump(const Duration(milliseconds: 100));
        }
        await tester.pump();
        expect(state.status.state, ConnState.connected);
        expect(cleanNodeName(state.status.node), 'London', reason: 'the pick switches the connection');
        expect(tester.takeException(), isNull);
        state.disconnect();
        await tester.pump(const Duration(seconds: 2));
        await tester.pump(const Duration(seconds: 1));
      });

      testWidgets('the servers list scrolls by the wheel and by a drag on a row', (tester) async {
        final state = await pumpApp(tester, size: size, stars: false);
        await tester.tap(isPhone ? find.descendant(of: find.byType(NavigationBar), matching: find.text('Серверы')) : find.text('Серверы').first);
        await tester.pump();
        await tester.pump(const Duration(seconds: 30));
        final scroll = tester.state<ScrollableState>(find.byType(Scrollable).first);
        expect(scroll.position.maxScrollExtent, greaterThan(100));
        final first = scroll.position.pixels;
        await wheel(tester, tester.getCenter(find.text('Frankfurt')), 120);
        expect(scroll.position.pixels, greaterThan(first));
        final before = scroll.position.pixels;
        await tester.drag(find.text('Falkenstein'), const Offset(0, -150), kind: kind);
        await tester.pump();
        expect(scroll.position.pixels, greaterThan(before), reason: 'a drag on a row scrolls the page');
        expect(state.loaded, isTrue);
        await tester.pump(const Duration(seconds: 1));
        expect(tester.takeException(), isNull);
      });
    });
  }
}
