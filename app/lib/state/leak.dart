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

/// One geolocation service's answer for the VPN server's address (the
/// engine's LeakGeoSource).
class LeakGeoSource {
  final String name;

  /// It answered with a country; a failed one is only listed in the details.
  final bool ok;
  final String country; // two letters, lower case
  final String city;
  final String org;

  const LeakGeoSource({this.name = '', this.ok = false, this.country = '', this.city = '', this.org = ''});

  factory LeakGeoSource.fromJson(Json j) => LeakGeoSource(
    name: j['name'] ?? '',
    ok: j['ok'] == true,
    country: (j['country'] as String? ?? '').toLowerCase(),
    city: j['city'] ?? '',
    org: j['org'] ?? '',
  );
}

/// The services that put the VPN server in one country.
class LeakGeoGroup {
  final String country;
  final List<String> sources;
  const LeakGeoGroup({this.country = '', this.sources = const []});

  factory LeakGeoGroup.fromJson(Json j) =>
      LeakGeoGroup(country: (j['country'] as String? ?? '').toLowerCase(), sources: [for (final s in (j['sources'] as List? ?? const [])) '$s']);
}

/// Where the geolocation services, bash.ws among them, put the VPN
/// server's address (the engine's LeakGeo): they often disagree, so the
/// country most of them name is taken, and the split is shown.
class LeakGeo {
  /// The country most services agree on, and the city and organisation
  /// those that agree gave.
  final String country;
  final String city;
  final String org;

  /// How many services put the server there, out of how many answered.
  final int votes;
  final int total;

  /// Another country has as many votes.
  final bool tie;

  /// The countries named, the most voted first.
  final List<LeakGeoGroup> groups;

  /// Every service asked, with those that did not answer.
  final List<LeakGeoSource> sources;

  const LeakGeo({
    this.country = '',
    this.city = '',
    this.org = '',
    this.votes = 0,
    this.total = 0,
    this.tie = false,
    this.groups = const [],
    this.sources = const [],
  });

  factory LeakGeo.fromJson(Json j) => LeakGeo(
    country: (j['country'] as String? ?? '').toLowerCase(),
    city: j['city'] ?? '',
    org: j['org'] ?? '',
    votes: (j['votes'] as num?)?.toInt() ?? 0,
    total: (j['total'] as num?)?.toInt() ?? 0,
    tie: j['tie'] == true,
    groups: [for (final g in (j['groups'] as List? ?? const [])) LeakGeoGroup.fromJson((g as Map).cast())],
    sources: [for (final g in (j['sources'] as List? ?? const [])) LeakGeoSource.fromJson((g as Map).cast())],
  );

  /// The services name more than one country.
  bool get split => groups.length > 1;
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

  /// The exit address as the geolocation services see it; null when none
  /// but bash.ws answered.
  final LeakGeo? exitGeo;

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
    this.exitGeo,
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
    final geo = j['exit_geo'] is Map ? LeakGeo.fromJson((j['exit_geo'] as Map).cast()) : null;
    return LeakReport(
      verdict: switch (j['verdict']) {
        'ok' => LeakVerdict.ok,
        'leak' => LeakVerdict.leak,
        'bypass' => LeakVerdict.bypass,
        _ => LeakVerdict.unknown,
      },
      exit: j['exit'] is Map ? LeakServer.fromJson((j['exit'] as Map).cast()) : null,
      dns: [for (final d in (j['dns'] as List? ?? const [])) LeakServer.fromJson((d as Map).cast())],
      exitGeo: geo == null || geo.country.isEmpty ? null : geo,
      home: home == null ? null : IpInfo.fromJson(home),
      server: j['server'] ?? '',
      serverIp: j['server_ip'] ?? '',
      provider: j['provider'] ?? '',
      systemChecked: system['checked'] == true,
      fakeIp: system['fake_ip'] == true,
      time: time ?? DateTime.now(),
    );
  }

  /// The services do not all put the VPN server where bash.ws does: they
  /// split, or the country they agree on is not bash.ws's.
  bool get exitCountriesDiffer {
    final g = exitGeo, e = exit;
    return g != null && e != null && (g.split || e.country != g.country);
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
