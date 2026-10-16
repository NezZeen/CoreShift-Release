import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:coreshift/api/backend.dart';
import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';

/// The servers of `fixtures/big_list_servers.json`: a big subscription of
/// 132 servers (flags, emoji and decorations in the names), with the
/// credentials and addresses made up.
List<Json> loadBigListNodes() {
  final j = jsonDecode(File('test/fixtures/big_list_servers.json').readAsStringSync()) as Json;
  return [for (final n in j['nodes'] as List) (n as Map).cast<String, dynamic>()];
}

/// Names the way providers decorate them: flags, emoji, symbols and
/// separators, some very long, some with a lone regional indicator, some
/// without a flag at all. [seed] makes the set the same every time.
List<Json> syntheticNodes(int count, {int seed = 5}) {
  final rnd = Random(seed);
  const protocols = ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'anytls', 'wireguard'];
  const transports = ['tcp', 'ws', 'grpc', 'xhttp', 'quic', 'udp'];
  const decor = ['✦', '❄', '🔥', '⭐️', '🚀', '👉', '⚡', '💎', '🛡️', '🌐', '👨‍👩‍👧‍👦', '🏳️‍🌈', '㊙️', '•', '|', '—', '·'];
  const words = [
    'Финляндия',
    'Швеция — Stockholm',
    'Germany',
    'Нидерланды',
    'Amsterdam',
    'Токио',
    'Premium',
    'telegram-only',
    'Россия',
    'USA',
    'New York',
    'Singapore',
    'Турция',
    'Almaty',
    'بغداد',
    '東京',
    'Server',
  ];
  String flag() {
    // Any pair of letters, also those that are no country.
    final a = 65 + rnd.nextInt(26), b = 65 + rnd.nextInt(26);
    return String.fromCharCodes([0x1F1E6 + a - 65, 0x1F1E6 + b - 65]);
  }

  String pick(List<String> l) => l[rnd.nextInt(l.length)];
  final out = <Json>[];
  for (var i = 0; i < count; i++) {
    final parts = <String>[];
    final shape = rnd.nextInt(10);
    if (shape < 7) parts.add(flag());
    if (shape == 7) parts.add(String.fromCharCode(0x1F1E6 + rnd.nextInt(26))); // a lone indicator
    for (var k = rnd.nextInt(4); k > 0; k--) {
      parts.add(pick(decor));
    }
    parts.add(pick(words));
    for (var k = rnd.nextInt(3); k > 0; k--) {
      parts.add(pick(decor));
    }
    if (rnd.nextInt(12) == 0) parts.add(List.generate(60 + rnd.nextInt(140), (_) => pick(decor)).join());
    if (rnd.nextBool()) parts.add('#$i');
    final protocol = pick(protocols);
    out.add({
      'fingerprint': 'syn${i.toString().padLeft(4, '0')}',
      'name': parts.join(' '),
      'protocol': protocol,
      'transport': pick(transports),
      'security': rnd.nextBool() ? 'tls' : 'reality',
      'server': 'h$i.example.test',
      'port': 443,
      'cores': rnd.nextInt(15) == 0 ? <String>[] : ['xray', 'sing-box', 'mihomo'],
    });
  }
  return out;
}

/// The demo daemon with another subscription, and a latency test whose
/// results arrive the way a test asks: all at once, or one by one.
class FixtureBackend implements Backend {
  final DemoBackend inner = DemoBackend();
  final List<Json> nodes;
  final String subId;
  final _events = StreamController<Event>.broadcast();

  /// How the test's results arrive: [burst] results in a row, then a pause
  /// of [gap]; 0 for [gap] is no pause at all.
  int burst;
  Duration gap;

  /// Nodes (indices) that fail the test.
  final Set<int> failing;
  int latencyStarted = 0;

  FixtureBackend(this.nodes, {this.subId = 'fx1', this.burst = 1, this.gap = const Duration(milliseconds: 10), this.failing = const {}});

  Json get _sub {
    final now = DateTime.now();
    return {
      'id': subId,
      'name': '',
      'display_name': 'ExampleVPN',
      'url': 'https://sub.example.test/api/TOKEN',
      'info': {'title': 'ExampleVPN', 'upload': 0, 'download': 0, 'total': 1 << 40, 'update_interval_hours': 1, 'support_url': 'https://t.me/example_support'},
      'format': 'links',
      'nodes': nodes,
      'added_at': now.toUtc().toIso8601String(),
      'updated_at': now.toUtc().toIso8601String(),
      'checked_at': now.toUtc().toIso8601String(),
      'next_update': now.add(const Duration(hours: 1)).toUtc().toIso8601String(),
    };
  }

  /// The result for node [i], as the daemon's event.
  Event latencyEvent(int i) => Event.fromJson({
    'time': DateTime.now().toUtc().toIso8601String(),
    'kind': 'latency',
    'subscription': subId,
    'fingerprint': nodes[i]['fingerprint'],
    'core': 'xray',
    'method': 'tcp',
    if (failing.contains(i)) 'error': 'context deadline exceeded' else 'latency_ms': 20 + (i * 37) % 700,
  });

  void emit(Event e) => _events.add(e);

  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    final p = path.replaceFirst('/v1', '');
    switch ('$method $p') {
      case 'GET /subscriptions':
        return [_sub];
      case 'GET /selection':
        final first = nodes.firstWhere((n) => (n['cores'] as List).isNotEmpty);
        return {'subscription': subId, 'fingerprint': first['fingerprint'], 'name': first['name']};
      case 'POST /latency':
        latencyStarted++;
        emit(Event.fromJson({'time': DateTime.now().toUtc().toIso8601String(), 'kind': 'latency', 'reason': 'started'}));
        for (var i = 0; i < nodes.length; i++) {
          emit(latencyEvent(i));
          if (burst > 0 && (i + 1) % burst == 0) await Future.delayed(gap);
        }
        emit(Event.fromJson({'time': DateTime.now().toUtc().toIso8601String(), 'kind': 'latency', 'reason': 'finished'}));
        return [];
    }
    return inner.call(method, path, body);
  }

  /// Only this backend's own events: the demo's would be listened to from
  /// the zone that started the app, whose microtasks a widget test does not run.
  @override
  Stream<Event> events() async* {
    yield* _events.stream;
  }

  @override
  String get description => inner.description;

  @override
  String get view => inner.view;
}
