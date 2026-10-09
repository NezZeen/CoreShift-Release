import 'punycode.dart';

/// The user's own routing rules (routing.rules): what a rule matches and
/// what is done with it. The service checks them again
/// (engine/internal/store/rules.go); this is to explain a mistake before
/// asking it.

/// What a rule does: through the VPN, around it, or refused.
const ruleActions = [('proxy', 'Через VPN'), ('direct', 'Напрямую'), ('block', 'Блокировать')];

/// A rule's action in words.
String ruleActionName(String action) => ruleActions.firstWhere((a) => a.$1 == action, orElse: () => (action, action)).$2;

/// A geosite or geoip category as v2ray and SagerNet name them:
/// "category-ads-all", "geolocation-!cn", "google@cn".
final _category = RegExp(r'^[a-z0-9][a-z0-9!@._-]*$');
final _domain = RegExp(r'^[a-z0-9_-]+(\.[a-z0-9_-]+)*$');
final _address = RegExp(r'^([0-9.]+|[0-9a-f:.]*:[0-9a-f:.]*)(/\d{1,3})?$');

const maxCategoryName = 64;

/// A rule typed by the user, tidied as the service stores it, or why it
/// cannot be one. "GeoSite:YouTube" gives "geosite:youtube",
/// "domain:Example.com" "example.com", "Госуслуги.рф" its punycode.
({String match, String? error}) parseRule(String raw) {
  final v = raw.trim();
  if (v.isEmpty) return (match: '', error: 'Введите правило: например geosite:youtube или example.com');
  final colon = v.indexOf(':');
  final prefix = colon < 0 ? '' : v.substring(0, colon).toLowerCase();
  if (prefix == 'geosite' || prefix == 'geoip') {
    final name = v.substring(colon + 1).trim().toLowerCase();
    final err = _categoryError(prefix, name);
    return (match: err == null ? '$prefix:$name' : '', error: err);
  }
  var rest = v;
  if (prefix == 'domain') rest = v.substring(colon + 1).trim();
  final low = rest.toLowerCase();
  if (prefix != 'domain' && (low.contains('/') || low.contains(':') || RegExp(r'^[0-9.]+$').hasMatch(low))) {
    if (colon > 0 && !(_address.hasMatch(low) && _validAddress(low))) {
      return (match: '', error: 'Неизвестное начало «${v.substring(0, colon + 1)}»: бывают geosite:, geoip: и domain:');
    }
    if (!_address.hasMatch(low) || !_validAddress(low)) return (match: '', error: '«$v» — не похоже на IP-адрес или подсеть.');
    return (match: low, error: null);
  }
  final d = domainToAscii(low.replaceAll(RegExp(r'^\.+|\.+$'), ''));
  if (d.isEmpty || d.length > 253 || !_domain.hasMatch(d) || d.split('.').any((l) => l.length > 63 || l.startsWith('-') || l.endsWith('-'))) {
    return (match: '', error: '«$v» — не похоже на адрес сайта.');
  }
  return (match: d, error: null);
}

String? _categoryError(String kind, String name) {
  if (name.isEmpty) return 'Укажите категорию после «$kind:», например $kind:${kind == 'geoip' ? 'ru' : 'youtube'}';
  if (name.length > maxCategoryName) return 'Имя категории длиннее $maxCategoryName знаков.';
  if (!_category.hasMatch(name) || name.contains('..')) return 'В имени категории бывают только латинские буквы, цифры и !@._-';
  final parts = name.split('@');
  if (parts.length > 1 && kind == 'geoip') return 'У категорий geoip нет атрибутов (@…).';
  if (parts.any((p) => p.isEmpty)) return 'Пустая категория или атрибут после @.';
  return null;
}

bool _validAddress(String v) {
  final parts = v.split('/');
  final addr = parts[0];
  final bits = parts.length > 1 ? int.tryParse(parts[1]) : null;
  if (addr.contains(':')) {
    try {
      Uri.parseIPv6Address(addr);
    } on FormatException {
      return false;
    }
    return bits == null || bits <= 128;
  }
  final octets = addr.split('.');
  if (octets.length != 4 || octets.any((o) => o.isEmpty || (int.tryParse(o) ?? 256) > 255)) return false;
  return bits == null || bits <= 32;
}

/// What a rule matches, for the list: a category stays as typed, a site in
/// another script is shown as typed rather than in punycode.
String ruleLabel(String match) => match.startsWith('geosite:') || match.startsWith('geoip:') ? match : domainToUnicode(match);

/// Where categories come from (routing.geo.source).
const geoSources = [('sagernet', 'SagerNet'), ('runetfreedom', 'runetfreedom'), ('custom', 'Своя ссылка')];

/// A link the user gives as a source of categories, or why it cannot be.
String? sourceLinkError(String raw) {
  final v = raw.trim();
  if (v.isEmpty) return null;
  if (v.length > 2048) return 'Ссылка длиннее 2048 знаков.';
  if ('{name}'.allMatches(v).length > 1) return '{name} должно быть в ссылке один раз.';
  final u = Uri.tryParse(v.replaceFirst('{name}', 'x'));
  if (u == null || u.scheme != 'https' || u.host.isEmpty || v.contains(' ')) return 'Ссылка должна начинаться с https://';
  if (u.userInfo.isNotEmpty) return 'В ссылке не должно быть имени и пароля.';
  return null;
}
