import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/pages/servers_page.dart';
import 'package:coreshift/ui/theme.dart';
import 'package:coreshift/ui/widgets.dart';

/// The speed test of the servers in the list: the button and its progress,
/// the figure in each row, the order by speed, the row's menu, the phone's
/// question about the traffic and the volume in the settings; on the
/// desktop (1400x900) and on a phone (390x844).
void main() {
  const phone = Size(390, 844);
  const desktop = Size(1400, 900);

  Future<AppState> pumpApp(WidgetTester tester, {Size size = desktop, DemoBackend? backend}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = AppState(backend ?? DemoBackend());
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

  List<String> onScreen(WidgetTester tester, List<String> names) {
    final ys = {for (final n in names) n: tester.getTopLeft(find.text(n).first).dy};
    return [...names]..sort((a, b) => ys[a]!.compareTo(ys[b]!));
  }

  Finder rowOf(String name) => find.ancestor(of: find.text(name), matching: find.byWidgetPredicate((w) => w.runtimeType.toString() == '_NodeRow')).first;

  /// The toolbar's button: «Скорость» on the desktop, an icon on a phone.
  Finder speedButton({required bool phone}) => phone
      ? find.byWidgetPredicate((w) => w is Tooltip && (w.message ?? '').startsWith('Проверить скорость скачивания показанных серверов'))
      : find.widgetWithText(Btn, 'Скорость');

  int usable(AppState s) => s.subscriptions.fold(0, (n, sub) => n + sub.nodes.where((x) => x.cores.isNotEmpty).length);

  /// Lets the demo daemon test for [seconds]. Its events reach the app on
  /// the real event loop, so every quarter second of the fake clock the
  /// real one gets a moment too.
  Future<void> run(WidgetTester tester, int seconds) async {
    for (var i = 0; i < seconds * 4; i++) {
      await tester.pump(const Duration(milliseconds: 250));
      await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 1)));
    }
    await tester.runAsync(() => Future.delayed(const Duration(milliseconds: 30)));
    await tester.pump();
  }

  group('sortServers by speed', () {
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
      for (final name in ['A', 'B', 'C', 'D', 'E']) (sub, n(name)),
    ];
    const speeds = {'A': NodeSpeed(mbps: 12.5), 'B': NodeSpeed(mbps: 80), 'C': NodeSpeed(error: 'nothing', mbps: 0), 'E': NodeSpeed(mbps: 12.5)};

    test('the fastest first, then the untested, the failed last, ties as listed', () {
      final out = sortServers(rows, 'speed', (_, _) => null, speedOf: (_, x) => speeds[x.name]);
      expect([for (final r in out) r.$2.name], ['B', 'A', 'E', 'D', 'C']);
    });

    test('a server no core runs counts as failed', () {
      final r = [(sub, n('Off', usable: false)), (sub, n('On'))];
      final out = sortServers(r, 'speed', (_, _) => null, speedOf: (_, _) => const NodeSpeed(mbps: 50));
      expect([for (final x in out) x.$2.name], ['On', 'Off']);
    });

    test('the colours follow the thresholds', () {
      expect(speedColor(48), okColor);
      expect(speedColor(30), okColor);
      expect(speedColor(29.9), warnColor);
      expect(speedColor(10), warnColor);
      expect(speedColor(9.9), errColor);
      expect(formatMbps(48.4), '48 Мбит/с');
      expect(formatMbps(4.84), '4.8 Мбит/с');
    });
  });

  testWidgets('the desktop tests every server at once: a button, progress, a figure in each row, no question', (tester) async {
    final state = await pumpApp(tester);
    await openServers(tester, phone: false);
    expect(state.speedSupported, isTrue);
    expect(find.text('Скорость'), findsNWidgets(2), reason: 'the column heading and the button');
    expect(find.widgetWithText(Btn, 'Проверить пинг'), findsOneWidget);
    expect(state.nodeSpeeds, isEmpty);

    await tester.tap(speedButton(phone: false));
    await tester.pump(const Duration(milliseconds: 600));
    // No dialog: only a phone asks.
    expect(find.text('Проверить скорость?'), findsNothing);
    expect(state.testingSpeed, isTrue);
    expect(find.textContaining('Скорость: '), findsOneWidget);
    expect(find.text('Остановить'), findsNWidgets(2), reason: 'the button turned into the stop, and the bar has one');
    expect(find.widgetWithText(Btn, 'Проверить пинг').evaluate().single.widget, isA<Btn>());
    final ping = tester.widget<Btn>(find.widgetWithText(Btn, 'Проверить пинг'));
    expect(ping.onPressed, isNull, reason: 'the latency test waits while the speed test downloads');

    await run(tester, 2);
    expect(state.speedDone, greaterThan(0));
    expect(find.textContaining('Скорость: ${state.speedDone} из ${state.speedTotal}'), findsOneWidget);

    await run(tester, 30);
    expect(state.testingSpeed, isFalse);
    expect(state.speedTotal, usable(state));
    expect(state.speedDone, usable(state));
    expect(find.textContaining('Скорость: '), findsNothing);
    // The toast names the traffic used.
    expect(find.textContaining('Скорость проверена:'), findsOneWidget);
    expect(state.speedBytes, greaterThan(0));
    // Figures in the rows, coloured by the thresholds.
    expect(find.text('94 Мбит/с'), findsOneWidget);
    expect(find.text('8.6 Мбит/с'), findsOneWidget);
    Color colorOf(String text) => tester.widget<Text>(find.text(text)).style!.color!;
    expect(colorOf('94 Мбит/с'), isNot(colorOf('8.6 Мбит/с')));
    expect(find.text('сбой'), findsWidgets, reason: 'a server through which nothing downloaded');
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 10));
  });

  testWidgets('sorted by speed, the fastest is first, the order is the speed\'s, not the ping\'s (desktop)', (tester) async {
    final state = await pumpApp(tester);
    await openServers(tester, phone: false);
    await tester.tap(speedButton(phone: false));
    await run(tester, 40);
    expect(state.testingSpeed, isFalse);

    await tester.tap(find.text('По пингу'));
    await tester.pump();
    expect(onScreen(tester, ['Amsterdam', 'Rotterdam', 'Warsaw', 'Frankfurt']), ['Amsterdam', 'Rotterdam', 'Warsaw', 'Frankfurt']);
    await tester.tap(find.text('По скорости'));
    await tester.pump();
    expect(state.serverSort, 'speed');
    expect(state.prefs['server_sort'], 'speed');
    // Frankfurt is faster than Warsaw although it answers later.
    expect(onScreen(tester, ['Amsterdam', 'Rotterdam', 'Warsaw', 'Frankfurt', 'Tokyo']), ['Amsterdam', 'Rotterdam', 'Frankfurt', 'Warsaw', 'Tokyo']);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 10));
  });

  testWidgets('the right-click menu tests one server', (tester) async {
    final state = await pumpApp(tester);
    await openServers(tester, phone: false);
    await tester.tap(find.text('Tokyo'), buttons: kSecondaryButton);
    await tester.pumpAndSettle();
    expect(find.text('Проверить скорость'), findsOneWidget);
    await tester.tap(find.text('Проверить скорость'));
    await tester.pump(const Duration(milliseconds: 200));
    expect(state.testingSpeed, isTrue);
    expect(state.speedTotal, 1);
    expect(find.textContaining('Скорость: 0 из 1'), findsOneWidget);
    await run(tester, 3);
    expect(state.testingSpeed, isFalse);
    expect(state.nodeSpeeds.length, 1);
    expect(find.descendant(of: rowOf('Tokyo'), matching: find.text('12 Мбит/с')), findsOneWidget);

    // Not while a test is on.
    await tester.tap(speedButton(phone: false));
    await tester.pump(const Duration(milliseconds: 600));
    await tester.ensureVisible(find.text('Tokyo'));
    await tester.pump();
    await tester.tap(find.text('Tokyo'), buttons: kSecondaryButton);
    await tester.pumpAndSettle();
    expect(find.text('Уже идёт проверка скорости'), findsOneWidget);
    await tester.tapAt(const Offset(5, 5));
    await tester.pumpAndSettle();
    await run(tester, 40);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 10));
  });

  testWidgets('stopping ends the test and keeps the figures so far', (tester) async {
    final state = await pumpApp(tester);
    await openServers(tester, phone: false);
    await tester.tap(speedButton(phone: false));
    await run(tester, 3);
    final before = state.speedDone;
    expect(before, greaterThan(0));
    await tester.tap(find.widgetWithText(Btn, 'Остановить').first);
    await run(tester, 2);
    expect(state.testingSpeed, isFalse);
    expect(state.nodeSpeeds.length, inInclusiveRange(before, usable(state) - 1));
    expect(find.textContaining('Проверка скорости остановлена'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 10));
  });

  testWidgets('a phone asks first: servers x megabytes = traffic, and says when it is mobile data', (tester) async {
    final backend = DemoBackend();
    final state = await pumpApp(tester, size: phone, backend: backend);
    await openServers(tester, phone: true);
    final n = usable(state);
    final cost = SpeedPlan(servers: n, megabytesEach: 10).estimate;
    expect(cost, '$n серверов × 10 МБ ≈ ${n * 10} МБ трафика');

    await tester.tap(speedButton(phone: true));
    await tester.pumpAndSettle();
    expect(find.text('$cost. Продолжить?'), findsOneWidget);
    expect(find.text('Вы на мобильной сети'), findsNothing);
    await tester.tap(find.text('Отмена'));
    await tester.pumpAndSettle();
    expect(state.testingSpeed, isFalse);
    expect(find.textContaining('Скорость: '), findsNothing);

    backend.mobileData = true;
    await tester.tap(speedButton(phone: true));
    await tester.pumpAndSettle();
    expect(find.text('$cost. Продолжить?'), findsOneWidget);
    expect(find.text('Вы на мобильной сети'), findsOneWidget);
    await tester.tap(find.text('Продолжить'));
    await tester.pump(const Duration(milliseconds: 800));
    expect(state.testingSpeed, isTrue);
    expect(find.textContaining('Скорость: '), findsOneWidget);
    expect(find.text('Остановить'), findsOneWidget, reason: 'the bar\'s: the toolbar has an icon');

    await run(tester, 40);
    expect(state.testingSpeed, isFalse);
    expect(find.text('94 Мбит/с'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 10));
  });

  testWidgets('a phone tests the servers it picked: hold a row, then the speed', (tester) async {
    final state = await pumpApp(tester, size: phone);
    await openServers(tester, phone: true);
    await tester.ensureVisible(rowOf('Helsinki'));
    await tester.pump();
    await tester.longPress(rowOf('Helsinki'));
    await tester.pump();
    expect(find.text('Выбрано: 1'), findsOneWidget);
    await tester.ensureVisible(find.byTooltip('Проверить скорость скачивания выбранных серверов'));
    await tester.pump();
    await tester.tap(find.byTooltip('Проверить скорость скачивания выбранных серверов'));
    await tester.pumpAndSettle();
    expect(find.text('1 сервер × 10 МБ ≈ 10 МБ трафика. Продолжить?'), findsOneWidget);
    await tester.tap(find.text('Продолжить'));
    await tester.pump(const Duration(milliseconds: 800));
    expect(find.textContaining('Выбрано'), findsNothing, reason: 'the picking ends');
    expect(state.speedTotal, 1);
    await run(tester, 3);
    expect(find.descendant(of: rowOf('Helsinki'), matching: find.text('33 Мбит/с')), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 10));
  });

  testWidgets('a phone orders by speed from the chip, and the row has room for the latency and the speed', (tester) async {
    final state = await pumpApp(tester, size: phone);
    await openServers(tester, phone: true);
    await tester.tap(find.byTooltip('Порядок серверов'));
    await tester.pumpAndSettle();
    expect(find.widgetWithText(CheckedPopupMenuItem<String>, 'По скорости'), findsOneWidget);
    await tester.tapAt(const Offset(5, 5));
    await tester.pumpAndSettle();

    await tester.tap(speedButton(phone: true));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Продолжить'));
    await run(tester, 40);
    expect(state.testingSpeed, isFalse);
    await tester.tap(find.byTooltip('Порядок серверов'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(CheckedPopupMenuItem<String>, 'По скорости'));
    await tester.pumpAndSettle();
    expect(state.serverSort, 'speed');
    expect(onScreen(tester, ['Amsterdam', 'Rotterdam', 'Frankfurt', 'Warsaw']), ['Amsterdam', 'Rotterdam', 'Frankfurt', 'Warsaw']);
    // The latency and the speed one above the other, in the same column.
    final row = rowOf('Amsterdam');
    final ping = tester.getRect(find.descendant(of: row, matching: find.textContaining('мс')).first);
    final speed = tester.getRect(find.descendant(of: row, matching: find.text('94 Мбит/с')));
    expect(speed.top, greaterThan(ping.bottom - 2));
    expect(speed.left, closeTo(ping.left, 2));
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 10));
  });

  for (final size in [desktop, phone]) {
    testWidgets('the latency with its TCP label and the speed fit the row (${size.width.round()})', (tester) async {
      // A phone's default test is a TCP ping, which has a label beside the number.
      final state = await pumpApp(tester, size: size, backend: DemoBackend(android: true));
      final isPhone = size.width < 600;
      await openServers(tester, phone: isPhone);
      await tester.tap(speedButton(phone: isPhone));
      await tester.pumpAndSettle();
      if (isPhone) await tester.tap(find.text('Продолжить'));
      await run(tester, 40);
      expect(state.testingSpeed, isFalse);
      final row = rowOf('Amsterdam');
      expect(find.descendant(of: row, matching: find.textContaining('TCP')), findsOneWidget);
      expect(find.descendant(of: row, matching: find.text('94 Мбит/с')), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pump(const Duration(seconds: 10));
    });
  }

  testWidgets('the figures come with the servers when the app starts again', (tester) async {
    final backend = DemoBackend();
    final first = await pumpApp(tester, backend: backend);
    await openServers(tester, phone: false);
    await tester.tap(speedButton(phone: false));
    await run(tester, 40);
    expect(first.nodeSpeeds, isNotEmpty);
    final kept = first.nodeSpeeds.length;
    await tester.pumpWidget(const SizedBox());

    // A new app, the same service: the figures are in the list at once.
    final second = await pumpApp(tester, backend: backend);
    expect(second.nodeSpeeds.length, kept);
    expect(second.testingSpeed, isFalse);
    await openServers(tester, phone: false);
    expect(find.text('94 Мбит/с'), findsOneWidget);
    final amsterdam = second.subscriptions.expand((s) => s.nodes).firstWhere((x) => x.name.endsWith('Amsterdam'));
    expect(amsterdam.speed!.mbps, 94.3);
    expect(amsterdam.speed!.at, isNotNull);
    expect(tester.takeException(), isNull);
    await tester.pump(const Duration(seconds: 10));
  });

  for (final size in [desktop, phone]) {
    testWidgets('the volume per server is in the settings: 5, 10 or 25 MB, and the question follows it (${size.width.round()})', (tester) async {
      final state = await pumpApp(tester, size: size);
      final isPhone = size.width < 600;
      Finder nav(String p) => isPhone ? find.descendant(of: find.byType(NavigationBar), matching: find.text(p)) : find.text(p).first;
      await tester.tap(nav('Настройки'));
      await tester.pump();
      expect(find.text('Проверка скорости'), findsOneWidget);
      await tester.ensureVisible(find.text('Проверка скорости'));
      expect(state.speedVolumeMB, 10);
      expect(find.text('5 МБ'), findsOneWidget);
      expect(find.text('10 МБ'), findsOneWidget);
      expect(find.text('25 МБ'), findsOneWidget);
      // Beside the switch of the latency test.
      expect(tester.getTopLeft(find.text('Проверка задержки')).dy, lessThan(tester.getTopLeft(find.text('Проверка скорости')).dy));
      await tester.runAsync(() async {
        await tester.tap(find.text('25 МБ'));
        await Future.delayed(const Duration(milliseconds: 300));
      });
      await tester.pump();
      expect(state.speedVolumeMB, 25);
      expect(state.setting('cores.speed_mb', 0), 25);
      expect(tester.takeException(), isNull);

      if (isPhone) {
        await tester.tap(nav('Серверы'));
        await tester.pump();
        await tester.pump(const Duration(seconds: 30));
        await tester.tap(speedButton(phone: true));
        await tester.pumpAndSettle();
        final n = usable(state);
        expect(find.textContaining('× 25 МБ ≈ ${n * 25} МБ трафика'), findsOneWidget);
        await tester.tap(find.text('Отмена'));
        await tester.pumpAndSettle();
      }
      await tester.pump(const Duration(seconds: 10));
    });
  }
}
