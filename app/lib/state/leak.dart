// DNS leak test through bash.ws: the app looks up a few unique names under a
// test domain, and the service reports which DNS servers asked for them and
// from where the app's traffic came. Run by the app, not the daemon: the
// daemon's own lookups bypass the tunnel's DNS on purpose.

import '../api/models.dart';

class LeakServer {
  final String ip;
  final String country; // two letters, lower case
  final String countryName;
  final String org;

  const LeakServer({this.ip = '', this.country = '', this.countryName = '', this.org = ''});

  factory LeakServer.fromJson(Json j) => LeakServer(
    ip: j['ip'] ?? '',
    country: (j['country'] as String? ?? '').toLowerCase(),
    countryName: j['country_name'] ?? '',
    org: (j['org'] as String?)?.isNotEmpty == true ? j['org'] : (j['asn'] ?? ''),
  );

  String get place => [countryName, org].where((s) => s.isNotEmpty).join(', ');
}

/// Public resolvers that answer from wherever they are nearest: seeing them
/// means the queries went out through the tunnel, not to the ISP.
const _publicResolvers = ['google', 'cloudflare', 'quad9', 'opendns', 'cisco', 'adguard', 'nextdns', 'cleanbrowsing', 'control d', 'mullvad'];

class LeakReport {
  /// Where sites see the computer coming from: the VPN server, if it works.
  final LeakServer? exit;

  /// The DNS servers that resolved the test names.
  final List<LeakServer> dns;
  final DateTime time;

  const LeakReport({this.exit, this.dns = const [], required this.time});

  /// Reads bash.ws's JSON: entries of type "ip", "dns" and "conclusion".
  factory LeakReport.fromEntries(List<Json> entries, {DateTime? time}) {
    LeakServer? exit;
    final dns = <LeakServer>[];
    final seen = <String>{};
    for (final e in entries) {
      final s = LeakServer.fromJson(e);
      if (e['type'] == 'ip') {
        exit ??= s;
      } else if (e['type'] == 'dns' && seen.add(s.ip)) {
        dns.add(s);
      }
    }
    return LeakReport(exit: exit, dns: dns, time: time ?? DateTime.now());
  }

  /// DNS servers that are neither in the VPN server's country nor a public
  /// resolver: most likely the ISP's, which the tunnel should have hidden.
  List<LeakServer> get suspicious => [
    for (final d in dns)
      if (!_trusted(d)) d,
  ];

  bool _trusted(LeakServer d) {
    if (exit != null && d.country.isNotEmpty && d.country == exit!.country) return true;
    final org = d.org.toLowerCase();
    return _publicResolvers.any(org.contains);
  }

  bool get leaks => suspicious.isNotEmpty;
}
