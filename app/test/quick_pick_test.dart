import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/state/app_state.dart';

/// «Другой сервер» on the banner of a server that does not answer offered
/// that very server as «Самый быстрый»: its ping was from before it went
/// down.
void main() {
  test('the fastest server is another one while the connected one is down', () async {
    final state = AppState(DemoBackend());
    state.start();
    for (var i = 0; i < 50 && !state.loaded; i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    await state.testLatency();
    for (var i = 0; i < 100 && state.fastestNode() == null; i++) {
      await Future.delayed(const Duration(milliseconds: 50));
    }
    final fastest = state.fastestNode();
    expect(fastest, isNotNull);
    await state.connect(subscription: fastest!.$1.id, fingerprint: fastest.$2.fingerprint, name: fastest.$2.name);
    for (var i = 0; i < 100 && state.status.state != ConnState.connected; i++) {
      await Future.delayed(const Duration(milliseconds: 50));
    }
    expect(state.status.state, ConnState.connected);
    expect(state.fastestNode()?.$2.fingerprint, fastest.$2.fingerprint, reason: 'answering: it may stay the fastest');

    state.injectEvent(Event(time: DateTime.now(), kind: 'server', reason: 'server-down', from: state.status.node));
    expect(state.serverUnresponsive, isTrue);
    final other = state.fastestNode();
    expect(other, isNotNull);
    expect(other!.$2.fingerprint, isNot(fastest.$2.fingerprint));

    state.injectEvent(Event(time: DateTime.now(), kind: 'server', reason: 'ok', from: state.status.node));
    expect(state.fastestNode()?.$2.fingerprint, fastest.$2.fingerprint);
    await state.disconnect();
  });
}
