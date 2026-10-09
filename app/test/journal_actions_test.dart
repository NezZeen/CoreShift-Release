import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/state/app_state.dart';

/// Every change of the connection says why in the journal: the actions taken
/// outside the window (Android's tile and notification, "Автозапуск", the
/// tray) and the service's own reasons.
void main() {
  test('actions from outside the window and the service reasons are journalled', () {
    final state = AppState(DemoBackend());
    final now = DateTime.now();
    state.injectEvent(Event(time: now, kind: 'action', line: 'отключить (плитка)'));
    expect((state.logs.last.source, state.logs.last.message), ('действие', 'отключить (плитка)'));

    state.injectEvent(Event(time: now.add(const Duration(seconds: 1)), kind: 'action', line: 'подключить: Германия (автозапуск)'));
    expect((state.logs.last.source, state.logs.last.message), ('действие', 'подключить: Германия (автозапуск)'));

    state.injectEvent(
      Event(time: now.add(const Duration(seconds: 2)), kind: 'action', source: 'обновление', line: 'отключаюсь, чтобы установить версию 0.8.2'),
    );
    expect((state.logs.last.source, state.logs.last.message), ('обновление', 'отключаюсь, чтобы установить версию 0.8.2'));

    const changed = 'сеть сменилась: DNS 192.168.1.1 больше нет, теперь система спрашивает 10.0.0.1; переподключаюсь';
    state.injectEvent(Event(time: now.add(const Duration(seconds: 3)), kind: 'dns', reason: 'network-changed', line: changed));
    expect((state.logs.last.source, state.logs.last.message), ('сеть', changed));
    state.dispose();
  });

  test('the tray says so', () async {
    final state = AppState(DemoBackend());
    await state.disconnect(from: 'трей');
    expect(state.logs.map((l) => '${l.source}  ${l.message}'), contains('действие  отключить (трей)'));
    await state.connect(from: 'трей');
    expect(state.logs.map((l) => '${l.source}  ${l.message}'), contains(startsWith('действие  подключить')));
    expect(state.logs.where((l) => l.source == 'действие').last.message, endsWith(' (трей)'));
    state.dispose();
  });
}
