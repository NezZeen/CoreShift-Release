import 'geo.dart';

/// Базы категорий в демо-режиме: те же правила сравнения названий, что в службе
/// (engine/internal/ruleset/categories.go), и несколько списков категорий.

const _sagernetSites = [
  'category-ads-all',
  'category-ru',
  'category-ua',
  'category-media-ru-blocked',
  'geolocation-!cn',
  'google',
  'google@cn',
  'netflix',
  'telegram',
  'tiktok',
  'youtube',
];
const _runetSites = ['category-ads-all', 'category-ru', 'category-ua', 'google', 'netflix', 'ru-blocked', 'telegram', 'tiktok', 'ua', 'youtube'];
const _ownSites = ['category-ru', 'category-ua', 'google', 'ua', 'youtube'];
const _ips = ['cn', 'google', 'private', 'ru', 'ua', 'us'];

/// Категории базы [kind] из [origin].
List<String> demoCategories(String kind, String origin) {
  if (kind == 'geoip') return _ips;
  return switch (origin) {
    'runetfreedom' => _runetSites,
    'custom' => _ownSites,
    _ => _sagernetSites,
  };
}

/// Название для сравнения: строчные буквы, без «-», «_», «:», «.» и пробелов,
/// без начального «category», «geosite», «geoip».
String normalizeCategory(String s) {
  var n = s.toLowerCase().replaceAll(RegExp(r'[-_:. \t]'), '');
  while (true) {
    var cut = false;
    for (final p in const ['category', 'geosite', 'geoip']) {
      if (n.length > p.length && n.startsWith(p) && n[p.length] != '@') {
        n = n.substring(p.length);
        cut = true;
      }
    }
    if (!cut) return n;
  }
}

int _distance(String a, String b, int max) {
  if ((a.length - b.length).abs() > max) return max + 1;
  var prev = List<int>.generate(b.length + 1, (j) => j);
  for (var i = 1; i <= a.length; i++) {
    final cur = List<int>.filled(b.length + 1, 0);
    cur[0] = i;
    var low = i;
    for (var j = 1; j <= b.length; j++) {
      final cost = a[i - 1] == b[j - 1] ? 0 : 1;
      var v = prev[j] + 1;
      if (cur[j - 1] + 1 < v) v = cur[j - 1] + 1;
      if (prev[j - 1] + cost < v) v = prev[j - 1] + cost;
      cur[j] = v;
      if (v < low) low = v;
    }
    if (low > max) return max + 1;
    prev = cur;
  }
  return prev[b.length];
}

({String name, bool exact, List<String> ambiguous, List<String> suggestions}) resolveCategory(List<String> names, String want) {
  var low = want.trim().toLowerCase();
  for (final p in const ['geosite:', 'geoip:']) {
    if (low.startsWith(p)) low = low.substring(p.length);
  }
  if (names.contains(low)) return (name: low, exact: true, ambiguous: const [], suggestions: const []);
  final at = low.indexOf('@');
  if (at >= 0 && !names.any((n) => n.contains('@'))) {
    final r = resolveCategory(names, low.substring(0, at));
    return (name: r.name.isEmpty ? '' : '${r.name}${low.substring(at)}', exact: false, ambiguous: r.ambiguous, suggestions: r.suggestions);
  }
  final n = normalizeCategory(low);
  if (n.isEmpty) return (name: '', exact: false, ambiguous: const [], suggestions: const []);
  final same = [
    for (final x in names)
      if (normalizeCategory(x) == n) x,
  ];
  if (same.length == 1) return (name: same.first, exact: false, ambiguous: const [], suggestions: const []);
  if (same.length > 1) return (name: '', exact: false, ambiguous: same, suggestions: const []);
  final maxDist = n.length > 8 ? 3 : (n.length > 4 ? 2 : 1);
  final found = <(String, int)>[];
  for (final x in names) {
    final m = normalizeCategory(x);
    if (m.isEmpty || m == n) continue;
    final d = _distance(n, m, maxDist);
    if (d <= maxDist) found.add((x, d));
  }
  found.sort((a, b) {
    if (a.$2 != b.$2) return a.$2.compareTo(b.$2);
    return a.$1.compareTo(b.$1);
  });
  return (name: '', exact: false, ambiguous: const [], suggestions: [for (final f in found.take(3)) f.$1]);
}

List<String> completeCategory(List<String> names, String q, [int limit = 8]) {
  var low = q.trim().toLowerCase();
  for (final p in const ['geosite:', 'geoip:']) {
    if (low.startsWith(p)) low = low.substring(p.length);
  }
  final n = normalizeCategory(low);
  if (n.isEmpty) return const [];
  final found = <(String, int)>[];
  for (final x in names) {
    final m = normalizeCategory(x);
    var rank = -1;
    if (x.startsWith(low)) {
      rank = 0;
    } else if (m.startsWith(n)) {
      rank = 1;
    } else if (n.length > 1 && m.contains(n)) {
      rank = 2;
    }
    if (rank >= 0) found.add((x, rank + (x.contains('@') ? 3 : 0)));
  }
  found.sort((a, b) {
    if (a.$2 != b.$2) return a.$2.compareTo(b.$2);
    if (a.$1.length != b.$1.length) return a.$1.length.compareTo(b.$1.length);
    return a.$1.compareTo(b.$1);
  });
  return [for (final f in found.take(limit)) f.$1];
}

/// Во что превращается правило с категорией в загруженной базе.
GeoRuleState demoRuleState(String match, List<String> names) {
  final name = match.substring(match.indexOf(':') + 1);
  final r = resolveCategory(names, name);
  if (r.name == name) return GeoRuleState(match: match, state: ruleOk);
  if (r.name.isNotEmpty) return GeoRuleState(match: match, state: ruleResolved, name: r.name);
  if (r.ambiguous.isNotEmpty) return GeoRuleState(match: match, state: ruleAmbiguous, candidates: r.ambiguous);
  return GeoRuleState(match: match, state: ruleMissing, suggestions: r.suggestions);
}
