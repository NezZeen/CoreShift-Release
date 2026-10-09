part of '../app_state.dart';

/// What a connection was, in the journal: the line «подключено» says with
/// what (the server, the protocol, the core and its version, how traffic
/// goes), and when it ends a line sums it up (how long, how much).
class _Session {
  /// When the service began connecting, by its event.
  DateTime? connectingAt;

  /// When it connected, and to what; null while not connected.
  DateTime? connectedAt;
  String node = '';
  String core = '';
}

extension AppStateSessionLog on AppState {
  /// Notes the state of an event, and returns the journal's text for it:
  /// «подключено» with the details, the plain state else.
  String _sessionStateText(Event e) {
    final s = _session;
    switch (e.state) {
      case 'connecting':
        // A reconnect from a running connection ends that one first.
        s.connectingAt = e.time;
      case 'connected':
        final took = s.connectingAt == null ? null : e.time.difference(s.connectingAt!);
        s
          ..connectingAt = null
          ..connectedAt = e.time
          ..node = selection.name
          ..core = e.core;
        return connectedText(took: took, core: e.core);
    }
    return AppState._stateText(e.state);
  }

  /// The line that sums up the connection that ended at [t], if one did:
  /// a state other than connected and held for want of a network.
  String? _sessionEnd(Event e) {
    final s = _session;
    if (s.connectedAt == null || e.state == 'connected' || e.state == 'no-network' || e.state == 'noNetwork') return null;
    final lasted = e.time.difference(s.connectedAt!);
    s.connectedAt = null;
    return sessionText(lasted: lasted, down: sessionDown, up: sessionUp, node: s.node, core: s.core);
  }

  /// «подключено за 1.4 с: «Amsterdam» · VLESS · sing-box 1.14.3 · всё через
  /// VPN, Россия напрямую · DNS через VPN, Fake-IP · IPv6»: what the
  /// connection runs with, as the settings are at the moment it connects.
  String connectedText({Duration? took, required String core}) {
    bool on(String path) => setting(path, false) == true;
    final node = selection.node;
    final version = info.versionOf(core);
    final route = [
      if (!on('tun')) 'только прокси',
      setting('routing.mode', 'all') == 'selected' ? 'только выбранное через VPN' : 'всё через VPN',
      if (on('routing.russia_direct')) 'Россия напрямую',
      if (setting('routing.block_ads', false) == true) 'без рекламы',
    ].join(', ');
    final dns = ['DNS через VPN', if (on('dns.fake_ip')) 'Fake-IP'].join(', ');
    final parts = [
      if (selection.name.isNotEmpty) '«${selection.name}»',
      if (node != null && node.protocol.isNotEmpty) _protocolName(node.protocol),
      if (core.isNotEmpty) [AppState.coreName(core), if (version.isNotEmpty) version].join(' '),
      route,
      dns,
      on('ipv6') ? 'IPv6 через VPN' : 'IPv6 выключен',
    ];
    final when = took == null || took.isNegative || took > const Duration(minutes: 5) ? '' : ' за ${_seconds(took)}';
    return 'подключено$when: ${parts.join(' · ')}';
  }

  /// «сессия: 2 ч 14 мин · ↓ 1.8 ГБ ↑ 120 МБ · «Amsterdam», sing-box».
  static String sessionText({required Duration lasted, required int down, required int up, String node = '', String core = ''}) {
    final via = [if (node.isNotEmpty) '«$node»', if (core.isNotEmpty) AppState.coreName(core)].join(', ');
    return 'сессия: ${_lasted(lasted)} · ↓ ${_bytes(down)} ↑ ${_bytes(up)}${via.isEmpty ? '' : ' · $via'}';
  }

  static String _seconds(Duration d) {
    final s = d.inMilliseconds / 1000;
    return '${s < 10 ? s.toStringAsFixed(1) : s.round()} с';
  }

  static String _lasted(Duration d) {
    if (d.inMinutes < 1) return '${max(0, d.inSeconds)} с';
    if (d.inHours < 1) return '${d.inMinutes} мин';
    final m = d.inMinutes % 60;
    return '${d.inHours} ч${m == 0 ? '' : ' $m мин'}';
  }

  /// As the home page counts traffic (formatBytes), kept here as the state
  /// does not reach into the UI.
  static String _bytes(num b) {
    if (b >= 1e12) return '${(b / 1e12).toStringAsFixed(1)} ТБ';
    if (b >= 1e9) return '${(b / 1e9).toStringAsFixed(b >= 1e11 ? 0 : 1)} ГБ';
    if (b >= 1e6) return '${(b / 1e6).toStringAsFixed(0)} МБ';
    return '${(b / 1e3).toStringAsFixed(0)} КБ';
  }

  static String _protocolName(String p) => switch (p) {
    'vless' => 'VLESS',
    'vmess' => 'VMess',
    'trojan' => 'Trojan',
    'shadowsocks' => 'Shadowsocks',
    'hysteria2' => 'Hysteria2',
    'hysteria' => 'Hysteria',
    'tuic' => 'TUIC',
    'anytls' => 'AnyTLS',
    'wireguard' => 'WireGuard',
    _ => p,
  };
}
