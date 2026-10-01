/// Russian names of the countries VPN servers and their users are usually
/// in, by ISO code; others show the code.
const _names = {
  'AE': 'ОАЭ',
  'AM': 'Армения',
  'AR': 'Аргентина',
  'AT': 'Австрия',
  'AU': 'Австралия',
  'AZ': 'Азербайджан',
  'BE': 'Бельгия',
  'BG': 'Болгария',
  'BR': 'Бразилия',
  'BY': 'Беларусь',
  'CA': 'Канада',
  'CH': 'Швейцария',
  'CN': 'Китай',
  'CY': 'Кипр',
  'CZ': 'Чехия',
  'DE': 'Германия',
  'DK': 'Дания',
  'EE': 'Эстония',
  'ES': 'Испания',
  'FI': 'Финляндия',
  'FR': 'Франция',
  'GB': 'Великобритания',
  'GE': 'Грузия',
  'GR': 'Греция',
  'HK': 'Гонконг',
  'HU': 'Венгрия',
  'IE': 'Ирландия',
  'IL': 'Израиль',
  'IN': 'Индия',
  'IS': 'Исландия',
  'IT': 'Италия',
  'JP': 'Япония',
  'KG': 'Киргизия',
  'KR': 'Южная Корея',
  'KZ': 'Казахстан',
  'LT': 'Литва',
  'LU': 'Люксембург',
  'LV': 'Латвия',
  'MD': 'Молдова',
  'MX': 'Мексика',
  'NL': 'Нидерланды',
  'NO': 'Норвегия',
  'PL': 'Польша',
  'PT': 'Португалия',
  'RO': 'Румыния',
  'RS': 'Сербия',
  'RU': 'Россия',
  'SE': 'Швеция',
  'SG': 'Сингапур',
  'SK': 'Словакия',
  'TH': 'Таиланд',
  'TJ': 'Таджикистан',
  'TR': 'Турция',
  'TW': 'Тайвань',
  'UA': 'Украина',
  'US': 'США',
  'UZ': 'Узбекистан',
  'VN': 'Вьетнам',
};

String countryName(String code) => _names[code.toUpperCase()] ?? code.toUpperCase();

/// English names, and the cities servers are usually named after, for the
/// subscriptions that name nodes in words rather than with a flag.
const _words = {
  'amsterdam': 'NL',
  'netherlands': 'NL',
  'holland': 'NL',
  'frankfurt': 'DE',
  'germany': 'DE',
  'berlin': 'DE',
  'falkenstein': 'DE',
  'nuremberg': 'DE',
  'helsinki': 'FI',
  'finland': 'FI',
  'stockholm': 'SE',
  'sweden': 'SE',
  'new york': 'US',
  'los angeles': 'US',
  'usa': 'US',
  'united states': 'US',
  'miami': 'US',
  'istanbul': 'TR',
  'turkey': 'TR',
  'almaty': 'KZ',
  'astana': 'KZ',
  'kazakhstan': 'KZ',
  'tokyo': 'JP',
  'japan': 'JP',
  'warsaw': 'PL',
  'poland': 'PL',
  'riga': 'LV',
  'latvia': 'LV',
  'vilnius': 'LT',
  'lithuania': 'LT',
  'tallinn': 'EE',
  'estonia': 'EE',
  'paris': 'FR',
  'france': 'FR',
  'madrid': 'ES',
  'spain': 'ES',
  'london': 'GB',
  'united kingdom': 'GB',
  'moscow': 'RU',
  'russia': 'RU',
  'singapore': 'SG',
  'hong kong': 'HK',
  'zurich': 'CH',
  'switzerland': 'CH',
  'vienna': 'AT',
  'austria': 'AT',
  'prague': 'CZ',
  'milan': 'IT',
  'italy': 'IT',
  'tbilisi': 'GE',
  'georgia': 'GE',
  'yerevan': 'AM',
  'armenia': 'AM',
  'kyiv': 'UA',
  'ukraine': 'UA',
  'toronto': 'CA',
  'canada': 'CA',
  'dubai': 'AE',
  'bucharest': 'RO',
  'sofia': 'BG',
  'belgrade': 'RS',
  'seoul': 'KR',
  'sydney': 'AU',
};

/// The ISO code of the country a server is in, as far as its name or host
/// say: a flag emoji, a country in Russian or English, a city, a code such
/// as "DE" or a host like "nl1.example". Null when nothing does.
String? countryOf(String name, [String host = '']) {
  // A flag is two regional-indicator letters.
  final runes = name.runes.toList();
  for (var i = 0; i + 1 < runes.length; i++) {
    final a = runes[i] - 0x1F1E6, b = runes[i + 1] - 0x1F1E6;
    if (a >= 0 && a < 26 && b >= 0 && b < 26) {
      final code = String.fromCharCodes([65 + a, 65 + b]);
      if (_names.containsKey(code)) return code;
    }
  }
  final low = name.toLowerCase();
  for (final e in _names.entries) {
    if (low.contains(e.value.toLowerCase())) return e.key;
  }
  for (final e in _words.entries) {
    if (RegExp('(^|[^a-z])${RegExp.escape(e.key)}(\$|[^a-z])').hasMatch(low)) return e.value;
  }
  final code = RegExp(r'(?<![A-Za-z])([A-Z]{2})(?![A-Za-z])').allMatches(name).map((m) => m.group(1)!).where(_names.containsKey).firstOrNull;
  if (code != null) return code;
  final fromHost = RegExp(r'^([a-z]{2})\d*[.-]', caseSensitive: false).firstMatch(host)?.group(1)?.toUpperCase();
  return fromHost != null && _names.containsKey(fromHost) ? fromHost : null;
}

/// A node's name without the flag emoji in front of it, which the country
/// badge shows instead (Windows draws such flags as two letters).
String cleanNodeName(String name) {
  final cleaned = name.replaceAll(RegExp('[\u{1F1E6}-\u{1F1FF}]{2}', unicode: true), '').replaceFirst(RegExp(r'^[\s|:·\-–—]+'), '').trim();
  return cleaned.isEmpty ? name : cleaned;
}
