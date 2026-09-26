// Runs against a real daemon when CORESHIFT_API_FILE points at its api.json:
//
//   coreshiftd serve -data-dir <dir> ...
//   CORESHIFT_API_FILE=<dir>/api.json flutter test test/live_daemon_test.dart
//
// It only reads state and edits a subscription it adds itself.
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/platform/platform_io.dart';
import 'package:coreshift/state/app_state.dart';

void main() {
  final file = Platform.environment['CORESHIFT_API_FILE'];

  test('talks to a live daemon', () async {
    final backend = HttpBackend(file!);
    final info = DaemonInfo.fromJson(await backend.call('GET', '/v1/info') as Json);
    expect(info.version, isNotEmpty);
    expect(info.cores.map((c) => c.kind), containsAll(['xray', 'sing-box', 'mihomo']));

    final events = <Event>[];
    final sub = backend.events().listen(events.add);

    final state = AppState(backend);
    state.settings = await backend.call('GET', '/v1/settings') as Json;
    // Leftovers of an earlier failed run.
    for (final s in (await backend.call('GET', '/v1/subscriptions') as List).cast<Json>()) {
      if (s['name'] == 'live test') await backend.call('DELETE', '/v1/subscriptions/${s['id']}');
    }
    final added = await state.addSubscription(
      source: 'trojan://pw@203.0.113.77:443?sni=live.example.com#Live-Test\nhy2://a@203.0.113.78:443/?sni=h.example.com#Live-Hy2',
      name: 'live test',
    );
    expect(added, isNull);
    final created = state.subscriptions.firstWhere((s) => s.name == 'live test');
    expect(created.nodes.map((n) => n.name), ['Live-Test', 'Live-Hy2']);
    expect(created.nodes[1].cores, ['sing-box', 'mihomo']);

    final err = await state.updateSettings((s) => s['dns']['remote'] = 'gopher://x');
    expect(err, contains('dns.remote'));

    await state.removeSubscription(created.id);
    await Future.delayed(const Duration(milliseconds: 300));
    await sub.cancel();
    expect(events.where((e) => e.kind == 'store').map((e) => e.reason), containsAll(['subscription-added', 'subscription-removed']));

    await expectLater(HttpBackend('$file.missing').call('GET', '/v1/status'), throwsA(isA<DaemonOffline>()));
  }, skip: file == null ? 'set CORESHIFT_API_FILE to run against a live daemon' : null);
}
