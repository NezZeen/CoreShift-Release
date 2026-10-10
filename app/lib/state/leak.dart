// DNS leak test through bash.ws, run by the engine (POST /v1/leaktest, see
// engine/internal/service/leaktest.go): it looks names up the way apps
// inside the VPN do, through the tunnel, and reports which DNS servers
// asked for them and from which address bash.ws saw the test. Not run by
// the app itself: on Android the app is outside its own VPN, so its
// requests would go around the tunnel and test the ISP instead.
//
// The verdict is the engine's (engine/internal/service/leakjudge.go): a
// leak is DNS that does not go through the tunnel, the ISP's or the
// network's. DNS servers in another country than the VPN server's are none
// by themselves: a DNS the user chose for the tunnel (AdGuard, Cloudflare)
// has servers all over the world.

import '../api/models.dart';

class LeakServer {
  final String ip;
  final String country; // two letters, lower case
  final String countryName;
  final String org;

  /// Of a DNS server: "system" when it answered apps' lookups, "proxy"
  /// when the VPN server asked it for names of connections.
  final String path;

  /// Whose server it is, as the engine judged: "own" for the DNS the user
  /// set for the tunnel, "vpn" for the VPN's side, "isp" for the ISP's or
  /// the network's; empty when nothing is known.
  final String role;

  const LeakServer({this.ip = '', this.country = '', this.countryName = '', this.org = '', this.path = '', this.role = ''});

  factory LeakServer.fromJson(Json j) => LeakServer(
    ip: j['ip'] ?? '',
    country: (j['country'] as String? ?? '').toLowerCase(),
    countryName: j['country_name'] ?? '',
    org: (j['org'] as String?)?.isNotEmpty == true ? j['org'] : (j['asn'] is String ? j['asn'] : ''),
    path: j['path'] ?? '',
    role: j['role'] ?? '',
  );

  String get place => [countryName, org].where((s) => s.isNotEmpty).join(', ');
}

enum LeakVerdict {
  /// The test went through the VPN and no DNS server is the ISP's.
  ok,

  /// Some lookups reached a DNS server of the ISP or the network, not the
  /// tunnel's.
  leak,

  /// bash.ws saw the device's own address: the test itself went around
  /// the VPN, so it says nothing about leaks.
  bypass,

  /// Whether the test went through the VPN cannot be told.
  unknown,
}

class LeakReport {
  final LeakVerdict verdict;

  /// Where sites see the device coming from: the VPN server, if it works.
  final LeakServer? exit;

  /// The DNS servers that resolved the test names.
  final List<LeakServer> dns;

  /// The device's address outside the VPN, when the engine could learn it.
  final IpInfo? home;

  /// The server connected and its address.
  final String server;
  final String serverIp;

  /// The public DNS the user set for the tunnel ("AdGuard"), when known.
  final String provider;

  /// Apps' lookups were tested: the tunnel's DNS answered them.
  final bool systemChecked;

  /// The tunnel answered apps' lookups itself, with fake addresses, so no
  /// DNS server saw them.
  final bool fakeIp;
  final DateTime time;

  const LeakReport({
    this.verdict = LeakVerdict.unknown,
    this.exit,
    this.dns = const [],
    this.home,
    this.server = '',
    this.serverIp = '',
    this.provider = '',
    this.systemChecked = false,
    this.fakeIp = false,
    required this.time,
  });

  /// Reads the engine's LeakResult.
  factory LeakReport.fromJson(Json j, {DateTime? time}) {
    final system = (j['system'] as Map?)?.cast<String, dynamic>() ?? const {};
    final home = (j['home'] as Map?)?.cast<String, dynamic>();
    return LeakReport(
      verdict: switch (j['verdict']) {
        'ok' => LeakVerdict.ok,
        'leak' => LeakVerdict.leak,
        'bypass' => LeakVerdict.bypass,
        _ => LeakVerdict.unknown,
      },
      exit: j['exit'] is Map ? LeakServer.fromJson((j['exit'] as Map).cast()) : null,
      dns: [for (final d in (j['dns'] as List? ?? const [])) LeakServer.fromJson((d as Map).cast())],
      home: home == null ? null : IpInfo.fromJson(home),
      server: j['server'] ?? '',
      serverIp: j['server_ip'] ?? '',
      provider: j['provider'] ?? '',
      systemChecked: system['checked'] == true,
      fakeIp: system['fake_ip'] == true,
      time: time ?? DateTime.now(),
    );
  }

  /// bash.ws saw the device's own address.
  bool get bypassed => verdict == LeakVerdict.bypass;

  /// DNS servers on the ISP's or the network's side rather than the tunnel's.
  List<LeakServer> get suspicious => [
    for (final d in dns)
      if (d.role == 'isp') d,
  ];

  /// The servers of the DNS the user set for the tunnel that answered.
  List<LeakServer> get ownDns => [
    for (final d in dns)
      if (d.role == 'own') d,
  ];

  bool get leaks => verdict == LeakVerdict.leak;
}
