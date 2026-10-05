import 'dart:convert';

/// What a link from outside asks CoreShift to add: a panel's «add to
/// CoreShift» button (coreshift://, the only scheme the system opens
/// CoreShift for), or a scanned QR code or the clipboard, where other
/// clients' links (happ://, v2rayng://…) are read too.
class ImportLink {
  /// A subscription URL (http or https), or empty.
  final String url;

  /// Server links to add as a pasted list (vless://…), or empty.
  final String content;

  /// The name the link suggests; may be empty.
  final String name;

  /// Set when the link is recognised but CoreShift cannot use it, e.g. an
  /// encrypted Happ link: what to tell the user.
  final String error;

  const ImportLink({this.url = '', this.content = '', this.name = '', this.error = ''});

  /// What is added, for comparing with what the user already has.
  String get source => url.isNotEmpty ? url : content;
}

/// Server link schemes that can be pasted as a list.
const _serverSchemes = {'vless', 'vmess', 'trojan', 'ss', 'hy2', 'hysteria2', 'tuic', 'anytls', 'wireguard', 'wg', 'socks', 'socks5'};

/// Apps whose "add subscription" links panels offer, and CoreShift's own.
/// The forms differ: scheme://add/URL, scheme://import/URL#name, or
/// scheme://install-config?url=ENCODED&name=…
const _appSchemes = {
  'coreshift',
  'happ',
  'v2rayng',
  'v2raytun',
  'hiddify',
  'streisand',
  'sing-box',
  'clash',
  'clashmeta',
  'flclash',
  'karing',
  'nekobox',
  'incy',
};

/// Reads [text] as something to add, or returns null when it is none.
///
/// With [strict], used for the clipboard, a plain http(s) URL counts only
/// when it looks like a subscription: the user may have copied any link.
ImportLink? parseImportLink(String text, {bool strict = false}) {
  final t = text.trim();
  if (t.isEmpty || t.length > 64 * 1024) return null;

  // Server links, one per line. A name after "#" may hold spaces.
  final lines = t.split(RegExp(r'[\r\n]+')).map((l) => l.trim()).where((l) => l.isNotEmpty).toList();
  if (lines.every((l) => _serverSchemes.contains(_scheme(l)))) {
    return ImportLink(content: lines.join('\n'));
  }
  if (lines.length != 1) return null;

  final scheme = _scheme(t);
  if (scheme == 'http' || scheme == 'https') {
    if (!_isWebUrl(t)) return null;
    if (strict && !looksLikeSubscription(t)) return null;
    return ImportLink(url: t);
  }
  if (!_appSchemes.contains(scheme)) return null;
  return _appLink(t, scheme);
}

String _scheme(String s) {
  final i = s.indexOf('://');
  return i <= 0 ? '' : s.substring(0, i).toLowerCase();
}

bool _isWebUrl(String s) {
  if (s.contains(RegExp(r'\s'))) return false;
  final u = Uri.tryParse(s);
  return u != null && (u.scheme == 'http' || u.scheme == 'https') && u.host.isNotEmpty;
}

ImportLink? _appLink(String link, String scheme) {
  var rest = link.substring(scheme.length + 3);
  // The action: "add", "import", "install-config", "import-remote-profile"…
  final cut = rest.indexOf(RegExp(r'[/?]'));
  final action = (cut < 0 ? rest : rest.substring(0, cut)).toLowerCase();
  rest = cut < 0 ? '' : rest.substring(cut);

  if (scheme == 'happ' && action.startsWith('crypt')) {
    return const ImportLink(error: 'Это зашифрованная ссылка Happ: её открывает только Happ. Скопируйте из панели обычную ссылку на подписку.');
  }

  var name = '';
  String target;
  if (rest.startsWith('?')) {
    // scheme://install-config?url=…&name=…#name
    var query = rest.substring(1);
    final hash = query.indexOf('#');
    if (hash >= 0) {
      name = _decode(query.substring(hash + 1));
      query = query.substring(0, hash);
    }
    final params = <String, String>{};
    for (final pair in query.split('&')) {
      final eq = pair.indexOf('=');
      if (eq > 0) params[pair.substring(0, eq).toLowerCase()] = _decode(pair.substring(eq + 1));
    }
    target = params['url'] ?? '';
    if (params['name']?.isNotEmpty == true) name = params['name']!;
  } else {
    // scheme://add/URL#name: the URL is everything after the action, its
    // own query included; a fragment names the subscription.
    target = rest.startsWith('/') ? rest.substring(1) : rest;
    if (RegExp(r'^[a-z0-9]+%3A', caseSensitive: false).hasMatch(target)) target = _decode(target);
    // A server link keeps its fragment: that is the server's name.
    final hash = target.lastIndexOf('#');
    if (hash >= 0 && !_serverSchemes.contains(_scheme(target))) {
      name = _decode(target.substring(hash + 1));
      target = target.substring(0, hash);
    }
  }
  target = target.trim();
  if (target.isEmpty) return null;
  final inner = parseImportLink(target);
  if (inner == null || inner.error.isNotEmpty) return inner;
  return ImportLink(url: inner.url, content: inner.content, name: name.trim());
}

/// Undoes percent-encoding ("+" is a space), leaving characters that were
/// never encoded, such as Cyrillic in a name, as they are.
String _decode(String s) {
  final bytes = <int>[];
  final runes = s.runes.toList();
  for (var i = 0; i < runes.length; i++) {
    final c = runes[i];
    final hex = c == 0x25 && i + 2 < runes.length ? int.tryParse(String.fromCharCodes(runes, i + 1, i + 3), radix: 16) : null;
    if (hex != null) {
      bytes.add(hex);
      i += 2;
    } else {
      bytes.addAll(utf8.encode(String.fromCharCode(c == 0x2B ? 0x20 : c)));
    }
  }
  return utf8.decode(bytes, allowMalformed: true);
}

/// Whether an http(s) URL is likely a subscription rather than any page:
/// panels put it under /sub, /subscribe, /api/…, or give it a long token.
bool looksLikeSubscription(String url) {
  final u = Uri.tryParse(url);
  if (u == null || u.host.isEmpty) return false;
  if (u.host.split('.').any((l) => l == 'sub' || l.startsWith('sub-') || l.endsWith('-sub') || l == 'subscribe')) return true;
  final segments = u.pathSegments.where((s) => s.isNotEmpty).toList();
  const marks = {'sub', 'subs', 'subscribe', 'subscription', 'subscriptions', 'link', 's', 'api', 'getsub'};
  if (segments.any((s) => marks.contains(s.toLowerCase()))) return true;
  const keys = {'token', 'sub', 'subscribe', 'key', 'flag', 'target'};
  if (u.queryParameters.keys.any((k) => keys.contains(k.toLowerCase()))) return true;
  // Remnawave and others: https://host/<short id>, a token of 16+ letters
  // and digits with at least one digit.
  if (segments.isNotEmpty) {
    final last = segments.last;
    if (last.length >= 16 && RegExp(r'^[A-Za-z0-9_-]+$').hasMatch(last) && RegExp(r'\d').hasMatch(last)) return true;
  }
  return false;
}

/// A short fingerprint of [s], to remember a clipboard link was offered
/// without keeping the link, which carries an access token: FNV-1a, 32 bit,
/// computed so that it also stays exact in JavaScript (the web demo).
String linkFingerprint(String s) {
  var h = 0x811c9dc5;
  for (final c in s.codeUnits) {
    h ^= c;
    // h × 0x01000193 mod 2^32, split so no product exceeds 2^53.
    h = (h * 0x193 + ((h & 0xFF) << 24)) & 0xFFFFFFFF;
  }
  return '${s.length.toRadixString(16)}-${h.toRadixString(16)}';
}
