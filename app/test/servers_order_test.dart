import 'dart:math';

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/countries.dart';
import 'package:coreshift/ui/pages/servers_page.dart';
import 'package:coreshift/ui/widgets.dart';

/// The order of the server list, the favourites and the servers removed
/// from a subscription: on the desktop, on a phone and in the home page's
/// quick pick.
void main() {
  const phone = Size(390, 844);
  const desktop = Size(1400, 900);

  Future<AppState> pumpApp(WidgetTester tester, {Size size = desktop, Json? prefs}) async {
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

  /// Opens the servers and waits out the first ping test.
  Future<void> openServers(WidgetTester tester, {required bool phone}) async {
    await tester.tap(phone ? find.descendant(of: find.byType(NavigationBar), matching: find.text('Серверы')) : find.text('Серверы').first);
    await tester.pump();
    await tester.pump(const Duration(seconds: 30));
  }

  /// Lets a request to the demo daemon finish.
  Future<void> settle(WidgetTester tester) async {
    for (var i = 0; i < 3; i++) {
      await tester.pump(const Duration(milliseconds: 300));
    }
  }

  (Subscription, NodeView) node(AppState s, String name) {
    for (final sub in s.subscriptions) {
      for (final n in sub.nodes) {
        if (cleanNodeName(n.name) == name) return (sub, n);
      }
    }
    throw StateError('no node $name');
  }

  /// The names in the order the screen shows them, top to bottom.
  List<String> onScreen(WidgetTester tester, List<String> names) {
    final ys = {for (final n in names) n: tester.getTopLeft(find.text(n).first).dy};
    return [...names]..sort((a, b) => ys[a]!.compareTo(ys[b]!));
  }

  Finder rowOf(String name) => find.ancestor(of: find.text(name), matching: find.byWidgetPredicate((w) => w.runtimeType.toString() == '_NodeRow')).first;

  group('sortServers', () {
    const sub = Subscription(id: 's', name: '', displayName: 's', url: '', userAgent: '', info: SubInfo(), format: '', nodes: [], skipped: []);
    NodeView n(String name, {bool usable = true}) => NodeView(
      fingerprint: name,
      name: name,
      protocol: 'vless',
      transport: 'tcp',
      security: 'tls',
      server: 'x',
      port: 443,
      cores: usable ? const ['xray'] : const [],
    );
    final rows = [
      for (final name in ['Gamma', 'Server 10', 'Alpha', 'Server 2', 'beta', 'Dead', 'Untested']) (sub, n(name)),
    ];
    const pings = {
      'Gamma': Latency(ms: 300),
      'Server 10': Latency(ms: 40),
      'Alpha': Latency(ms: 120),
      'Server 2': Latency(ms: 40),
      'beta': Latency(ms: 90),
      'Dead': Latency(error: 'timeout'),
    };
    List<String> names(List<(Subscription, NodeView)> r) => [for (final x in r) x.$2.name];

    test('by ping: the fastest first, untested and silent last, ties as listed', () {
      expect(names(sortServers(rows, 'ping', (_, n) => pings[n.name])), ['Server 10', 'Server 2', 'beta', 'Alpha', 'Gamma', 'Untested', 'Dead']);
    });

    test('by name: without case, numbers as numbers, flags ignored', () {
      expect(names(sortServers(rows, 'name', (_, _) => null)), ['Alpha', 'beta', 'Dead', 'Gamma', 'Server 2', 'Server 10', 'Untested']);
      final flagged = [(sub, n('\u{1F1FA}\u{1F1F8} Boston')), (sub, n('\u{1F1E6}\u{1F1F9} Vienna')), (sub, n('Austin'))];
      expect(names(sortServers(flagged, 'name', (_, _) => null)), ['Austin', '\u{1F1FA}\u{1F1F8} Boston', '\u{1F1E6}\u{1F1F9} Vienna']);
    });

    test('as in the subscription: untouched', () {
      expect(names(sortServers(rows, 'sub', (_, n) => pings[n.name])), names(rows));
    });

    test('a server no core runs counts as not answering', () {
      final r = [(sub, n('Off', usable: false)), (sub, n('On'))];
      expect(names(sortServers(r, 'ping', (_, _) => const Latency(ms: 10))), ['On', 'Off']);
    });
  });

  testWidgets('the desktop orders the servers by ping or name, within countries too', (tester) async {
    final state = await pumpApp(tester);
    await openServers(tester, phone: false);
    const de = ['Frankfurt', 'Falkenstein', 'Nuremberg'];
    expect(onScreen(tester, de), de, reason: 'as the subscription lists them');

    await tester.tap(find.text('По пингу'));
    await tester.pump();
    expect(state.serverSort, 'ping');
    expect(state.prefs['server_sort'], 'ping');
    // Istanbul never answers: it is the last of all.
    expect(onScreen(tester, ['Amsterdam', 'Rotterdam', 'Warsaw', 'Frankfurt', 'Tokyo', 'Istanbul']), [
      'Amsterdam',
      'Rotterdam',
      'Warsaw',
      'Frankfurt',
      'Tokyo',
      'Istanbul',
    ]);
    expect(find.text('NorthLink Premium'), findsWidgets, reason: 'sorted, the subscriptions make one list and each row names its own');

    // Grouped by country, each country is sorted on its own.
    await tester.tap(find.text('По странам'));
    await tester.pump();
    expect(onScreen(tester, de), ['Frankfurt', 'Nuremberg', 'Falkenstein']);
    expect(onScreen(tester, ['Германия', 'Нидерланды', 'США']), ['Германия', 'Нидерланды', 'США'], reason: 'countries stay in the order of their names');

    await tester.tap(find.text('По имени'));
    await tester.pump();
    expect(state.serverSort, 'name');
    expect(onScreen(tester, de), ['Falkenstein', 'Frankfurt', 'Nuremberg']);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a phone picks the order from a chip', (tester) async {
    final state = await pumpApp(tester, size: phone);
    await openServers(tester, phone: true);
    expect(find.text('По пингу'), findsNothing, reason: 'no room for the three side by side');
    expect(find.text('Как в подписке'), findsOneWidget);
    await tester.tap(find.byTooltip('Порядок серверов'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(CheckedPopupMenuItem<String>, 'По пингу'));
    await tester.pumpAndSettle();
    expect(state.serverSort, 'ping');
    expect(find.text('По пингу'), findsOneWidget, reason: 'the chip names the order');
    expect(onScreen(tester, ['Amsterdam', 'Rotterdam']), ['Amsterdam', 'Rotterdam']);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a star puts the server among the favourites, on top, and first in the quick pick', (tester) async {
    final state = await pumpApp(tester);
    await openServers(tester, phone: false);
    final (sub, tokyo) = node(state, 'Tokyo');
    final mouse = await tester.createGesture(kind: PointerDeviceKind.mouse);
    addTearDown(mouse.removePointer);
    await mouse.addPointer(location: tester.getCenter(find.text('Tokyo')));
    await tester.pump();
    await tester.tap(find.descendant(of: rowOf('Tokyo'), matching: find.byTooltip('В избранное')));
    // The row waits for a possible double tap before it lets the star act.
    await tester.pump(const Duration(milliseconds: 400));
    expect(state.isFavorite(sub, tokyo), isTrue);
    expect(find.text('Избранное'), findsOneWidget);
    expect(onScreen(tester, ['Tokyo', 'Rotterdam']), ['Tokyo', 'Rotterdam']);
    expect(find.text('Tokyo'), findsOneWidget, reason: 'a favourite is not listed twice');

    // One subscription shown: the rest under a heading of its own.
    await tester.tap(find.text('NorthLink Premium').first);
    await tester.pump();
    expect(find.text('Остальные'), findsOneWidget);
    await tester.tap(find.text('NorthLink Premium').first);
    await tester.pump();

    // A search shows the favourites it matches, in their section.
    await tester.enterText(find.byType(TextField).first, 'япония');
    await tester.pump();
    expect(find.text('Избранное'), findsOneWidget);
    expect(find.text('Tokyo'), findsOneWidget);
    await tester.enterText(find.byType(TextField).first, 'amster');
    await tester.pump();
    expect(find.text('Избранное'), findsNothing);
    await tester.enterText(find.byType(TextField).first, '');
    await tester.pump();

    // The quick pick: the favourites, then the servers used last.
    final (rigaSub, riga) = node(state, 'Riga');
    state.noteRecent(rigaSub.id, riga.fingerprint);
    state.noteRecent(sub.id, tokyo.fingerprint);
    expect(state.quickNodes().map((e) => cleanNodeName(e.$2.name)), ['Tokyo', 'Riga']);
    await tester.tap(find.text('Главная').first);
    await tester.pump();
    await tester.tap(find.text('Amsterdam').last);
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.text('Избранное'), findsOneWidget);
    expect(find.text('Недавние'), findsOneWidget);
    expect(onScreen(tester, ['Избранное', 'Tokyo', 'Недавние', 'Riga']), ['Избранное', 'Tokyo', 'Недавние', 'Riga']);
    expect(tester.takeException(), isNull);
  });

  testWidgets('the right-click menu: connect, star, remove from the subscription and bring back', (tester) async {
    final state = await pumpApp(tester);
    await openServers(tester, phone: false);
    final (sub, tokyo) = node(state, 'Tokyo');
    final count = state.nodeCount;

    await tester.tap(find.text('Tokyo'), buttons: kSecondaryButton);
    await tester.pumpAndSettle();
    expect(find.text('Подключиться'), findsOneWidget);
    expect(find.text('В избранное'), findsOneWidget);
    expect(find.text('Удалить из подписки'), findsOneWidget);
    expect(find.text('Проверить пинг'), findsNWidgets(1), reason: 'only the toolbar\'s: the daemon tests no single server');
    await tester.tap(find.text('В избранное'));
    await tester.pumpAndSettle();
    expect(state.isFavorite(sub, tokyo), isTrue);

    await tester.tap(find.text('Tokyo'), buttons: kSecondaryButton);
    await tester.pumpAndSettle();
    expect(find.text('Убрать из избранного'), findsOneWidget);
    await tester.tap(find.text('Удалить из подписки'));
    await tester.pump();
    await settle(tester);
    expect(find.text('Tokyo'), findsNothing);
    expect(state.nodeCount, count - 1);
    expect(state.subscriptionById(sub.id)!.hiddenNodes.map((n) => n.fingerprint), [tokyo.fingerprint]);
    expect(state.quickNodes(), isEmpty, reason: 'a removed favourite is out of the quick pick');
    expect(find.text('«Tokyo» удалён из подписки'), findsOneWidget);
    expect(find.text('Удалено из подписок: 1'), findsOneWidget);

    // «Отменить» on the toast brings it back.
    await tester.tap(find.text('Отменить'));
    await tester.pump();
    await settle(tester);
    expect(find.text('Tokyo'), findsOneWidget);
    expect(find.text('Удалено из подписок: 1'), findsNothing);

    // Removed again, it stays removed when the subscription is refreshed.
    await tester.tap(find.text('Tokyo'), buttons: kSecondaryButton);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Удалить из подписки'));
    await tester.pump();
    await settle(tester);
    await tester.runAsync(() => state.refreshSubscription(sub.id));
    await tester.pump();
    expect(find.text('Tokyo'), findsNothing);
    expect(find.text('Удалено из подписок: 1'), findsOneWidget);

    // «Показать и вернуть» lists it, and brings it back.
    await tester.pump(const Duration(seconds: 9)); // the toasts go
    await tester.ensureVisible(find.text('Показать и вернуть'));
    await tester.tap(find.text('Показать и вернуть'));
    await tester.pumpAndSettle();
    expect(find.text('Удалённые серверы'), findsOneWidget);
    expect(find.descendant(of: find.byType(Dialog), matching: find.text('Tokyo')), findsOneWidget);
    await tester.tap(find.text('Вернуть'));
    await tester.pump();
    await settle(tester);
    expect(find.text('Все серверы снова в списке'), findsOneWidget);
    await tester.tap(find.text('Закрыть'));
    await tester.pumpAndSettle();
    expect(find.text('Tokyo'), findsOneWidget);
    expect(state.nodeCount, count);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 10));
  });

  /// Holds a press on [from], moves it over [over] in turn, and lets go.
  Future<void> holdAndDrag(WidgetTester tester, String from, List<String> over, {PointerDeviceKind kind = PointerDeviceKind.touch}) async {
    final g = await tester.startGesture(tester.getCenter(rowOf(from)), kind: kind, buttons: kPrimaryButton);
    await tester.pump(kLongPressTimeout + const Duration(milliseconds: 100));
    for (final name in over) {
      await g.moveTo(tester.getCenter(rowOf(name)));
      await tester.pump();
    }
    await g.up();
    await tester.pump();
  }

  for (final isPhone in [false, true]) {
    testWidgets('a held press picks servers, a drag picks a range, removal and undo${isPhone ? ' on a phone' : ''}', (tester) async {
      final state = await pumpApp(tester, size: isPhone ? phone : desktop);
      await openServers(tester, phone: isPhone);
      final count = state.nodeCount;
      final (sub, _) = node(state, 'Rotterdam');
      if (isPhone) {
        // Rotterdam to the middle of the screen, away from the edges that
        // scroll the list.
        await tester.ensureVisible(rowOf('Rotterdam'));
        final pos = tester.state<ScrollableState>(find.ancestor(of: rowOf('Rotterdam'), matching: find.byType(Scrollable)).first).position;
        pos.jumpTo(max(0, pos.pixels - 250));
        await tester.pump();
      }

      // Held on Rotterdam, then over Frankfurt and Falkenstein: three.
      await holdAndDrag(tester, 'Rotterdam', ['Frankfurt', 'Falkenstein'], kind: isPhone ? PointerDeviceKind.touch : PointerDeviceKind.mouse);
      expect(find.text('Выбрано: 3'), findsOneWidget);
      expect(find.text('Удалить из подписки'), findsOneWidget);
      expect(find.text('Выбрать все'), findsOneWidget);
      expect(state.status.active, isFalse, reason: 'picking connects nothing');
      // The checkbox takes the radio's place and size, clear of the flag.
      final box = tester.getRect(find.descendant(of: rowOf('Rotterdam'), matching: find.byWidgetPredicate((w) => w.runtimeType.toString() == '_PickBox')));
      final flag = tester.getRect(find.descendant(of: rowOf('Rotterdam'), matching: find.byType(CountryBadge)));
      expect(box.size, const Size(16, 16));
      expect(flag.left - box.right, greaterThanOrEqualTo(6));

      // Taps pick and put back.
      await tester.tap(rowOf('Nuremberg'));
      await tester.pump();
      expect(find.text('Выбрано: 4'), findsOneWidget);
      await tester.tap(rowOf('Nuremberg'));
      await tester.pump();
      expect(find.text('Выбрано: 3'), findsOneWidget);
      expect(state.selection.name, contains('Amsterdam'), reason: 'a tap while picking selects no server');

      await tester.tap(find.text('Удалить из подписки'));
      await tester.pump();
      await settle(tester);
      expect(find.text('Выбрано: 3'), findsNothing);
      for (final n in ['Rotterdam', 'Frankfurt', 'Falkenstein']) {
        expect(find.text(n), findsNothing, reason: n);
      }
      expect(state.subscriptionById(sub.id)!.hiddenNodes.length, 3);
      expect(state.nodeCount, count - 3);
      expect(find.text('Удалено из подписки: 3 сервера'), findsOneWidget);

      // One «Отменить» brings all three back.
      await tester.tap(find.text('Отменить'));
      await tester.pump();
      await settle(tester);
      expect(state.nodeCount, count);
      expect(find.text('Rotterdam'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pump(const Duration(seconds: 10));
    });
  }

  testWidgets('Esc ends the picking; the server in use is not picked', (tester) async {
    final state = await pumpApp(tester);
    await openServers(tester, phone: false);
    await tester.runAsync(() => state.connect());
    await tester.pump();
    expect(state.status.active, isTrue);

    // Held on the server in use: a note, and no picking.
    await tester.longPress(rowOf('Amsterdam'));
    await tester.pump();
    expect(find.textContaining('Выбрано'), findsNothing);
    expect(find.text('К этому серверу вы подключены — его не удалить'), findsOneWidget);

    // A drag from Rotterdam over it leaves it out, and so does «Выбрать все».
    await holdAndDrag(tester, 'Rotterdam', ['Amsterdam']);
    expect(find.text('Выбрано: 1'), findsOneWidget);
    await tester.tap(find.text('Выбрать все'));
    await tester.pump();
    expect(find.text('Выбрано: ${state.nodeCount - 1}'), findsOneWidget);

    await tester.sendKeyEvent(LogicalKeyboardKey.escape);
    await tester.pump();
    expect(find.textContaining('Выбрано'), findsNothing);
    expect(state.subscriptions.every((s) => s.hiddenNodes.isEmpty), isTrue);

    // The right-click menu does not remove it either.
    await tester.tap(rowOf('Amsterdam'), buttons: kSecondaryButton);
    await tester.pumpAndSettle();
    expect(find.text('Сначала подключитесь к другому'), findsOneWidget);
    await tester.tap(find.text('Удалить из подписки'), warnIfMissed: false);
    await tester.pumpAndSettle();
    expect(state.subscriptions.every((s) => s.hiddenNodes.isEmpty), isTrue);
    await tester.runAsync(() => state.disconnect());
    await tester.pump(const Duration(seconds: 10));
  });

  testWidgets('a phone: the star, the hint, and the back gesture ends the picking', (tester) async {
    final state = await pumpApp(tester, size: phone);
    await openServers(tester, phone: true);
    expect(find.textContaining('Удерживайте и ведите пальцем'), findsOneWidget);

    final (sub, riga) = node(state, 'Riga');
    await tester.ensureVisible(rowOf('Riga'));
    await tester.pump();
    await tester.tap(find.descendant(of: rowOf('Riga'), matching: find.byIcon(Icons.star_outline_rounded)));
    await tester.pump(const Duration(milliseconds: 400));
    expect(state.isFavorite(sub, riga), isTrue);
    expect(find.text('Избранное'), findsOneWidget);
    await tester.ensureVisible(rowOf('Riga'));
    await tester.pump();
    await tester.tap(find.descendant(of: rowOf('Riga'), matching: find.byIcon(Icons.star_rounded)));
    await tester.pump(const Duration(milliseconds: 400));
    expect(state.isFavorite(sub, riga), isFalse);

    // A swipe to the left does nothing now; the star is the way.
    await tester.ensureVisible(rowOf('Helsinki'));
    await tester.pump();
    await tester.drag(rowOf('Helsinki'), const Offset(-300, 0));
    await tester.pumpAndSettle();
    expect(state.favorites, isEmpty);

    await tester.longPress(rowOf('Helsinki'));
    await tester.pump();
    expect(find.text('Выбрано: 1'), findsOneWidget);
    // Back: the picking ends, the app stays.
    await tester.binding.handlePopRoute();
    await tester.pump();
    expect(find.textContaining('Выбрано'), findsNothing);
    expect(find.text('Helsinki'), findsOneWidget);
    expect(state.prefs['server_hint'], isTrue, reason: 'the hint goes once the picking was used');
    expect(tester.takeException(), isNull);
  });
}
