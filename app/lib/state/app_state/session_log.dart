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
      if (!on('tun')) _proxyModeName,
      tr(setting('routing.mode', 'all') == 'selected' ? 'app.session.routeSelected' : 'app.session.routeAll'),
      if (on('routing.russia_direct')) tr('app.session.russiaDirect'),
    ].join(', ');
    final dns = [tr('app.session.dns'), if (on('dns.fake_ip')) tr('app.session.fakeIp')].join(', ');
    final parts = [
      if (selection.name.isNotEmpty) tr('app.node.quoted', {'name': selection.name}),
      if (node != null && node.protocol.isNotEmpty) _protocolName(node.protocol),
      if (core.isNotEmpty) [AppState.coreName(core), if (version.isNotEmpty) version].join(' '),
      route,
      dns,
      tr(on('ipv6') ? 'app.session.ipv6On' : 'app.session.ipv6Off'),
    ];
    final shown = took == null || took.isNegative || took > const Duration(minutes: 5) ? null : _seconds(took);
    return shown == null
        ? tr('app.session.connected', {'parts': parts.join(' · ')})
        : tr('app.session.connectedIn', {'took': shown, 'parts': parts.join(' · ')});
  }

  /// «сессия: 2 ч 14 мин · ↓ 1.8 ГБ ↑ 120 МБ · «Amsterdam», sing-box».
  static String sessionText({required Duration lasted, required int down, required int up, String node = '', String core = ''}) {
    final via = [
      if (node.isNotEmpty) tr('app.node.quoted', {'name': node}),
      if (core.isNotEmpty) AppState.coreName(core),
    ].join(', ');
    final args = {'label': tr('app.session.label'), 'lasted': _lasted(lasted), 'down': _bytes(down), 'up': _bytes(up), 'via': via};
    return tr(via.isEmpty ? 'app.session.summary' : 'app.session.summaryVia', args);
  }

  static String _seconds(Duration d) {
    final s = d.inMilliseconds / 1000;
    return tr('app.unit.s', {'n': s < 10 ? s.toStringAsFixed(1) : s.round()});
  }

  static String _lasted(Duration d) {
    if (d.inMinutes < 1) return tr('app.unit.s', {'n': max(0, d.inSeconds)});
    if (d.inHours < 1) return tr('app.unit.min', {'n': d.inMinutes});
    final m = d.inMinutes % 60;
    return m == 0 ? tr('app.unit.h', {'n': d.inHours}) : tr('app.unit.hmin', {'h': d.inHours, 'm': m});
  }

  /// As the home page counts traffic (formatBytes), kept here as the state
  /// does not reach into the UI.
  static String _bytes(num b) {
    if (b >= 1e12) return tr('app.unit.tb', {'n': (b / 1e12).toStringAsFixed(1)});
    if (b >= 1e9) return tr('app.unit.gb', {'n': (b / 1e9).toStringAsFixed(b >= 1e11 ? 0 : 1)});
    if (b >= 1e6) return tr('app.unit.mb', {'n': (b / 1e6).toStringAsFixed(0)});
    return tr('app.unit.kb', {'n': (b / 1e3).toStringAsFixed(0)});
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
