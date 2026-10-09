import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/ui/pages/home_page.dart';
import 'package:coreshift/ui/theme.dart';

/// The time under "Подключено" ticked only when something else redrew the
/// page (a health check every 15 s, a batch of logs), so it jumped by 5 to
/// 30 seconds; a reconnect starting it over looked like it ran backwards.
void main() {
  Widget app(DateTime since, DateTime Function() now) => MaterialApp(
    theme: buildTheme(Brightness.dark),
    home: Scaffold(
      body: Center(
        child: ConnectedTime(since: since, tun: true, now: now),
      ),
    ),
  );

  String shown(WidgetTester tester) => tester
      .widgetList<RichText>(find.descendant(of: find.byType(ConnectedTime), matching: find.byType(RichText)))
      .map((t) => t.text.toPlainText())
      .join(' | ');

  int seconds(WidgetTester tester) {
    final m = RegExp(r'(\d\d):(\d\d):(\d\d)').firstMatch(shown(tester))!;
    return int.parse(m[1]!) * 3600 + int.parse(m[2]!) * 60 + int.parse(m[3]!);
  }

  // A frame a second, nothing else redrawing the page: every second shows.
  for (final ms in [5500, 5990]) {
    testWidgets('ticks every second by itself, ${ms}ms in', (tester) async {
      DateTime now() => tester.binding.clock.now();
      final since = now().subtract(Duration(milliseconds: ms));
      await tester.pumpWidget(app(since, now));
      expect(seconds(tester), 5);

      final seen = <int>[];
      for (var i = 0; i < 10; i++) {
        await tester.pump(const Duration(seconds: 1));
        seen.add(seconds(tester));
      }
      expect(seen, [for (var s = 6; s <= 15; s++) s]);
    });
  }

  // Frames as often as a screen draws them: each second once, in order, on
  // whole, half and nearly whole seconds since the connection came up.
  for (final ms in [5000, 5500, 5990]) {
    testWidgets('no second is skipped or shown twice, ${ms}ms in', (tester) async {
      DateTime now() => tester.binding.clock.now();
      final since = now().subtract(Duration(milliseconds: ms));
      await tester.pumpWidget(app(since, now));
      final seen = [seconds(tester)];
      for (var i = 0; i < 200; i++) {
        await tester.pump(const Duration(milliseconds: 50));
        final s = seconds(tester);
        if (s != seen.last) seen.add(s);
        final real = now().difference(since).inSeconds;
        // Behind by no more than the few milliseconds the tick waits past
        // the second, until the next frame.
        expect(s, anyOf(real, real - 1), reason: 'shown $s s at $real s');
      }
      expect(seen, [for (var s = 5; s <= seen.last; s++) s]);
      expect(seen.length, greaterThanOrEqualTo(10), reason: 'seconds shown in 10 s: $seen');
    });
  }

  testWidgets('a reconnect starts the count over and says so for a moment', (tester) async {
    DateTime now() => tester.binding.clock.now();
    await tester.pumpWidget(app(now().subtract(const Duration(seconds: 65)), now));
    expect(shown(tester), contains('00:01:05'));
    expect(shown(tester), isNot(contains('переподключено')));

    final again = now();
    await tester.pumpWidget(app(again, now));
    await tester.pump(const Duration(milliseconds: 300));
    expect(shown(tester), contains('00:00:00'));
    expect(shown(tester), contains('переподключено'));
    expect(shown(tester), isNot(contains('00:01:05')));

    // The new count ticks from its own start.
    await tester.pump(const Duration(milliseconds: 800));
    expect(shown(tester), contains('00:00:01'));
    expect(shown(tester), contains('переподключено'));

    await tester.pump(ConnectedTime.noteFor);
    await tester.pump(const Duration(milliseconds: 300));
    expect(shown(tester), isNot(contains('переподключено')));
    expect(shown(tester), contains('все приложения через VPN'));
    expect(seconds(tester), now().difference(again).inSeconds);

    // The same moment again (the status read anew) is no reconnect.
    await tester.pumpWidget(app(again.toUtc(), now));
    await tester.pump(const Duration(milliseconds: 300));
    expect(shown(tester), isNot(contains('переподключено')));
  });
}
