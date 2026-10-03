import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/state/app_state.dart';

void main() {
  test('the copied journal keeps the cores\' warnings and errors, not their start-up', () {
    final state = AppState(DemoBackend());
    addTearDown(state.dispose);
    final t0 = DateTime(2026, 10, 3, 12, 25, 51);
    void log(DateTime t, String source, String line) => state.injectEvent(Event(time: t, kind: 'log', source: source, line: line));

    state.injectEvent(Event(time: t0, kind: 'state', state: 'connecting'));
    log(t0, 'xray', 'Xray 26.3.27 (Xray, Penetrates Everything.) d2758a0 (go1.26.1 windows/amd64)');
    log(t0, 'xray', 'A unified platform for anti-censorship.');
    log(t0, 'xray', r'2026/10/03 12:25:51.210168 [Info] infra/conf/serial: Reading config: &{Name:C:\ProgramData\CoreShift\work\xray\config.json Format:json}');
    log(t0, 'xray', '2026/10/03 12:25:51.212236 [Warning] core: Xray 26.3.27 started');
    log(t0, 'xray', '2026/10/03 12:25:51.300000 [Info] app/dispatcher: taking detour [proxy]');
    state.injectEvent(Event(time: t0.add(const Duration(seconds: 1)), kind: 'state', state: 'connected', core: 'xray'));
    log(t0.add(const Duration(minutes: 1)), 'xray', '2026/10/03 12:26:51.000000 [Warning] transport/internet/splithttp: slow response');
    log(
      t0.add(const Duration(minutes: 2)),
      'tun',
      'ERROR [74448989 5.3s] connection: open connection to 192.0.2.1:443 using outbound/direct[direct]: i/o timeout',
    );
    // Around a failure everything the core printed is kept.
    final fail = t0.add(const Duration(minutes: 5));
    log(fail.subtract(const Duration(seconds: 3)), 'xray', '2026/10/03 12:30:48.000000 [Info] proxy/vless/outbound: dial to server timed out');
    state.injectEvent(Event(time: fail, kind: 'core-failed', core: 'xray', reason: 'health-check', error: 'no answer'));

    final copy = state.journalForSupport().join('\n');
    expect(copy, contains('CoreShift: приложение'));
    for (final gone in ['Penetrates Everything', 'anti-censorship', 'Reading config', r'C:\ProgramData', 'Xray 26.3.27 started', 'taking detour']) {
      expect(copy, isNot(contains(gone)), reason: gone);
    }
    for (final kept in [
      'служба  подключение…',
      'служба  подключено',
      '[Warning] transport/internet/splithttp: slow response',
      'tun  ERROR [74448989 5.3s]',
      'dial to server timed out',
      'xray  отключено',
    ]) {
      expect(copy, contains(kept), reason: kept);
    }
    // The page still has every line, for a search.
    expect(state.logs.where((l) => l.output), hasLength(8));
  });
}
