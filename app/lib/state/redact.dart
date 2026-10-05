/// What the journal copied for support hides: the user pastes it into chats.
///
/// The cores and the TUN layer print the sites a failed connection went to
/// ("to www.example.com:443"), the server's address and, now and then, a
/// link or a user ID. The copy keeps what explains a failure (the error,
/// the port, whether an address is local) and drops what identifies the
/// user, the sites they visit and their server's credentials:
///
/// - links lose everything after the scheme: their path and query carry
///   subscription tokens;
/// - UUIDs (VLESS and VMess users) become `<uuid>`;
/// - public IPv4 addresses keep their first two numbers (`203.0.x.x`), and
///   public IPv6 ones their first two groups; local, private and the
///   tunnel's own addresses stay as they are;
/// - names before a port (`www.example.com:443`) and names looked up
///   (`lookup www.example.com`) become `<домен>`.
String redactForSupport(String line) {
  var s = line.replaceAllMapped(_urlRe, (m) => '${m[1]}://…');
  s = s.replaceAll(_uuidRe, '<uuid>');
  s = s.replaceAllMapped(_ipv4Re, (m) => _maskIPv4(m[0]!));
  s = s.replaceAllMapped(_ipv6Re, (m) => _maskIPv6(m[0]!));
  // "conn.go:123" in a panic is a source line, not a site.
  s = s.replaceAllMapped(_hostPortRe, (m) => _sourceExt.contains(m[1]!.split('.').last.toLowerCase()) ? m[0]! : '<домен>:${m[2]}');
  s = s.replaceAllMapped(_lookupRe, (m) => '${m[1]}<домен>');
  return s;
}

final _urlRe = RegExp(
  r'\b([a-zA-Z][a-zA-Z0-9+.-]{1,15})://[^\s"'
  "'"
  r'<>)\]]+',
);
final _uuidRe = RegExp(r'\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b');
final _ipv4Re = RegExp(r'(?<![\d.])(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(?![\d.]*\d)');
// Candidates only: whether one is an address is for Uri.parseIPv6Address.
final _ipv6Re = RegExp(r'(?<![0-9A-Za-z:])[0-9A-Fa-f]{0,4}(?::[0-9A-Fa-f]{0,4}){2,7}(?![0-9A-Za-z:])');
// A name of two or more labels, the last of letters, before ":port".
final _hostPortRe = RegExp(r'(?<![\w.-])((?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z][A-Za-z0-9-]{1,62})\.?:(\d{1,5})\b');
const _sourceExt = {'go', 'json', 'yaml', 'yml', 'txt', 'log', 'exe', 'dll', 'so', 'srs', 'db', 'dart', 'kt', 'java', 'cc', 'cpp', 'rs', 's'};
final _lookupRe = RegExp(r'(\blookup (?:failed for |of )?)((?:[A-Za-z0-9_](?:[A-Za-z0-9_-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z][A-Za-z0-9-]{1,62}\.?)');

String _maskIPv4(String a) {
  final p = a.split('.').map(int.parse).toList();
  if (p.any((n) => n > 255)) return a; // a version number, not an address
  final local =
      p[0] == 0 ||
      p[0] == 10 ||
      p[0] == 127 ||
      (p[0] == 169 && p[1] == 254) ||
      (p[0] == 172 && p[1] >= 16 && p[1] <= 31) ||
      (p[0] == 192 && p[1] == 168) ||
      (p[0] == 100 && p[1] >= 64 && p[1] <= 127) || // carrier-grade NAT
      (p[0] == 198 && (p[1] == 18 || p[1] == 19)) || // the tunnel's fake IPs
      p[0] >= 224;
  return local ? a : '${p[0]}.${p[1]}.x.x';
}

String _maskIPv6(String a) {
  final List<int> b;
  try {
    b = Uri.parseIPv6Address(a);
  } on FormatException {
    return a; // a time of day, a MAC address…
  }
  final loopbackOrNone = b.take(15).every((x) => x == 0);
  final linkLocal = b[0] == 0xfe && (b[1] & 0xc0) == 0x80;
  final uniqueLocal = (b[0] & 0xfe) == 0xfc; // fc00::/7, the tunnel's too
  if (loopbackOrNone || linkLocal || uniqueLocal) return a;
  String group(int i) => ((b[i] << 8) | b[i + 1]).toRadixString(16);
  return '${group(0)}:${group(2)}:…';
}
