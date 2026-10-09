import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/platform/platform.dart';
import 'package:coreshift/state/app_state.dart';

Future<AppState> _started(VpnConsent consent, {Future<bool> Function()? opener}) async {
  final state = AppState(DemoBackend(), vpnConsent: () async => consent, vpnSettingsOpener: opener);
  addTearDown(state.dispose);
  state.start();
  for (var i = 0; i < 50 && !state.loaded; i++) {
    await Future.delayed(const Duration(milliseconds: 20));
  }
  return state;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // With another app as the always-on VPN, Android refuses at once without
  // asking: «Подключить» pressed 15 times left only "действие подключить".
  test('a VPN refused without asking is told in the journal, with the way out', () async {
    var opened = 0;
    final state = await _started(
      VpnConsent.unasked,
      opener: () async {
        opened++;
        return true;
      },
    );
    await state.connect();
    final line = state.logs.lastWhere((l) => l.source == 'VPN');
    expect(line.level, LogLevel.err);
    expect(line.message, contains('не показав запрос'));
    expect(line.message, contains('«Постоянная VPN»'));
    expect(line.message, contains('настройки VPN'));
    expect(state.toasts.single.kind, ToastKind.err);
    expect(state.status.state, ConnState.idle);
    expect(state.busy, isFalse);
    // The home page offers Android's VPN settings until it is allowed.
    expect(state.vpnBlocked, isTrue);
    await state.openVpnSettings();
    expect(opened, 1);
    state.dismissVpnRefusal();
    expect(state.vpnBlocked, isFalse);
  });

  test('a VPN request the user declined is told too', () async {
    final state = await _started(VpnConsent.denied, opener: () async => false);
    await state.connect();
    final line = state.logs.lastWhere((l) => l.source == 'VPN');
    expect(line.level, LogLevel.err);
    expect(line.message, startsWith('Android не разрешил VPN: запрос отклонён'));
    expect(state.vpnRefusal, VpnConsent.denied);
    // No settings screen opened: the toast says where to find it.
    await state.openVpnSettings();
    expect(state.toasts.map((t) => t.message), contains(startsWith('Не удалось открыть настройки VPN')));
  });

  test('an allowed VPN connects and clears an earlier refusal', () async {
    final state = await _started(VpnConsent.granted);
    state.vpnRefusal = VpnConsent.unasked;
    await state.connect();
    expect(state.vpnRefusal, isNull);
    expect(state.logs.where((l) => l.source == 'VPN'), isEmpty);
    expect(state.status.active, isTrue);
  });
}
