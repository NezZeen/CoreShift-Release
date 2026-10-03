// DNS leak test through bash.ws, run by the engine (POST /v1/leaktest, see
// engine/internal/service/leaktest.go): it looks names up the way apps
// inside the VPN do, through the tunnel, and reports which DNS servers
// asked for them and from which address bash.ws saw the test. Not run by
// the app itself: on Android the app is outside its own VPN, so its
// requests would go around the tunnel and test the ISP instead.

import '../api/models.dart';

class LeakServer {
  final String ip;
  final String country; // two letters, lower case
  final String countryName;
  final String org;

  /// Of a DNS server: "system" when it answered apps' lookups, "proxy"
  /// when the VPN server asked it for names of connections.
  final String path;

  const LeakServer({this.ip = '', this.country = '', this.countryName = '', this.org = '', this.path = ''});

  factory LeakServer.fromJson(Json j) => LeakServer(
    ip: j['ip'] ?? '',
    country: (j['country'] as String? ?? '').toLowerCase(),
    countryName: j['country_name'] ?? '',
    org: (j['org'] as String?)?.isNotEmpty == true ? j['org'] : (j['asn'] ?? ''),
    path: j['path'] ?? '',
  );

  String get place => [countryName, org].where((s) => s.isNotEmpty).join(', ');
}

/// Public resolvers that answer from wherever they are nearest: seeing them
/// means the queries went out through the tunnel, not to the ISP.
const _publicResolvers = ['google', 'cloudflare', 'quad9', 'opendns', 'cisco', 'adguard', 'nextdns', 'cleanbrowsing', 'control d', 'mullvad'];

enum LeakVerdict {
  /// The test went through the VPN and no DNS server is the ISP's.
  ok,

  /// Some lookups reached a DNS server that is not on the VPN's side.
  leak,

  /// bash.ws saw the device's own address: the test itself went around
  /// the VPN, so it says nothing about leaks.
  bypass,

  /// Whether the test went through the VPN cannot be told.
  unknown,
}

class LeakReport {
  /// Where sites see the device coming from: the VPN server, if it works.
  final LeakServer? exit;

  /// The DNS servers that resolved the test names.
  final List<LeakServer> dns;

  /// The device's address outside the VPN, when the engine could learn it.
  final IpInfo? home;

  /// The server connected and its address.
  final String server;
  final String serverIp;

  /// Apps' lookups were tested: the tunnel's DNS answered them.
  final bool systemChecked;

  /// The tunnel answered apps' lookups itself, with fake addresses, so no
  /// DNS server saw them.
  final bool fakeIp;
  final DateTime time;

  const LeakReport({
    this.exit,
    this.dns = const [],
    this.home,
    this.server = '',
    this.serverIp = '',
    this.systemChecked = false,
    this.fakeIp = false,
    required this.time,
  });

  /// Reads the engine's LeakResult.
  factory LeakReport.fromJson(Json j, {DateTime? time}) {
    final system = (j['system'] as Map?)?.cast<String, dynamic>() ?? const {};
    final home = (j['home'] as Map?)?.cast<String, dynamic>();
    return LeakReport(
      exit: j['exit'] is Map ? LeakServer.fromJson((j['exit'] as Map).cast()) : null,
      dns: [for (final d in (j['dns'] as List? ?? const [])) LeakServer.fromJson((d as Map).cast())],
      home: home == null ? null : IpInfo.fromJson(home),
      server: j['server'] ?? '',
      serverIp: j['server_ip'] ?? '',
      systemChecked: system['checked'] == true,
      fakeIp: system['fake_ip'] == true,
      time: time ?? DateTime.now(),
    );
  }

  String get _homeCountry => home?.country.toLowerCase() ?? '';

  /// bash.ws saw the device's own address, or one next to it: providers
  /// often hand out neighbouring addresses from one pool.
  bool get bypassed {
    final e = exit, h = home;
    if (e == null || h == null || h.ip.isEmpty) return false;
    if (e.ip == h.ip) return true;
    final a = e.ip.split('.'), b = h.ip.split('.');
    return a.length == 4 && b.length == 4 && a.take(3).join('.') == b.take(3).join('.');
  }

  /// bash.ws saw the VPN: the server's own address, or at least not the
  /// device's.
  bool get exitIsVpn {
    final e = exit;
    if (e == null || bypassed) return false;
    return e.ip == serverIp || home != null;
  }

  /// DNS servers on the ISP's side rather than the VPN's: those in the
  /// device's own country when the VPN server is elsewhere, and, of the
  /// ones that answered apps, those outside the VPN server's country.
  /// Public resolvers answer from near wherever the query left, and those
  /// the VPN server itself asked are on its side.
  List<LeakServer> get suspicious => [
    for (final d in dns)
      if (_suspicious(d)) d,
  ];

  bool _suspicious(LeakServer d) {
    if (d.country.isEmpty || _public(d)) return false;
    final exitCountry = exit?.country ?? '';
    if (_homeCountry.isNotEmpty && d.country == _homeCountry && exitCountry != _homeCountry) return true;
    if (d.path == 'proxy') return false;
    return exitCountry.isNotEmpty && d.country != exitCountry;
  }

  static bool _public(LeakServer d) {
    final org = d.org.toLowerCase();
    return _publicResolvers.any(org.contains);
  }

  /// The DNS servers look like the ISP's own: in the device's country.
  bool get ispResolvers => _homeCountry.isNotEmpty && suspicious.any((d) => d.country == _homeCountry);

  LeakVerdict get verdict {
    if (exit == null) return LeakVerdict.unknown;
    if (bypassed) return LeakVerdict.bypass;
    if (suspicious.isNotEmpty) return LeakVerdict.leak;
    if (!exitIsVpn) return LeakVerdict.unknown;
    return LeakVerdict.ok;
  }

  bool get leaks => verdict == LeakVerdict.leak;
}
