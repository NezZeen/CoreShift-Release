import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/widgets.dart';

import 'support/fixture_backend.dart';

/// The servers page under load: a latency test whose results arrive one by
/// one or all at once, over subscriptions of hundreds of servers named with
/// flags, emoji and decorations. A tester's subscription of 130 servers
/// froze the window at the start of the test: every result redrew every
/// row, and each row worked its name out anew (seventy patterns compiled
/// per name), so the results of a few seconds took a minute to draw.
///
/// The frames are timed in a debug build of the test harness, several times
/// slower than a release build; the budgets are for that, with room.
void main() {
  const desktop = Size(1400, 900);
  const phone = Size(390, 844);

  /// What a frame, and all the frames of a test together, may take. A frame
  /// that changes hundreds of rows at once is slow by itself; what the old
  /// page did wrong was to draw every row again for every result, which made
  /// the frames of a test add up to minutes.

  Future<AppState> openServers(WidgetTester tester, FixtureBackend backend, {Size size = desktop, bool light = false, Json prefs = const {}}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = AppState(backend, prefs: {'theme': light ? 'light' : 'dark', ...prefs});
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    // The page tests the servers on its own the first time: the test does it.
    state.latencyAutoTested = true;
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    final compact = size.width < 720;
    await tester.tap(compact ? find.descendant(of: find.byType(NavigationBar), matching: find.text('Серверы')) : find.text('Серверы').first);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 500));
    return state;
  }

  /// What a test of the latency did to the screen.
  Future<({List<Duration> frames, int notifications})> runLatency(WidgetTester tester, AppState state) async {
    var notifications = 0;
    void count() => notifications++;
    state.addListener(count);
    final frames = <Duration>[];
    final sw = Stopwatch();
    // The first frames build what is new to the harness; the page's own cost
    // is what is measured.
    final done = state.testLatency();
    var finished = false;
    done.then((_) => finished = true);
    for (var i = 0; i < 4000 && !finished; i++) {
      sw.reset();
      sw.start();
      await tester.pump(const Duration(milliseconds: 16));
      sw.stop();
      frames.add(sw.elapsed);
    }
    await done;
    await tester.pump(const Duration(seconds: 2));
    state.removeListener(count);
    return (frames: frames, notifications: notifications);
  }

  Duration percentile(List<Duration> d, double p) {
    final s = [...d]..sort();
    return s[((s.length - 1) * p).round()];
  }

  void expectLoaded(
    WidgetTester tester,
    AppState state,
    FixtureBackend backend,
    ({List<Duration> frames, int notifications}) r,
    String what, {
    int slowestMs = 1000,
    int totalMs = 4000,
  }) {
    expect(tester.takeException(), isNull, reason: what);
    expect(state.testingLatency, isFalse, reason: what);
    expect(state.latency.length, backend.nodes.length, reason: '$what: every result is kept');
    expect(state.latencyDone, state.latencyTotal, reason: what);
    final slowest = r.frames.reduce((a, b) => a > b ? a : b);
    final p95 = percentile(r.frames, .95);
    final total = r.frames.fold(Duration.zero, (a, b) => a + b);
    debugPrint(
      '$what: ${r.frames.length} frames, ${r.notifications} redraws, p95 ${p95.inMilliseconds} ms, slowest ${slowest.inMilliseconds} ms, all ${total.inMilliseconds} ms',
    );
    expect(slowest.inMilliseconds, lessThan(slowestMs), reason: '$what: a frame took $slowest');
    expect(total.inMilliseconds, lessThan(totalMs), reason: '$what: the frames took $total together');
  }

  final bigList = loadBigListNodes();

  group('the 132 servers of a public subscription', () {
    for (final (label, light, burst, gap) in [
      ('light theme, a result every 10 ms', true, 1, const Duration(milliseconds: 10)),
      ('dark theme, ten results every 30 ms', false, 10, const Duration(milliseconds: 30)),
      ('light theme, all results at once', true, 1000, Duration.zero),
    ]) {
      testWidgets('desktop, $label', (tester) async {
        final backend = FixtureBackend(bigList, burst: burst, gap: gap);
        final state = await openServers(tester, backend, light: light);
        final r = await runLatency(tester, state);
        expectLoaded(tester, state, backend, r, 'big list desktop $label');
        // A result a row, not a redraw a result: the page is told at a pace.
        expect(r.notifications, lessThan(40), reason: '${backend.nodes.length} results made ${r.notifications} redraws');
        // The numbers are on the rows.
        expect(find.textContaining(' мс'), findsWidgets);
      });
    }

    testWidgets('a phone', (tester) async {
      final backend = FixtureBackend(bigList, burst: 4, gap: const Duration(milliseconds: 20));
      final state = await openServers(tester, backend, size: phone, light: true);
      final r = await runLatency(tester, state);
      expectLoaded(tester, state, backend, r, 'big list phone');
      expect(find.textContaining(' мс'), findsWidgets);
    });

    testWidgets('grouped by country and sorted by ping while the results come', (tester) async {
      final backend = FixtureBackend(bigList, burst: 8, gap: const Duration(milliseconds: 25));
      final state = await openServers(tester, backend, light: true, prefs: {'server_group': true, 'server_sort': 'ping'});
      final r = await runLatency(tester, state);
      expectLoaded(tester, state, backend, r, 'big list by country and ping');
    });

    testWidgets('the search typed while the test runs', (tester) async {
      final backend = FixtureBackend(bigList, burst: 2, gap: const Duration(milliseconds: 20));
      final state = await openServers(tester, backend);
      final done = state.testLatency();
      await tester.pump(const Duration(milliseconds: 100));
      final sw = Stopwatch()..start();
      for (final q in ['ф', 'фин', 'финл', 'финляндия', 'fi', 'vless', 'telegram', '']) {
        await tester.enterText(find.byType(TextField).first, q);
        await tester.pump(const Duration(milliseconds: 50));
      }
      sw.stop();
      debugPrint('big list search: 8 queries in ${sw.elapsedMilliseconds} ms');
      expect(sw.elapsed, lessThan(const Duration(seconds: 6)));
      await tester.pump(const Duration(seconds: 10));
      await done;
      expect(tester.takeException(), isNull);
    });
  });

  group('subscriptions of any size and naming', () {
    for (final (count, size, label) in [(500, desktop, 'desktop'), (1000, desktop, 'desktop'), (800, phone, 'phone')]) {
      testWidgets('$count synthetic servers, $label, bursts of results', (tester) async {
        final backend = FixtureBackend(syntheticNodes(count), burst: 25, gap: const Duration(milliseconds: 5));
        final state = await openServers(tester, backend, size: size, light: count.isEven);
        final r = await runLatency(tester, state);
        expectLoaded(tester, state, backend, r, '$count synthetic $label', slowestMs: 5000, totalMs: 12000);
        // Whatever the size, the redraws follow the clock, not the results.
        expect(r.notifications, lessThan(count ~/ 4), reason: '$count results made ${r.notifications} redraws');
      });
    }

    testWidgets('600 synthetic servers, a result every 20 ms: the frames stay short', (tester) async {
      final backend = FixtureBackend(syntheticNodes(600, seed: 3), burst: 1, gap: const Duration(milliseconds: 20));
      final state = await openServers(tester, backend, light: true);
      final r = await runLatency(tester, state);
      expectLoaded(tester, state, backend, r, '600 synthetic one by one', slowestMs: 3000, totalMs: 30000);
      // Between the redraws only the rows with a new result are drawn again.
      expect(percentile(r.frames, .95).inMilliseconds, lessThan(400));
    });

    testWidgets('1000 synthetic servers, all results at once, grouped by country', (tester) async {
      final backend = FixtureBackend(syntheticNodes(1000, seed: 9), burst: 5000, gap: Duration.zero);
      final state = await openServers(tester, backend, prefs: {'server_group': true, 'server_sort': 'ping'});
      final r = await runLatency(tester, state);
      expectLoaded(tester, state, backend, r, '1000 synthetic grouped', slowestMs: 6000, totalMs: 12000);
      expect(r.notifications, lessThan(6), reason: 'results that came together made ${r.notifications} redraws');
    });
  });

  group('rows that are not redrawn still follow what they show', () {
    testWidgets('a result, a favourite, the theme and the window size reach the row', (tester) async {
      tester.platformDispatcher.platformBrightnessTestValue = Brightness.light;
      addTearDown(tester.platformDispatcher.clearPlatformBrightnessTestValue);
      final backend = FixtureBackend(bigList, burst: 1000, gap: Duration.zero);
      final state = await openServers(tester, backend, prefs: {'theme': 'system'});
      expect(find.textContaining(' мс'), findsNothing);

      // Only node 3 has an answer: its row alone shows it.
      backend.emit(backend.latencyEvent(3));
      await tester.pump(const Duration(milliseconds: 20));
      final ms = '${20 + (3 * 37) % 700} мс';
      expect(find.textContaining(ms), findsOneWidget);
      Color? inkOf() => tester.widget<Text>(find.descendant(of: find.byType(Panel), matching: find.textContaining(ms))).textSpan?.style?.color;
      final lightInk = inkOf();

      // The theme changes: the colour of the number follows.
      tester.platformDispatcher.platformBrightnessTestValue = Brightness.dark;
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      expect(inkOf(), isNot(lightInk), reason: 'the row kept the colours of the other theme');

      // A favourite is starred: its star fills.
      final sub = state.subscriptions.single;
      final node = sub.nodes[3];
      expect(find.byIcon(Icons.star_rounded), findsNothing);
      state.toggleFavorite(sub, node);
      await tester.pump();
      expect(find.byIcon(Icons.star_rounded), findsWidgets);

      // The window narrows to a phone's: the row becomes a list item.
      tester.view.physicalSize = phone;
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      expect(tester.takeException(), isNull);
      // The row's cell, not the country header's «от … мс».
      expect(find.byWidgetPredicate((w) => w is Text && (w.data ?? w.textSpan?.toPlainText() ?? '').startsWith(ms)), findsOneWidget);
    });
  });
}
