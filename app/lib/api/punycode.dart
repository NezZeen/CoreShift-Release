/// Punycode (RFC 3492) for domain names in other scripts: the service keeps
/// «кремль.рф» as "xn--e1ajeds9e.xn--p1ai", the form the cores match, and
/// the app shows it as it was typed.
library;

const _base = 36, _tMin = 1, _tMax = 26, _skew = 38, _damp = 700, _initialBias = 72, _initialN = 128;

int _adapt(int delta, int points, bool first) {
  delta = first ? delta ~/ _damp : delta ~/ 2;
  delta += delta ~/ points;
  var k = 0;
  while (delta > ((_base - _tMin) * _tMax) ~/ 2) {
    delta ~/= _base - _tMin;
    k += _base;
  }
  return k + (_base - _tMin + 1) * delta ~/ (delta + _skew);
}

int _digit(int d) => d < 26 ? 0x61 + d : 0x30 + d - 26;

int? _value(int c) => c >= 0x30 && c <= 0x39
    ? c - 0x30 + 26
    : c >= 0x61 && c <= 0x7a
    ? c - 0x61
    : c >= 0x41 && c <= 0x5a
    ? c - 0x41
    : null;

/// Encodes one label; ASCII labels stay as they are.
String _encodeLabel(String label) {
  final input = label.runes.toList();
  if (input.every((c) => c < 0x80)) return label;
  final out = StringBuffer();
  for (final c in input.where((c) => c < 0x80)) {
    out.writeCharCode(c);
  }
  final basic = out.length;
  var handled = basic;
  if (basic > 0) out.write('-');
  var n = _initialN, delta = 0, bias = _initialBias;
  while (handled < input.length) {
    final m = input.where((c) => c >= n).reduce((a, b) => a < b ? a : b);
    delta += (m - n) * (handled + 1);
    n = m;
    for (final c in input) {
      if (c < n) delta++;
      if (c != n) continue;
      var q = delta;
      for (var k = _base; ; k += _base) {
        final t = k <= bias ? _tMin : (k >= bias + _tMax ? _tMax : k - bias);
        if (q < t) break;
        out.writeCharCode(_digit(t + (q - t) % (_base - t)));
        q = (q - t) ~/ (_base - t);
      }
      out.writeCharCode(_digit(q));
      bias = _adapt(delta, handled + 1, handled == basic);
      delta = 0;
      handled++;
    }
    delta++;
    n++;
  }
  return 'xn--$out';
}

/// Decodes one "xn--" label; null when it is not valid punycode.
String? _decodeLabel(String label) {
  final s = label.substring(4);
  final cut = s.lastIndexOf('-');
  final out = cut > 0 ? s.substring(0, cut).runes.toList() : <int>[];
  var n = _initialN, i = 0, bias = _initialBias;
  var pos = cut > 0 ? cut + 1 : 0;
  while (pos < s.length) {
    final old = i;
    var w = 1;
    for (var k = _base; ; k += _base) {
      if (pos >= s.length) return null;
      final d = _value(s.codeUnitAt(pos++));
      if (d == null) return null;
      i += d * w;
      final t = k <= bias ? _tMin : (k >= bias + _tMax ? _tMax : k - bias);
      if (d < t) break;
      w *= _base - t;
      if (i > 0x10FFFF * 64) return null;
    }
    bias = _adapt(i - old, out.length + 1, old == 0);
    n += i ~/ (out.length + 1);
    i %= out.length + 1;
    if (n > 0x10FFFF) return null;
    out.insert(i++, n);
  }
  return String.fromCharCodes(out);
}

/// "кремль.рф" → "xn--e1ajeds9e.xn--p1ai", lowercased.
String domainToAscii(String domain) => domain.toLowerCase().split('.').map(_encodeLabel).join('.');

/// "xn--e1ajeds9e.xn--p1ai" → "кремль.рф"; labels that are not punycode
/// stay as they are.
String domainToUnicode(String domain) => domain.split('.').map((l) => l.toLowerCase().startsWith('xn--') ? (_decodeLabel(l.toLowerCase()) ?? l) : l).join('.');
