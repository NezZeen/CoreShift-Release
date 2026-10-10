import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/platform/desktop_io.dart' show freezeWhenHidden;
import 'package:coreshift/state/app_state.dart';

/// «После переключения вкладок ничего не происходит»: a phone upgraded from
/// 0.8.1 showed the home page whatever was tapped. The journal is verbose
/// since 0.9.3 (the cores log at info level), so the navigation is tried
/// under hundreds of lines of core output a second, a server that does not
/// answer, and the first start of an existing user after the upgrade.
void main() {
  const phone = Size(390, 844);

  Future<AppState> pumpApp(WidgetTester tester, Size size, {Map<String, dynamic>? prefs, bool ask = false}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = AppState(DemoBackend(), prefs: prefs, askDisclaimer: ask, askWizard: ask);
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

  Finder nav(String label, Size size) => size == phone ? find.descendant(of: find.byType(NavigationBar), matching: find.text(label)) : find.text(label).first;

  /// Which page is shown, by something only it has.
  bool onHome(WidgetTester t) => t.any(find.text('Другой сервер'));
  bool onServers(WidgetTester t) => t.any(find.text('Добавить подписку')) || t.any(find.text('Добавить'));
  bool onSettings(WidgetTester t) => t.any(find.text('Прочитать'));

  for (final (name, size) in [('phone', phone), ('desktop', const Size(1400, 900))]) {
    testWidgets('$name: a storm of core output and a server that does not answer; the navigation still works', (tester) async {
      final state = await pumpApp(tester, size);
      state.connect();
      for (var i = 0; i < 60 && state.status.state != ConnState.connected; i++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      expect(state.status.state, ConnState.connected);
      state.injectEvent(Event(time: DateTime.now(), kind: 'server', reason: 'server-down', from: state.status.node));
      await tester.pump(const Duration(milliseconds: 300));
      expect(onHome(tester), isTrue, reason: 'the red banner of the recording');

      var t = DateTime.now();
      var n = 0;
      var notifies = 0;
      state.addListener(() => notifies++);
      final frames = <int>[];
      final watch = Stopwatch();
      Future<void> frame() async {
        // ~200 lines a second at 60 frames a second, and the traffic.
        for (var k = 0; k < 3; k++) {
          t = t.add(const Duration(milliseconds: 5));
          state.injectEvent(
            Event(time: t, kind: 'log', source: 'xray', line: '2026/10/10 12:00:00 [Info] app/proxyman/outbound: connection ${n++} to example.com:443 failed'),
          );
        }
        if (n % 60 == 0) state.injectEvent(Event(time: t, kind: 'traffic', up: n, down: n * 2, upRate: 1000, downRate: 5000));
        watch
          ..reset()
          ..start();
        await tester.pump(const Duration(milliseconds: 16));
        watch.stop();
        frames.add(watch.elapsedMicroseconds);
      }

      Future<void> storm(int frameCount) async {
        for (var i = 0; i < frameCount; i++) {
          await frame();
        }
      }

      await storm(60);
      await tester.tap(nav('Серверы', size));
      await storm(30);
      expect(onServers(tester), isTrue, reason: 'a tap on «Серверы» in the storm');
      await tester.tap(nav('Настройки', size));
      await storm(30);
      expect(onSettings(tester), isTrue, reason: 'a tap on «Настройки» in the storm');
      await tester.tap(nav('Главная', size));
      await storm(30);
      expect(onHome(tester), isTrue, reason: 'and back home');

      frames.sort();
      // ignore: avoid_print
      print(
        '$name: ${frames.length} frames, median ${frames[frames.length ~/ 2]} us, p95 ${frames[frames.length * 95 ~/ 100]} us, lines $n, notifies $notifies',
      );
      // About 500 lines of output used to rebuild the page 25 times.
      expect(notifies, lessThan(30));
      expect(tester.takeException(), isNull);
      state.disconnect();
      await tester.pump(const Duration(seconds: 2));
      await tester.pump(const Duration(seconds: 30));
    });
  }

  testWidgets('an existing user starting after the upgrade: nothing covers the shell once the disclaimer is answered', (tester) async {
    // 0.8.1 knew no disclaimer and no wizard: the prefs have neither.
    await pumpApp(tester, phone, prefs: {'last_version': '0.8.1'}, ask: true);
    await tester.pump(const Duration(milliseconds: 600));
    expect(find.text('Отказ от ответственности'), findsOneWidget);
    await tester.ensureVisible(find.text('Принимаю'));
    await tester.tap(find.text('Принимаю'));
    await tester.pumpAndSettle();
    expect(find.text('Отказ от ответственности'), findsNothing);
    // Only the shell's own route is left: no barrier, no hidden window.
    final navigator = tester.state<NavigatorState>(find.byType(Navigator).first);
    expect(navigator.canPop(), isFalse, reason: 'a route is left over the shell');
    await tester.tap(nav('Серверы', phone));
    await tester.pump();
    expect(onServers(tester), isTrue);
    await tester.tap(nav('Главная', phone));
    await tester.pump();
    await tester.pump(const Duration(seconds: 30));
  });

  testWidgets('the banner of a server that does not answer: its sheets close and leave the navigation working', (tester) async {
    final state = await pumpApp(tester, phone);
    state.connect();
    for (var i = 0; i < 60 && state.status.state != ConnState.connected; i++) {
      await tester.pump(const Duration(milliseconds: 100));
    }
    state.injectEvent(Event(time: DateTime.now(), kind: 'server', reason: 'server-down', from: state.status.node));
    await tester.pump(const Duration(milliseconds: 300));
    final navigator = tester.state<NavigatorState>(find.byType(Navigator).first);

    // «Другой сервер»: the quick pick, closed by a tap beside it.
    await tester.tap(find.text('Другой сервер'));
    await tester.pumpAndSettle();
    expect(find.text('Выбор сервера'), findsOneWidget);
    await tester.tapAt(const Offset(195, 40));
    await tester.pumpAndSettle();
    expect(find.text('Выбор сервера'), findsNothing);
    expect(navigator.canPop(), isFalse);
    await tester.tap(nav('Серверы', phone));
    await tester.pump();
    expect(onServers(tester), isTrue, reason: 'the navigation after the quick pick');
    await tester.tap(nav('Главная', phone));
    await tester.pump();

    // «Проверить всё»: the checkup sheet, closed by a swipe down.
    await tester.tap(find.text('Проверить всё').first);
    await tester.pump(const Duration(milliseconds: 100));
    await tester.pump(const Duration(milliseconds: 600));
    await tester.fling(find.byType(BottomSheet), const Offset(0, 600), 2000);
    await tester.pump(const Duration(seconds: 2));
    expect(navigator.canPop(), isFalse, reason: 'the checkup sheet is gone');
    await tester.tap(nav('Настройки', phone));
    await tester.pump();
    expect(onSettings(tester), isTrue, reason: 'the navigation after the checkup');
    state.disconnect();
    await tester.pump(const Duration(seconds: 2));
    await tester.pump(const Duration(seconds: 60));
  });

  testWidgets('an app believed hidden (the lifecycle said so) answers the first tap, animations included', (tester) async {
    final state = await pumpApp(tester, phone);
    state.setShown(false);
    await tester.pump(const Duration(milliseconds: 100));
    await tester.tap(nav('Серверы', phone));
    await tester.pump();
    expect(state.shown.value, isTrue, reason: 'a touch says the window is on screen');
    expect(onServers(tester), isTrue);
    state.setShown(false);
    await tester.pump();
    await tester.tap(nav('Главная', phone));
    await tester.pump();
    state.setShown(false);
    await tester.pump();
    await tester.tap(find.text('Amsterdam').last);
    await tester.pumpAndSettle();
    expect(find.text('Выбор сервера'), findsOneWidget, reason: 'a sheet opened by the first tap of a window believed hidden still comes up');
    await tester.tapAt(const Offset(195, 40));
    await tester.pumpAndSettle();
    expect(find.text('Выбор сервера'), findsNothing);
    await tester.pump(const Duration(seconds: 30));
  });

  // On Android nothing freezes: the system draws no frames for an app in the
  // background, and an app wrongly believed hidden there froze its sheets.
  testWidgets('on Android an app believed hidden keeps its animations', (tester) async {
    freezeWhenHidden = false;
    addTearDown(() => freezeWhenHidden = true);
    final state = await pumpApp(tester, phone);
    state.setShown(false);
    await tester.pump();
    expect(TickerMode.valuesOf(tester.element(find.byType(Scaffold).first)).enabled, isTrue);
    await tester.tap(find.text('Amsterdam').last);
    await tester.pumpAndSettle();
    expect(find.text('Выбор сервера'), findsOneWidget);
    expect(state.shown.value, isTrue, reason: 'the touch still says the app is on screen, for the speed');
    await tester.tapAt(const Offset(195, 40));
    await tester.pumpAndSettle();
    await tester.pump(const Duration(seconds: 30));
  });
}
