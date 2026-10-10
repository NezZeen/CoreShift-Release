import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/announcements.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/countries.dart';

/// The server list (countries, swipes), the quick pick
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

  testWidgets('servers by country and folding', (tester) async {
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

    // No favourites yet, and no transport column.
    expect(find.text('Избранное'), findsNothing);
    expect(find.text('ТРАНСПОРТ'), findsNothing);
    expect(find.textContaining('tcp'), findsNothing);
    expect(tester.takeException(), isNull);

    // Searching by the country's name finds its servers.
    await tester.enterText(find.byType(TextField).first, 'швеция');
    await tester.pump();
    expect(find.text('Stockholm'), findsOneWidget);
    expect(find.text('Helsinki'), findsNothing);
    await tester.pump(const Duration(seconds: 30));
  });

  testWidgets('the ping test shows no progress strip', (tester) async {
    final state = await pumpApp(tester);
    await tester.tap(navTo('Серверы', phone: false));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    expect(state.testingLatency, isTrue);
    expect(find.textContaining('Проверено'), findsNothing);
    expect(find.byType(LinearProgressIndicator), findsNWidgets(2), reason: 'only the traffic bars of the subscriptions');
    await tester.pump(const Duration(seconds: 30));
    expect(state.testingLatency, isFalse);
  });

  testWidgets('rows keep their place when the pointer moves over them', (tester) async {
    await pumpApp(tester);
    await tester.tap(navTo('Серверы', phone: false));
    await tester.pump();
    await tester.pump(const Duration(seconds: 30));
    final before = [
      for (final n in ['Rotterdam', 'Frankfurt', 'Tokyo']) tester.getTopLeft(find.text(n)).dy,
    ];
    final mouse = await tester.createGesture(kind: PointerDeviceKind.mouse);
    addTearDown(mouse.removePointer);
    // Over a row that is not the selected one, which gets a button then.
    await mouse.addPointer(location: tester.getCenter(find.text('Rotterdam')));
    await mouse.moveTo(tester.getCenter(find.text('Rotterdam')));
    await tester.pump();
    expect(find.text('Подключить'), findsNWidgets(2), reason: 'the selected row and the one under the pointer');
    expect([
      for (final n in ['Rotterdam', 'Frankfurt', 'Tokyo']) tester.getTopLeft(find.text(n)).dy,
    ], before);
    await mouse.moveTo(tester.getCenter(find.text('Tokyo')));
    await tester.pump();
    expect([
      for (final n in ['Rotterdam', 'Frankfurt', 'Tokyo']) tester.getTopLeft(find.text(n)).dy,
    ], before);
  });

  testWidgets('a phone stars a server, and swipes one to connect', (tester) async {
    final state = await pumpApp(tester, size: phone);
    await tester.tap(navTo('Серверы', phone: true));
    await tester.pump();
    await tester.pump(const Duration(seconds: 30));
    expect(find.textContaining('Смахните сервер'), findsOneWidget);

    // The star moves the server up, to the favourites.
    final (sub, helsinki) = node(state, 'Helsinki');
    await tester.ensureVisible(find.text('Helsinki'));
    await tester.pump();
    final row = find.ancestor(of: find.text('Helsinki'), matching: find.byWidgetPredicate((w) => w.runtimeType.toString() == '_NodeRow'));
    await tester.tap(find.descendant(of: row, matching: find.byTooltip('В избранное')));
    await tester.pump(const Duration(milliseconds: 400));
    expect(state.isFavorite(sub, helsinki), isTrue);
    expect(find.text('Избранное'), findsOneWidget);
    expect(find.byIcon(Icons.star_rounded), findsWidgets);

    final (_, stockholm) = node(state, 'Stockholm');
    await tester.ensureVisible(find.text('Stockholm'));
    await tester.pump();
    await tester.drag(find.text('Stockholm'), const Offset(300, 0));
    // The swipe finishes its slide, then the server connects.
    for (var i = 0; i < 4; i++) {
      await tester.pump(const Duration(milliseconds: 300));
    }
    expect(state.quickNodes().map((e) => e.$2.name), [helsinki.name, stockholm.name], reason: 'the favourite first, then the one used');
    expect(state.prefs['server_hint'], isTrue);
    expect(find.textContaining('Смахните сервер'), findsNothing, reason: 'the hint goes after the first swipe');
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
      state.noteRecent(sub.id, frankfurt.fingerprint);
      await tester.pump();

      // On the desktop the sidebar's status names the server too, before the card does.
      await tester.tap(find.text('Amsterdam').last);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      expect(find.text('Выбор сервера'), findsOneWidget);
      expect(find.text('Найти самый быстрый'), findsOneWidget);
      expect(find.text('Недавние'), findsOneWidget);
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
      // The provider's announcement has a «Подробнее» of its own.
      state.announcements.forEach(state.hideAnnouncement);
      await settle(tester);
      expect(state.statsLoaded, isTrue);
      expect(state.stats.length, 30);
      // One line on the home page; the week's bars behind «Подробнее».
      expect(find.text('Трафик'), findsOneWidget);
      await tester.ensureVisible(find.text('Подробнее'));
      await tester.tap(find.text('Подробнее'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      expect(find.text('Трафик через VPN'), findsOneWidget);
      expect(find.text('Скачано'), findsOneWidget);
      // A week only: no month view.
      expect(find.text('30 дней'), findsNothing);
      expect(find.text('за 7 дней'), findsOneWidget);
      expect(tester.takeException(), isNull);
      // Pointing at a day names it.
      // The chart is above its caption: a tap on its left edge picks the oldest day.
      final caption = find.descendant(of: find.byType(isPhone ? BottomSheet : Dialog), matching: find.textContaining('Сегодня:'));
      await tester.tapAt(tester.getTopLeft(caption) + const Offset(6, -50));
      await tester.pump();
      // The picked day is named instead of «Сегодня».
      expect(find.textContaining('Сегодня:'), findsNothing);
      expect(tester.takeException(), isNull);
      await tester.pump(const Duration(seconds: 6));
    });
  }
}
