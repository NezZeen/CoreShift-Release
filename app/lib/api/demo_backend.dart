import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'backend.dart';
import 'models.dart';
import '../version.dart';

/// A simulated daemon for previewing the UI (`--dart-define=DEMO=true`, and
/// always on the web). It answers the same endpoints as the real one.
class DemoBackend implements Backend {
  final _events = StreamController<Event>.broadcast();
  final _rand = Random(7);
  final _recent = <Event>[];
  Timer? _health;
  Timer? _crash;
  Timer? _traffic;
  int _up = 0, _down = 0;
  final Map<String, String> _versions = {'xray': '26.3.27', 'sing-box': '1.14.2', 'mihomo': '1.19.31'};

  Json _settings = {
    'tun': true,
    'ipv6': true,
    'auto_connect': false,
    'cores': {
      'priority': ['xray', 'sing-box', 'mihomo'],
      'mode': 'auto',
      'health_url': 'http://cp.cloudflare.com/generate_204',
      'health_interval_s': 15,
      'health_failures': 3,
      'max_latency_ms': 0,
      'return_after_min': 10,
      'latency_test': 'ping',
      'fragment': false,
      'switch_server': true,
    },
    'dns': {'remote': 'https://1.1.1.1/dns-query', 'direct': '', 'fake_ip': true, 'block_browser_doh': false, 'block_dot': false, 'strict': true},
    'routing': {
      'mode': 'all',
      'russia_direct': true,
      'direct_domains': <String>[],
      'direct_apps': ['qbittorrent.exe'],
      'direct_ips': <String>[],
      'proxy_domains': <String>[],
      'proxy_ips': <String>[],
      'proxy_apps': <String>[],
      'app_filter': 'all',
      'filter_apps': <String>[],
      'block_domains': <String>[],
    },
    'updates': {'auto': true, 'interval_hours': 12, 'user_agent': ''},
    'app_update': {'auto': true, 'source': ''},
  };

  Json _appUpdate = {'state': 'idle', 'checked_at': DateTime.now().toUtc().toIso8601String()};

  /// A release appears on checking; it waits while the demo is connected.
  Future<void> _demoAppUpdate() async {
    _appUpdate = {..._appUpdate, 'state': 'checking'};
    _emit({'kind': 'app-update', 'reason': 'checking'});
    await Future.delayed(const Duration(milliseconds: 800));
    _appUpdate = {
      'state': 'ready',
      'version': '0.3.0',
      'build': 9,
      'checked_at': DateTime.now().toUtc().toIso8601String(),
      'waiting': _status['state'] == 'connected',
    };
    _emit({'kind': 'app-update', 'reason': 'ready', 'line': '0.3.0'});
  }

  static const _features = {
    'xray': ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'wireguard', 'ws', 'grpc', 'httpupgrade', 'xhttp', 'reality'],
    'sing-box': ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'anytls', 'wireguard', 'ws', 'grpc', 'http', 'httpupgrade', 'reality'],
    'mihomo': ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'anytls', 'wireguard', 'ws', 'grpc', 'http', 'httpupgrade', 'reality'],
  };

  final List<Json> _subs = [];
  Json? _selection;
  Json _status = {'state': 'idle', 'tun': true};
  Json? _lastNode;
  bool _pending = false;

  /// Traffic per day, oldest first, ending with today (GET /v1/stats).
  final List<List<int>> _history = [];
  int _todayUp = 0, _todayDown = 0;

  List<List<int>> _makeHistory() {
    final r = Random(11);
    final days = <List<int>>[];
    for (var i = 29; i >= 1; i--) {
      final weekend = DateTime.now().subtract(Duration(days: i)).weekday >= 6;
      // A few days the VPN was off, the rest browsing and the odd big download.
      final down = i % 9 == 4 ? 0 : ((weekend ? 3.2e9 : 1.1e9) * (.3 + r.nextDouble() * 1.7)).round();
      days.add([(down * (.06 + r.nextDouble() * .09)).round(), down]);
    }
    _todayUp = 82000000;
    _todayDown = 1240000000;
    return days;
  }

  String _date(int daysAgo) {
    final d = DateTime.now().subtract(Duration(days: daysAgo));
    return '${d.year}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';
  }

  Json _stats(int n) {
    final all = [
      for (var i = 0; i < _history.length; i++) {'date': _date(_history.length - i), 'up': _history[i][0], 'down': _history[i][1]},
      {'date': _date(0), 'up': _todayUp, 'down': _todayDown},
    ];
    return {'days': all.sublist(all.length - n.clamp(1, all.length))};
  }

  DemoBackend() {
    _history.addAll(_makeHistory());
    _seedJournal();
    _subs.add(
      _makeSub(
        'a1b2c3',
        'NorthLink Premium',
        'https://sub.northlink.example/api/v1/client/TOKEN',
        [
          // Named as panels name them: a flag, then the place.
          ['\u{1F1F3}\u{1F1F1} Amsterdam', 'vless', 'tcp +vision', 'reality', 'nl1.northlink.example'],
          ['\u{1F1F3}\u{1F1F1} Rotterdam', 'vless', 'grpc', 'reality', 'nl2.northlink.example'],
          ['\u{1F1E9}\u{1F1EA} Frankfurt', 'vless', 'xhttp/auto', 'reality', 'de1.northlink.example'],
          ['\u{1F1E9}\u{1F1EA} Falkenstein', 'trojan', 'tcp', 'tls', 'de2.northlink.example'],
          ['\u{1F1E9}\u{1F1EA} Nuremberg', 'vmess', 'ws', 'tls', 'de3.northlink.example'],
          ['\u{1F1EB}\u{1F1EE} Helsinki', 'hysteria2', 'quic', 'tls', 'fi1.northlink.example'],
          ['\u{1F1F8}\u{1F1EA} Stockholm', 'tuic', 'quic', 'tls', 'se1.northlink.example'],
          ['\u{1F1FA}\u{1F1F8} New York', 'trojan', 'grpc', 'tls', 'us1.northlink.example'],
          ['\u{1F1FA}\u{1F1F8} Los Angeles', 'vless', 'ws', 'tls', 'us2.northlink.example'],
          ['\u{1F1F9}\u{1F1F7} Istanbul', 'vmess', 'ws', 'tls', 'tr1.northlink.example'],
          ['\u{1F1F0}\u{1F1FF} Almaty', 'shadowsocks', 'tcp', 'none', 'kz1.northlink.example'],
          ['\u{1F1EF}\u{1F1F5} Tokyo', 'anytls', 'tcp', 'tls', 'jp1.northlink.example'],
        ],
        used: 142e9,
        total: 500e9,
        expireDays: 47,
      ),
    );
    _subs.add(
      _makeSub(
        'd4e5f6',
        '',
        'https://panel.backup.example/sub/TOKEN',
        [
          ['Warsaw', 'vless', 'grpc', 'reality', '203.0.113.21'],
          ['Riga', 'wireguard', 'udp', 'none', '203.0.113.22'],
        ],
        title: 'Резерв',
        used: 18e9,
        total: 100e9,
        expireDays: 7,
      ),
    );
    final first = (_subs[0]['nodes'] as List).first as Json;
    _selection = {'subscription': 'a1b2c3', 'fingerprint': first['fingerprint'], 'name': first['name']};
  }

  @override
  String get description => 'демо-режим';

  List<String> _chain(String protocol, String transport) {
    final net = transport.split(RegExp(r'[ /]')).first;
    final prio = _settings['cores']['mode'] == 'manual'
        ? [_settings['cores']['manual'] as String? ?? 'xray']
        : List<String>.from(_settings['cores']['priority'] as List);
    return prio.where((k) {
      final f = _features[k]!;
      return f.contains(protocol) && (const ['tcp', 'quic', 'udp'].contains(net) || f.contains(net));
    }).toList();
  }

  Json _node(List<String> n) => {
    'fingerprint': base64Url.encode(utf8.encode(n[0] + n[1])).replaceAll('=', ''),
    'name': n[0],
    'protocol': n[1],
    'transport': n[2],
    'security': n[3],
    'server': n[4],
    'port': 443,
    'cores': _chain(n[1], n[2]),
  };

  Json _makeSub(String id, String name, String url, List<List<String>> nodes, {String title = '', double used = 0, double total = 0, int expireDays = 0}) {
    final now = DateTime.now();
    return {
      'id': id,
      'name': name,
      'display_name': name.isNotEmpty ? name : (title.isNotEmpty ? title : 'Subscription'),
      'url': url,
      if (url.startsWith('http://')) 'insecure': true,
      'info': {
        'title': title,
        'upload': (used * .1).round(),
        'download': (used * .9).round(),
        'total': total.round(),
        if (expireDays > 0) 'expire': now.add(Duration(days: expireDays)).toUtc().toIso8601String(),
        'update_interval_hours': 12,
        // Both kinds of support chats, to show their buttons.
        if (url.isNotEmpty) 'support_url': url.contains('backup') ? 'https://vk.me/example_support' : 'https://t.me/example_support',
      },
      'format': 'base64',
      'nodes': nodes.map(_node).toList(),
      'added_at': now.subtract(const Duration(days: 20)).toUtc().toIso8601String(),
      'updated_at': now.subtract(const Duration(minutes: 12)).toUtc().toIso8601String(),
      'checked_at': now.subtract(const Duration(minutes: 12)).toUtc().toIso8601String(),
      'next_update': now.add(const Duration(hours: 11, minutes: 48)).toUtc().toIso8601String(),
    };
  }

  /// An earlier session for the journal to show: a connection, a core that
  /// crashed and the switch to the next, an error, and the end. Replayed,
  /// so it fills the journal without toasts.
  void _seedJournal() {
    final start = DateTime.now().subtract(const Duration(hours: 3));
    var t = start;
    void at(int seconds, Json j) {
      t = t.add(Duration(seconds: seconds));
      _recent.add(Event.fromJson({'time': t.toUtc().toIso8601String(), ...j}));
    }

    at(0, {'kind': 'state', 'state': 'connecting'});
    at(1, {'kind': 'core-state', 'core': 'xray', 'reason': 'starting'});
    at(0, {'kind': 'log', 'source': 'xray', 'line': 'Xray 26.3.27 started'});
    at(1, {'kind': 'tun', 'reason': 'up'});
    at(0, {'kind': 'dns', 'reason': 'applied'});
    at(0, {'kind': 'core-state', 'core': 'xray', 'reason': 'connected'});
    at(0, {'kind': 'state', 'state': 'connected', 'core': 'xray'});
    at(1500, {'kind': 'health', 'core': 'xray', 'error': 'context deadline exceeded'});
    at(15, {'kind': 'health', 'core': 'xray', 'error': 'context deadline exceeded'});
    at(2, {'kind': 'core-failed', 'core': 'xray', 'reason': 'health-check', 'error': 'проверка связи не прошла 3 раза подряд'});
    at(1, {'kind': 'swap', 'core': 'sing-box', 'from': 'xray', 'reason': 'health-check'});
    at(0, {'kind': 'core-state', 'core': 'sing-box', 'reason': 'connected'});
    at(0, {'kind': 'health', 'core': 'sing-box', 'latency_ms': 212});
    at(600, {'kind': 'swap', 'core': 'xray', 'from': 'sing-box', 'reason': 'return-to-primary'});
    at(1, {'kind': 'health', 'core': 'xray', 'latency_ms': 168});
    at(1800, {'kind': 'error', 'error': 'subscription panel.backup.example: server returned 502 Bad Gateway'});
    at(900, {'kind': 'state', 'state': 'disconnecting'});
    at(1, {'kind': 'dns', 'reason': 'reverted'});
    at(0, {'kind': 'tun', 'reason': 'down'});
    at(0, {'kind': 'state', 'state': 'idle'});
  }

  void _emit(Json j) {
    final e = Event.fromJson({'time': DateTime.now().toUtc().toIso8601String(), ...j});
    _recent.add(e);
    if (_recent.length > 300) _recent.removeAt(0);
    _events.add(e);
  }

  Json _statusJson() => {..._status, if (_pending && _status['state'] == 'connected') 'settings_pending': true};

  Json? _findNode(String sub, String fp) {
    for (final s in _subs) {
      if (s['id'] != sub) continue;
      for (final n in s['nodes'] as List) {
        if (n['fingerprint'] == fp) return n as Json;
      }
    }
    return null;
  }

  Json _selectionJson() {
    final sel = _selection;
    if (sel == null) return {'subscription': '', 'fingerprint': '', 'name': '', 'available': false};
    final n = _findNode(sel['subscription'], sel['fingerprint']);
    return {...sel, 'available': n != null, 'node': ?n};
  }

  Future<void> _connect(Json node) async {
    _stop(silent: true);
    _lastNode = node;
    _pending = false;
    final chain = _chain(node['protocol'], node['transport']);
    if (chain.isEmpty) throw const ApiError(502, 'no installed core supports this node');
    final tun = _settings['tun'] == true;
    _status = {'state': 'connecting', 'node': node['name'], 'protocol': node['protocol'], 'chain': chain, 'tun': tun};
    _emit({'kind': 'state', 'state': 'connecting'});
    await Future.delayed(const Duration(milliseconds: 900));
    _emit({'kind': 'core-state', 'core': chain.first, 'reason': 'starting'});
    await Future.delayed(const Duration(milliseconds: 500));
    _emit({'kind': 'health', 'core': chain.first, 'latency_ms': 180 + _rand.nextInt(80)});
    if (tun) {
      _emit({'kind': 'tun', 'reason': 'up'});
      _emit({'kind': 'dns', 'reason': 'applied'});
    }
    _status = {..._status, 'state': 'connected', 'core': chain.first, 'failed': <String, String>{}, 'since': DateTime.now().toUtc().toIso8601String()};
    _emit({'kind': 'core-state', 'core': chain.first, 'reason': 'connected'});
    _emit({'kind': 'state', 'state': 'connected', 'core': chain.first});
    _startTimers(chain);
  }

  /// Health checks, traffic and the pretend crash of a connected demo.
  void _startTimers(List<String> chain) {
    _health = Timer.periodic(const Duration(seconds: 3), (_) {
      _emit({'kind': 'health', 'core': _status['core'], 'latency_ms': 140 + _rand.nextInt(120)});
    });
    _up = _down = 0;
    var tick = 0;
    _traffic = Timer.periodic(const Duration(seconds: 1), (_) {
      tick++;
      // Browsing: a steady trickle with the odd download burst.
      final down = (tick % 17 < 4 ? 3500000 : 120000) + _rand.nextInt(250000);
      final up = down ~/ 12 + _rand.nextInt(20000);
      _up += up;
      _down += down;
      _todayUp += up;
      _todayDown += down;
      _events.add(
        Event.fromJson({'time': DateTime.now().toUtc().toIso8601String(), 'kind': 'traffic', 'up': _up, 'down': _down, 'up_rate': up, 'down_rate': down}),
      );
    });
    // Show off auto-swap: the first core "crashes" after a while.
    if (chain.length > 1) {
      _crash = Timer(const Duration(seconds: 25), () {
        final from = _status['core'] as String;
        final to = chain[1];
        _emit({'kind': 'core-failed', 'core': from, 'reason': 'exited', 'error': 'exit status 1: simulated crash'});
        _status = {
          ..._status,
          'core': to,
          'failed': {from: 'exited: exit status 1: simulated crash'},
        };
        _emit({'kind': 'swap', 'core': to, 'from': from, 'reason': 'exited'});
        _emit({'kind': 'core-state', 'core': to, 'reason': 'connected'});
      });
    }
  }

  void _stop({bool silent = false}) {
    _health?.cancel();
    _crash?.cancel();
    _traffic?.cancel();
    if (_status['state'] == 'idle') return;
    if (!silent) _emit({'kind': 'state', 'state': 'disconnecting'});
    if (_status['tun'] == true) {
      _emit({'kind': 'dns', 'reason': 'reverted'});
      _emit({'kind': 'tun', 'reason': 'down'});
    }
    _status = {'state': 'idle', 'tun': _settings['tun']};
    if (!silent) _emit({'kind': 'state', 'state': 'idle'});
  }

  /// A speed test that ramps up as a real one does, a little slower
  /// through the "VPN", against the speedtest.net server nearest to the
  /// address sites see.
  Future<Json> _speedTest() async {
    final vpn = _status['state'] == 'connected';
    final scale = vpn ? .7 : 1.0;
    final (city, sponsor, country) = vpn ? ('Amsterdam', 'Leaseweb', 'Netherlands') : ('Moscow', 'Rostelecom', 'Russia');
    _emit({'kind': 'speedtest', 'reason': 'latency', 'latency_ms': vpn ? 48 : 12});
    var down = 0, up = 0;
    for (var i = 1; i <= 8; i++) {
      await Future.delayed(const Duration(milliseconds: 250));
      down = (11.5e6 * scale * (1 - 1 / (i + 1))).round();
      _emit({'kind': 'speedtest', 'reason': 'download', 'down_rate': down});
    }
    for (var i = 1; i <= 6; i++) {
      await Future.delayed(const Duration(milliseconds: 250));
      up = (5.2e6 * scale * (1 - 1 / (i + 1))).round();
      _emit({'kind': 'speedtest', 'reason': 'upload', 'up_rate': up});
    }
    final res = {
      'download_bps': down,
      'upload_bps': up,
      'latency_ms': vpn ? 48 : 12,
      'vpn': vpn,
      if (vpn) 'server': _status['node'],
      'test_server': sponsor,
      'test_city': city,
      'test_country': country,
    };
    _emit({'kind': 'speedtest', 'reason': 'done', 'down_rate': down, 'up_rate': up});
    return res;
  }

  Json _subBy(String id) => _subs.firstWhere((s) => s['id'] == id, orElse: () => throw const ApiError(404, 'no such subscription'));

  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    await Future.delayed(const Duration(milliseconds: 120));
    final b = (body as Map?)?.cast<String, dynamic>() ?? {};
    final seg = path.split('?').first.split('/').where((s) => s.isNotEmpty).toList(); // v1, …
    final route = '$method /${seg.skip(1).map((s) => s.length == 6 && seg[1] == 'subscriptions' && s != 'refresh' ? '{id}' : s).join('/')}';
    String id() => seg[2];

    switch (route) {
      case 'GET /status':
        return _statusJson();
      case 'GET /info':
        return {
          'version': appVersion == 'dev' ? '0.2.0' : appVersion,
          'build': appBuild,
          'commit': appCommit,
          'cores': [
            for (final k in ['xray', 'sing-box', 'mihomo'])
              {
                'kind': k,
                'installed': true,
                'version': _versions[k],
                'features': [for (final f in _features[k]!) _featureName(f)],
              },
          ],
          'tun_available': true,
          'store': true,
        };
      case 'POST /connect':
        if (b['subscription'] != null) {
          _selection = {'subscription': b['subscription'], 'fingerprint': b['fingerprint'], 'name': b['name'] ?? ''};
          _emit({'kind': 'store', 'reason': 'selection', 'subscription': b['subscription']});
        }
        final sel = _selectionJson();
        if (sel['available'] != true) throw const ApiError(409, 'no node selected');
        await _connect(sel['node'] as Json);
        return _statusJson();
      case 'GET /ip':
        return _status['state'] == 'connected' ? {'ip': '185.23.41.7', 'country': 'DE', 'vpn': true} : {'ip': '95.31.18.119', 'country': 'RU', 'vpn': false};
      case 'GET /stats':
        return _stats(int.tryParse(Uri.parse(path).queryParameters['days'] ?? '') ?? 30);
      case 'POST /speedtest':
        return _speedTest();
      case 'POST /reconnect':
        if (_lastNode == null) throw const ApiError(502, 'nothing to reconnect');
        await _connect(_lastNode!);
        return _statusJson();
      case 'POST /disconnect':
        _stop();
        return _statusJson();
      case 'GET /settings':
        return jsonDecode(jsonEncode(_settings));
      case 'PUT /settings':
        final dns = (b['dns'] as Map?)?['remote'] as String? ?? '';
        if (!RegExp(r'^(https?|tls|udp|tcp|quic|h3)://|^\d').hasMatch(dns)) {
          throw ApiError(400, 'dns.remote: DNS server "$dns": unsupported scheme');
        }
        final routing = (b['routing'] as Map?) ?? const {};
        final problems = <String>[];
        List<String> names(String key) {
          final out = <String>[];
          for (final d in routing[key] as List? ?? const []) {
            final v = '$d'.trim().toLowerCase().replaceAll(RegExp(r'^\.+|\.+$'), '');
            if (v.isEmpty) continue;
            if (!RegExp(r'^[a-z0-9_-]+(\.[a-z0-9_-]+)*$').hasMatch(v)) {
              problems.add('routing.$key: "$d" is not a domain');
            } else if (!out.contains(v)) {
              out.add(v);
            }
          }
          return out;
        }

        List<String> addrs(String key) {
          final out = <String>[];
          for (final d in routing[key] as List? ?? const []) {
            final v = '$d'.trim();
            if (v.isEmpty) continue;
            if (!RegExp(r'^[0-9a-fA-F:.]+(/\d{1,3})?$').hasMatch(v) || !RegExp(r'[.:]').hasMatch(v)) {
              problems.add('routing.$key: "$d" is not an address or subnet');
            } else if (v.startsWith('198.18.')) {
              problems.add('routing.$key: "$d" overlaps the tunnel\'s own addresses 198.18.0.0/15');
            } else if (!out.contains(v)) {
              out.add(v);
            }
          }
          return out;
        }

        List<String> apps(String key) {
          final out = <String>[];
          for (final a in routing[key] as List? ?? const []) {
            var name = '$a'.trim().split(RegExp(r'[\\/]')).last.trim();
            if (name.isEmpty) continue;
            if (!name.contains('.')) name += '.exe';
            if (!out.any((x) => x.toLowerCase() == name.toLowerCase())) out.add(name);
          }
          return out;
        }

        final clean = {
          'direct_domains': names('direct_domains'),
          'proxy_domains': names('proxy_domains'),
          'block_domains': names('block_domains'),
          'direct_ips': addrs('direct_ips'),
          'proxy_ips': addrs('proxy_ips'),
          'direct_apps': apps('direct_apps'),
          'proxy_apps': apps('proxy_apps'),
        };
        if (problems.isNotEmpty) throw ApiError(400, problems.join('\n'));
        _settings = jsonDecode(jsonEncode(b)) as Json;
        _settings['routing'].addAll(clean);
        if (_status['state'] == 'connected') _pending = true;
        _emit({'kind': 'store', 'reason': 'settings'});
        return jsonDecode(jsonEncode(_settings));
      case 'GET /subscriptions':
        return _subs;
      case 'POST /subscriptions':
        await Future.delayed(const Duration(milliseconds: 700));
        final url = (b['url'] as String? ?? '').trim();
        final content = b['content'] as String? ?? '';
        if (url.isNotEmpty && !url.startsWith('http')) throw const ApiError(400, 'a subscription URL starts with https:// or http://');
        if (url.contains('fail')) throw const ApiError(502, 'fetch subscription: server returned 404 Not Found');
        final nodes = url.isNotEmpty
            ? [
                ['Paris', 'vless', 'ws', 'tls', 'fr1.example'],
                ['Madrid', 'trojan', 'tcp', 'tls', 'es1.example'],
              ]
            : [
                for (final l in content.split('\n').where((l) => l.contains('://')))
                  [Uri.decodeComponent(l.split('#').last), l.split('://').first == 'hy2' ? 'hysteria2' : l.split('://').first, 'tcp', 'tls', 'local.example'],
              ];
        if (nodes.isEmpty) throw const ApiError(400, 'no nodes found');
        // Like the daemon: servers pasted without a name join the list
        // pasted before.
        final into = url.isEmpty && (b['name'] as String? ?? '').isEmpty ? _subs.where((s) => s['url'] == '' && s['name'] == '').firstOrNull : null;
        if (into != null) {
          final have = {for (final n in into['nodes'] as List) n['name']};
          final fresh = nodes.where((n) => !have.contains(n[0])).map(_node).toList();
          if (fresh.isEmpty) throw const ApiError(409, 'these servers are already added');
          into['nodes'] = [...into['nodes'] as List, ...fresh];
          _emit({'kind': 'store', 'reason': 'subscription-updated', 'subscription': into['id']});
          return into;
        }
        final sub = _makeSub(
          _rand.nextInt(0xffffff).toRadixString(16).padLeft(6, '0'),
          b['name'] as String? ?? '',
          url,
          nodes,
          title: url.isNotEmpty ? 'New panel' : '',
        );
        if (url.isEmpty) {
          sub['display_name'] = (b['name'] as String?)?.isNotEmpty == true ? b['name'] : 'Local nodes';
          sub.remove('next_update');
        }
        _subs.add(sub);
        _emit({'kind': 'store', 'reason': 'subscription-added', 'subscription': sub['id']});
        return sub;
      case 'GET /subscriptions/{id}':
        return _subBy(id());
      case 'GET /subscriptions/{id}/url':
        return {'url': _subBy(id())['url']};
      case 'PATCH /subscriptions/{id}':
        final s = _subBy(id());
        if (b['name'] != null) {
          s['name'] = b['name'];
          s['display_name'] = (b['name'] as String).isNotEmpty ? b['name'] : (s['info']['title'] as String? ?? 'Subscription');
        }
        _emit({'kind': 'store', 'reason': 'subscription-updated', 'subscription': id()});
        return s;
      case 'DELETE /subscriptions/{id}':
        _subs.remove(_subBy(id()));
        if (_selection?['subscription'] == id()) _selection = null;
        _emit({'kind': 'store', 'reason': 'subscription-removed', 'subscription': id()});
        return null;
      case 'POST /subscriptions/{id}/refresh':
        final s = _subBy(id());
        await Future.delayed(const Duration(milliseconds: 900));
        final now = DateTime.now().toUtc();
        s['updated_at'] = now.toIso8601String();
        s['checked_at'] = now.toIso8601String();
        s['next_update'] = now.add(const Duration(hours: 12)).toIso8601String();
        _emit({'kind': 'store', 'reason': 'subscription-updated', 'subscription': id()});
        return s;
      case 'POST /latency':
        final subId = b['subscription'] as String?;
        _emit({'kind': 'latency', 'reason': 'started'});
        for (final s in _subs.where((s) => subId == null || s['id'] == subId)) {
          for (final n in (s['nodes'] as List).cast<Json>()) {
            await Future.delayed(Duration(milliseconds: 60 + _rand.nextInt(120)));
            final bad = _rand.nextInt(8) == 0 || (n['cores'] as List).isEmpty;
            final ms = 60 + _rand.nextInt(400);
            n['latency_ms'] = bad ? null : ms;
            n['latency_error'] = bad ? 'context deadline exceeded' : null;
            final core = (n['cores'] as List).isEmpty ? '' : (n['cores'] as List).first;
            n['latency_core'] = core;
            n['latency_method'] = 'icmp';
            _emit({
              'kind': 'latency',
              'subscription': s['id'],
              'fingerprint': n['fingerprint'],
              'core': core,
              'method': 'icmp',
              if (bad) 'error': 'context deadline exceeded' else 'latency_ms': ms,
            });
          }
        }
        _emit({'kind': 'latency', 'reason': 'finished'});
        return [];
      case 'GET /apps':
        return [
          for (final (n, p) in const [
            ('chrome.exe', r'C:\Program Files\Google\Chrome\Application\chrome.exe'),
            ('Discord.exe', r'C:\Users\user\AppData\Local\Discord\app-1.0.9200\Discord.exe'),
            ('qbittorrent.exe', r'C:\Program Files\qBittorrent\qbittorrent.exe'),
            ('steam.exe', r'C:\Program Files (x86)\Steam\steam.exe'),
            ('Telegram.exe', r'C:\Users\user\AppData\Roaming\Telegram Desktop\Telegram.exe'),
          ])
            {'name': n, 'path': p},
        ];
      case 'POST /cores/return':
        final chain = (_status['chain'] as List?)?.cast<String>() ?? const [];
        if (_status['state'] != 'connected') throw const ApiError(409, 'not connected');
        if (chain.isEmpty || _status['core'] == chain.first) throw const ApiError(409, 'already on the primary core');
        await Future.delayed(const Duration(milliseconds: 600));
        final from = _status['core'] as String;
        _status = {..._status, 'core': chain.first, 'failed': <String, String>{}};
        _emit({'kind': 'swap', 'core': chain.first, 'from': from, 'reason': 'return-to-primary'});
        _emit({'kind': 'core-state', 'core': chain.first, 'reason': 'connected'});
        return _statusJson();
      case 'GET /app-update':
        return _appUpdate;
      case 'POST /app-update/check':
        _demoAppUpdate();
        return {..._appUpdate, 'state': 'checking'};
      case 'POST /app-update/install':
        if (_appUpdate['state'] != 'ready') throw const ApiError(409, 'no update is ready to install');
        _appUpdate = {..._appUpdate, 'state': 'installing', 'waiting': false};
        _emit({'kind': 'app-update', 'reason': 'installing', 'line': '0.3.0'});
        return _appUpdate;
      case 'GET /cores/updates':
        await Future.delayed(const Duration(milliseconds: 700));
        return [
          for (final k in ['xray', 'sing-box', 'mihomo'])
            {
              'kind': k,
              'current': _versions[k],
              'latest': k == 'sing-box' ? '1.14.3' : _versions[k],
              'available': k == 'sing-box' && _versions[k] != '1.14.3',
              'size': 32857903,
            },
        ];
      case 'GET /selection':
        return _selectionJson();
      case 'PUT /selection':
        final n = _findNode(b['subscription'] ?? '', b['fingerprint'] ?? '');
        if (n == null) throw const ApiError(400, 'subscription has no such node');
        _selection = {'subscription': b['subscription'], 'fingerprint': b['fingerprint'], 'name': n['name']};
        _emit({'kind': 'store', 'reason': 'selection', 'subscription': b['subscription']});
        return _selectionJson();
    }
    if (method == 'POST' && seg.length == 4 && seg[1] == 'cores' && seg[3] == 'update') {
      await Future.delayed(const Duration(milliseconds: 1500));
      final k = seg[2];
      if (k == 'sing-box') _versions[k] = '1.14.3';
      _emit({'kind': 'cores', 'core': k, 'reason': 'updated', 'line': _versions[k]});
      if (_status['state'] == 'connected') _pending = true;
      return {'kind': k, 'version': _versions[k]};
    }
    throw ApiError(404, 'demo: $route');
  }

  /// What bash.ws reports when DNS goes through the tunnel as it should.
  static Future<List<Json>> leakSample() async {
    await Future.delayed(const Duration(milliseconds: 1200));
    return [
      {'ip': '203.0.113.40', 'country': 'nl', 'country_name': 'Netherlands', 'asn': 'AS64500 Example Hosting', 'type': 'ip'},
      {'ip': '172.253.10.1', 'country': 'nl', 'country_name': 'Netherlands', 'asn': 'AS15169 Google LLC', 'type': 'dns'},
      {'ip': '162.158.1.1', 'country': 'nl', 'country_name': 'Netherlands', 'asn': 'AS13335 CloudFlare Inc', 'type': 'dns'},
      {'type': 'conclusion'},
    ];
  }

  static String _featureName(String f) => switch (f) {
    'reality' => 'reality',
    'ws' || 'grpc' || 'http' || 'httpupgrade' || 'xhttp' => 'transport:$f',
    _ => 'protocol:$f',
  };

  @override
  Stream<Event> events() async* {
    for (final e in List.of(_recent)) {
      yield e;
    }
    yield* _events.stream;
  }
}
