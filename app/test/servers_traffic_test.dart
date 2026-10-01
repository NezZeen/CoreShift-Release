import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/countries.dart';

/// The server list (countries, favourites, sorting, swipes), the quick pick
/// on the home page and the traffic card, on the desktop and
/// on a phone.
void main() {
  const phone = Size(390, 844);

  Future<AppState> pumpApp(WidgetTester tester, {Size size = const Size(1400, 900), Json? prefs}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = AppState(DemoBackend(), prefs: prefs);
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    return state;
  }

  Future<void> settle(WidgetTester tester) async {
    for (var i = 0; i < 4; i++) {
      await tester.pump(const Duration(seconds: 1));
      await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 100)));
    }
    await tester.pump();
  }

  Finder navTo(String page, {required bool phone}) =>
      phone ? find.descendant(of: find.byType(NavigationBar), matching: find.text(page)) : find.text(page).first;

  (Subscription, NodeView) node(AppState s, String name) {
    for (final sub in s.subscriptions) {
      for (final n in sub.nodes) {
        if (cleanNodeName(n.name) == name) return (sub, n);
      }
    }
    throw StateError('no node $name');
  }

  test('countries are found by flag, word, code or host', () {
    expect(countryOf('\u{1F1E9}\u{1F1EA} Frankfurt'), 'DE');
    expect(countryOf('Helsinki'), 'FI');
    expect(countryOf('Warsaw'), 'PL');
    expect(countryOf('Германия #2'), 'DE');
    expect(countryOf('NL | fast'), 'NL');
    expect(countryOf('server 7', 'jp1.example.com'), 'JP');
    expect(countryOf('Mystery', '203.0.113.7'), isNull);
    // "usa" is a word, but a name that merely contains it is not.
    expect(countryOf('Husband'), isNull);
    expect(cleanNodeName('\u{1F1E9}\u{1F1EA} Frankfurt'), 'Frankfurt');
    expect(cleanNodeName('\u{1F1E9}\u{1F1EA}'), '\u{1F1E9}\u{1F1EA}');
  });

  testWidgets('servers by country, favourites, folding and sorting', (tester) async {
    final state = await pumpApp(tester);
    await tester.tap(navTo('Серверы', phone: false));
    await tester.pump();
    await tester.pump(const Duration(seconds: 30)); // the first ping test

    expect(find.text('Нидерланды'), findsNothing);
    await tester.tap(find.text('По странам'));
    await tester.pump();
    expect(state.serverGroup, isTrue);
    for (final c in ['Нидерланды', 'Германия', 'США', 'Польша', 'Латвия']) {
      expect(find.text(c), findsOneWidget, reason: c);
    }
    // The ones without a country in the name or the host come last.
    expect(find.text('Frankfurt'), findsOneWidget);

    // A heading folds its rows away.
    await tester.tap(find.text('Германия'));
    await tester.pump();
    expect(find.text('Frankfurt'), findsNothing);
    await tester.tap(find.text('Германия'));
    await tester.pump();
    expect(find.text('Frankfurt'), findsOneWidget);

    // A star moves the server to the favourites, on top.
    expect(find.text('Избранное'), findsNothing);
    final (sub, helsinki) = node(state, 'Helsinki');
    state.toggleFavorite(sub, helsinki);
    await tester.pump();
    expect(find.text('Избранное'), findsOneWidget);
    expect(state.isFavorite(sub, helsinki), isTrue);
    expect(find.byTooltip('Убрать из избранного'), findsOneWidget);
    await tester.tap(find.byTooltip('Убрать из избранного'));
    // The row waits for a possible double tap before it lets the star act.
    await tester.pump(const Duration(milliseconds: 400));
    expect(state.favorites, isEmpty);
    expect(find.text('Избранное'), findsNothing);

    // The order is a preference of its own.
    await tester.tap(find.text('Как в подписке'), warnIfMissed: false);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(find.text('По пингу').last, warnIfMissed: false);
    await tester.pump();
    expect(state.serverSort, 'ping');
    expect(tester.takeException(), isNull);

    // Searching by the country's name finds its servers.
    await tester.enterText(find.byType(TextField).first, 'швеция');
    await tester.pump();
    expect(find.text('Stockholm'), findsOneWidget);
    expect(find.text('Helsinki'), findsNothing);
    await tester.pump(const Duration(seconds: 30));
  });

  testWidgets('the ping says how far it is', (tester) async {
    final state = await pumpApp(tester);
    await tester.tap(navTo('Серверы', phone: false));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    expect(state.testingLatency, isTrue);
    expect(state.latencyTotal, state.nodeCount);
    expect(find.textContaining('Проверено'), findsOneWidget);
    await tester.pump(const Duration(seconds: 30));
    expect(state.testingLatency, isFalse);
    expect(state.latencyDone, state.latencyTotal);
    expect(find.textContaining('Проверено'), findsNothing);
  });

  testWidgets('a phone swipes a server to the favourites, and to connect', (tester) async {
    final state = await pumpApp(tester, size: phone);
    await tester.tap(navTo('Серверы', phone: true));
    await tester.pump();
    await tester.pump(const Duration(seconds: 30));
    expect(find.textContaining('Смахните сервер'), findsOneWidget);

    final (sub, helsinki) = node(state, 'Helsinki');
    await tester.ensureVisible(find.text('Helsinki'));
    await tester.pump();
    await tester.drag(find.text('Helsinki'), const Offset(-300, 0));
    await tester.pumpAndSettle();
    expect(state.isFavorite(sub, helsinki), isTrue);
    expect(state.prefs['swipe_hint'], isTrue);
    expect(find.textContaining('Смахните сервер'), findsNothing, reason: 'the hint goes after the first swipe');
    expect(find.text('Избранное'), findsOneWidget);

    final (_, stockholm) = node(state, 'Stockholm');
    await tester.ensureVisible(find.text('Stockholm'));
    await tester.pump();
    await tester.drag(find.text('Stockholm'), const Offset(300, 0));
    // The swipe finishes its slide, then the server connects.
    for (var i = 0; i < 4; i++) {
      await tester.pump(const Duration(milliseconds: 300));
    }
    expect(state.quickNodes().map((e) => e.$2.name), contains(stockholm.name));
    await tester.pump(const Duration(seconds: 30));
    expect(state.status.node, stockholm.name);
    expect(tester.takeException(), isNull);
    await tester.runAsync(() => state.disconnect());
    await tester.pump(const Duration(seconds: 6));
  });

  for (final isPhone in [false, true]) {
    testWidgets('the home page picks a server quickly${isPhone ? ' on a phone' : ''}', (tester) async {
      final state = await pumpApp(tester, size: isPhone ? phone : const Size(1400, 900));
      await settle(tester);
      final (sub, frankfurt) = node(state, 'Frankfurt');
      state.toggleFavorite(sub, frankfurt);
      await tester.pump();

      // On the desktop the sidebar's status names the server too, before the card does.
      await tester.tap(find.text('Amsterdam').last);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      expect(find.text('Выбор сервера'), findsOneWidget);
      expect(find.text('Найти самый быстрый'), findsOneWidget);
      expect(find.text('Избранные и недавние'.toUpperCase()), findsOneWidget);
      expect(find.text('Все серверы · ${state.nodeCount}'), findsOneWidget);
      expect(tester.takeException(), isNull);

      await tester.tap(find.text('Frankfurt'));
      await tester.pump(const Duration(milliseconds: 600));
      await tester.pump(const Duration(milliseconds: 600));
      expect(find.text('Выбор сервера'), findsNothing);
      expect(state.selection.name, frankfurt.name);
      expect(find.text('Frankfurt'), findsWidgets);

      // The way to the full list.
      await tester.tap(find.text('Frankfurt').last);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      await tester.tap(find.text('Все серверы · ${state.nodeCount}'));
      await tester.pump(const Duration(milliseconds: 600));
      await tester.pump(const Duration(milliseconds: 600));
      expect(find.text('Добавить подписку').evaluate().isNotEmpty || find.text('Добавить').evaluate().isNotEmpty, isTrue);
      expect(find.byType(NavigationBar).evaluate().isNotEmpty == isPhone, isTrue);
      await tester.pump(const Duration(seconds: 30));
    });

    testWidgets('the home page shows the traffic${isPhone ? ' on a phone' : ''}', (tester) async {
      final state = await pumpApp(tester, size: isPhone ? phone : const Size(1400, 900));
      await settle(tester);
      expect(state.statsLoaded, isTrue);
      expect(state.stats.length, 30);
      if (isPhone) {
        expect(find.text('Сегодня'), findsOneWidget);
        await tester.tap(find.text('Сегодня'));
      } else {
        expect(find.text('Трафик'), findsOneWidget);
        await tester.tap(find.text('Подробнее'));
      }
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      expect(find.text('Трафик через VPN'), findsOneWidget);
      expect(find.text('Скачано'), findsOneWidget);
      await tester.tap(find.text('30 дней'));
      await tester.pump();
      expect(tester.takeException(), isNull);
      // Pointing at a day names it.
      // The chart is above its caption: a tap on its left edge picks the oldest day.
      final caption = find.descendant(of: find.byType(isPhone ? BottomSheet : Dialog), matching: find.textContaining('Сегодня:'));
      await tester.tapAt(tester.getTopLeft(caption) + const Offset(6, -50));
      await tester.pump();
      // The picked day is named; the card's own chart still says "Сегодня".
      expect(find.textContaining('Сегодня:'), isPhone ? findsNothing : findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pump(const Duration(seconds: 6));
    });
  }
}
