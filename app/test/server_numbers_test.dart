import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';

/// The servers list's numbers (the place in the subscription's own order)
/// and the way a latency was measured: the switch, the defaults, the sort,
/// and the short label of a port check or a ping.
void main() {
  const phone = Size(390, 844);
  const desktop = Size(1400, 900);
  const tcpTip = 'Только проверка, что порт сервера открыт: через сервер не проверялось';

  /// A test whose app timers run out before the test ends.
  void appTest(String name, WidgetTesterCallback body) => testWidgets(name, (tester) async {
    await body(tester);
    await tester.pump(const Duration(seconds: 2));
  });

  Future<AppState> pumpApp(WidgetTester tester, {required Size size, bool android = false}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = AppState(DemoBackend(android: android));
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    // The page tests the latency itself on its first open; these tests do it
    // when they want to.
    state.latencyAutoTested = true;
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    return state;
  }

  /// Taps [target], scrolling it into view first: the phone's sort chip is
  /// below the subscriptions.
  Future<void> tap(WidgetTester tester, Finder target) async {
    await tester.ensureVisible(target);
    await tester.pump();
    await tester.tap(target);
    await tester.pumpAndSettle();
  }

  Future<void> openServers(WidgetTester tester, bool isPhone) async {
    await tap(tester, isPhone ? find.descendant(of: find.byType(NavigationBar), matching: find.text('Серверы')) : find.text('Серверы').first);
  }

  /// The number shown before a server's row, found by the server itself.
  Finder numberOf(Subscription sub, NodeView n) => find.byKey(ValueKey('server-number/${sub.id}/${n.fingerprint}'));

  Finder allNumbers() => find.byWidgetPredicate((w) => w.key is ValueKey<String> && (w.key as ValueKey<String>).value.startsWith('server-number/'));

  /// A phone's sort chip opens the sort menu, which holds the switch «Номера».
  Future<void> openSortMenu(WidgetTester tester) => tap(tester, find.byTooltip('Порядок серверов'));

  /// The sort, chosen the way the size shows it: the segments on a computer,
  /// the chip's menu on a phone.
  Future<void> chooseSort(WidgetTester tester, bool isPhone, String label) async {
    if (isPhone) {
      await openSortMenu(tester);
      // The menu is an overlay: its items need no scrolling into view. The
      // sort's result is checked by the test, so a missed tap would show.
      await tester.tap(find.text(label), warnIfMissed: false);
      await tester.pumpAndSettle();
    } else {
      await tap(tester, find.text(label).last);
    }
  }

  for (final (name, size) in [('phone', phone), ('desktop', desktop)]) {
    final isPhone = size == phone;
    group(name, () {
      appTest('every server has its number, with no switch to hide them', (tester) async {
        final state = await pumpApp(tester, size: size);
        await openServers(tester, isPhone);
        final total = state.subscriptions.fold(0, (sum, sub) => sum + sub.nodes.length);
        expect(allNumbers(), findsNWidgets(total), reason: 'every server has its number');
        final sub = state.subscriptions.first;
        expect(tester.widget<Text>(numberOf(sub, sub.nodes.first)).data, '1');
        expect(find.text('Номера'), findsNothing, reason: 'no chip on a computer');
        if (isPhone) {
          await openSortMenu(tester);
          expect(find.text('Номера'), findsNothing, reason: 'no item in the sort menu');
          await tester.tapAt(const Offset(10, 10));
          await tester.pumpAndSettle();
        }
        expect(tester.takeException(), isNull);
      });

      appTest('the rows have no radio circle: the selected server shows by its frame', (tester) async {
        await pumpApp(tester, size: size);
        await openServers(tester, isPhone);
        // The radio was a circle 18 points wide with a border; none is left.
        final circles = find.byWidgetPredicate((w) {
          if (w is! Container || w.decoration is! BoxDecoration) return false;
          final d = w.decoration as BoxDecoration;
          return d.shape == BoxShape.circle && d.border != null && w.constraints?.maxWidth == 18;
        });
        expect(circles, findsNothing);
        expect(tester.takeException(), isNull);
      });

      appTest('a number stays with its server when the list is sorted by ping', (tester) async {
        final state = await pumpApp(tester, size: size);
        await tester.runAsync(() => state.testLatency());
        await openServers(tester, isPhone);
        await chooseSort(tester, isPhone, 'По пингу');
        expect(state.serverSort, 'ping');

        // Every number is the server's place in its subscription.
        for (final sub in state.subscriptions) {
          for (final (i, n) in sub.nodes.indexed) {
            expect(tester.widget<Text>(numberOf(sub, n)).data, '${i + 1}', reason: '${n.name} keeps its place');
          }
        }

        // The fastest answering server is the first row, with the number it
        // has in its subscription.
        String keyOf(Subscription sub, NodeView n) => '${sub.id}/${n.fingerprint}';
        final fastest = <String>{};
        var best = 1 << 30;
        for (final sub in state.subscriptions) {
          for (final n in sub.nodes) {
            final l = state.latencyOf(sub.id, n.fingerprint);
            if (l == null || !l.ok) continue;
            if (l.ms < best) {
              best = l.ms;
              fastest.clear();
            }
            if (l.ms == best) fastest.add(keyOf(sub, n));
          }
        }
        expect(fastest, isNotEmpty, reason: 'the test measured some servers');
        final tops = <(String, double)>[];
        for (final sub in state.subscriptions) {
          for (final n in sub.nodes) {
            tops.add((keyOf(sub, n), tester.getTopLeft(numberOf(sub, n)).dy));
          }
        }
        tops.sort((a, b) => a.$2.compareTo(b.$2));
        expect(fastest, contains(tops.first.$1), reason: 'the fastest server is at the top');
        expect(tester.takeException(), isNull);
      });

      appTest('a test through the server has no label', (tester) async {
        // The computer's demo tests through the server: nothing to say about it.
        final state = await pumpApp(tester, size: size);
        await tester.runAsync(() => state.testLatency());
        await openServers(tester, isPhone);
        expect(find.text('TCP'), findsNothing);
        expect(find.text('ping'), findsNothing);
        expect(tester.takeException(), isNull);
      });

      appTest('a port check is labelled TCP and a ping ping, with the tooltip on a computer only', (tester) async {
        // The phone's demo: a port check for most servers, a ping for the UDP protocols.
        final state = await pumpApp(tester, size: size, android: true);
        await tester.runAsync(() => state.testLatency());
        await openServers(tester, isPhone);
        expect(find.text('TCP'), findsWidgets);
        expect(find.text('ping'), findsWidgets);
        if (isPhone) {
          // A long press on the label is the row's pick: no tooltip there.
          expect(find.byTooltip(tcpTip), findsNothing);
        } else {
          expect(find.byTooltip(tcpTip), findsWidgets);
        }
        expect(tester.takeException(), isNull);
      });
    });
  }
}
