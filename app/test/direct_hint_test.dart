import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';

/// The demo service, recording what the app asks of it; [directBlocked]
/// puts the engine's flag into the status.
class _RecordingBackend extends DemoBackend {
  final calls = <(String, String, Object?)>[];
  bool directBlocked = false;

  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    calls.add((method, path, body));
    final r = await super.call(method, path, body);
    if (method == 'GET' && path == '/v1/status' && directBlocked) return {...(r as Json), 'direct_blocked': true};
    return r;
  }
}

void main() {
  Future<(AppState, _RecordingBackend)> pumpConnected(WidgetTester tester, Size size) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final backend = _RecordingBackend();
    final state = AppState(backend);
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    await tester.runAsync(() async {
      await state.connect();
      await Future.delayed(const Duration(milliseconds: 2300));
    });
    await tester.pump();
    expect(state.status.state, ConnState.connected);
    return (state, backend);
  }

  /// Moves the test's clock on, through the demo service's delays.
  Future<void> advance(WidgetTester tester) async {
    for (var i = 0; i < 30; i++) {
      await tester.pump(const Duration(milliseconds: 100));
    }
  }

  const title = 'Сайты напрямую не открываются';
  Event direct() =>
      Event(time: DateTime.now(), kind: 'direct', reason: 'blocked', line: 'Прямые соединения не проходят (40 за минуту), а через VPN всё работает');

  testWidgets('the hint comes with the event and its button sends everything through the VPN', (tester) async {
    for (final size in [const Size(390, 844), const Size(1400, 900)]) {
      final (state, backend) = await pumpConnected(tester, size);
      expect(find.text(title), findsNothing, reason: 'before the event at $size');
      expect(state.setting('routing.russia_direct', false), isTrue);

      state.injectEvent(direct());
      await tester.pump();
      expect(find.text(title), findsOneWidget, reason: 'after the event at $size');
      expect(state.logs.last.message, startsWith('Прямые соединения не проходят'));
      expect(tester.takeException(), isNull);

      backend.calls.clear();
      await tester.ensureVisible(find.text('Всё через VPN').last);
      await tester.tap(find.text('Всё через VPN').last);
      // The tap runs in the test's clock: saving, then reconnecting.
      await advance(tester);

      final puts = backend.calls.where((c) => c.$1 == 'PUT' && c.$2 == '/v1/settings').toList();
      expect(puts, hasLength(1), reason: 'settings at $size');
      final routing = (puts.single.$3 as Map)['routing'] as Map;
      expect(routing['mode'], 'all');
      expect(routing['russia_direct'], isFalse);
      final put = backend.calls.indexWhere((c) => c.$1 == 'PUT' && c.$2 == '/v1/settings');
      final re = backend.calls.indexWhere((c) => c.$1 == 'POST' && c.$2 == '/v1/reconnect');
      expect(re, greaterThan(put), reason: 'reconnects after saving at $size');
      expect(state.setting('routing.russia_direct', true), isFalse);
      expect(find.text(title), findsNothing, reason: 'fixed at $size');

      await tester.runAsync(() => state.disconnect());
      await tester.pump(const Duration(seconds: 1));
    }
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('the status flag shows the hint; «Не сейчас» puts it off without changing settings', (tester) async {
    final (state, backend) = await pumpConnected(tester, const Size(390, 844));
    backend.directBlocked = true;
    // A later status, as when the app opens on a connection found so.
    state.injectEvent(Event(time: DateTime.now(), kind: 'options'));
    await advance(tester);
    expect(state.status.directBlocked, isTrue);
    expect(find.text(title), findsOneWidget);

    backend.calls.clear();
    await tester.ensureVisible(find.text('Не сейчас'));
    await tester.tap(find.text('Не сейчас'));
    await tester.pump();
    expect(find.text(title), findsNothing);
    expect(backend.calls.where((c) => c.$1 != 'GET'), isEmpty, reason: 'nothing changes without the fix');

    // Put off for this run of the app, even on the next event.
    state.injectEvent(direct());
    await tester.pump();
    expect(find.text(title), findsNothing);

    await tester.runAsync(() => state.disconnect());
    await tester.pump(const Duration(seconds: 1));
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('with only the user\'s own direct lists the hint leads to the rules', (tester) async {
    final (state, _) = await pumpConnected(tester, const Size(1400, 900));
    state.updateSettings((s) => s['routing']['russia_direct'] = false);
    await advance(tester);
    state.injectEvent(direct());
    await tester.pump();
    expect(find.text(title), findsOneWidget);
    expect(state.directFixable, isFalse);
    expect(find.text('Правила'), findsWidgets);
    expect(find.textContaining('Уберите из своих списков'), findsOneWidget);

    // A new connection starts without it.
    state.injectEvent(Event(time: DateTime.now(), kind: 'state', state: 'connecting'));
    await tester.pump();
    expect(state.directHint, isFalse);

    await tester.runAsync(() => state.disconnect());
    await tester.pump(const Duration(seconds: 1));
    await tester.pump(const Duration(seconds: 6));
  });
}
