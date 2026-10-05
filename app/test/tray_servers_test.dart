import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/platform/desktop_io.dart';
import 'package:coreshift/state/app_state.dart';

/// The tray's «Сервер» menu follows the window with the VPN off too: the
/// menu used to be rebuilt only when the icon or its tooltip changed.
void main() {
  test('the tray menu follows a server picked with the VPN off', () async {
    final state = AppState(DemoBackend());
    state.start();
    for (var i = 0; i < 50 && !state.loaded; i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    expect(state.status.active, isFalse);
    final (list, key) = trayServers(state);
    expect(list, isNotEmpty);
    expect(list.every((r) => r.$1.id == state.selection.subscription), isTrue);
    final ticked = list.where((r) => state.isSelected(r.$1, r.$2)).single;

    // Another server of the same subscription, picked in the window.
    final other = list.firstWhere((r) => r != ticked);
    await state.selectNode(other.$1.id, other.$2.fingerprint, other.$2.name);
    final (list2, key2) = trayServers(state);
    expect(key2, isNot(key));
    expect(state.isSelected(list2.firstWhere((r) => r.$2.fingerprint == other.$2.fingerprint).$1, other.$2), isTrue);

    // The servers of another subscription once one of them is picked.
    final sub2 = state.subscriptions.firstWhere((s) => s.id != other.$1.id);
    await state.selectNode(sub2.id, sub2.nodes.first.fingerprint, sub2.nodes.first.name);
    final (list3, key3) = trayServers(state);
    expect(key3, isNot(key2));
    expect(list3.every((r) => r.$1.id == sub2.id), isTrue);
    expect(trayServers(state, max: 1).$1, hasLength(1));
  });
}
