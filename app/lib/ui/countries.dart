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

/// Russian names of the big cities, for a geolocation service that gives
/// them in English; others stay as the service wrote them.
const _cities = {
  'moscow': 'Москва',
  'saint petersburg': 'Санкт-Петербург',
  'st petersburg': 'Санкт-Петербург',
  'novosibirsk': 'Новосибирск',
  'yekaterinburg': 'Екатеринбург',
  'kyiv': 'Киев',
  'kiev': 'Киев',
  'kharkiv': 'Харьков',
  'odesa': 'Одесса',
  'odessa': 'Одесса',
  'dnipro': 'Днепр',
  'minsk': 'Минск',
  'warsaw': 'Варшава',
  'berlin': 'Берлин',
  'frankfurt am main': 'Франкфурт-на-Майне',
  'frankfurt': 'Франкфурт',
  'amsterdam': 'Амстердам',
  'london': 'Лондон',
  'paris': 'Париж',
  'helsinki': 'Хельсинки',
  'stockholm': 'Стокгольм',
  'tallinn': 'Таллин',
  'riga': 'Рига',
  'vilnius': 'Вильнюс',
  'prague': 'Прага',
  'vienna': 'Вена',
  'zurich': 'Цюрих',
  'madrid': 'Мадрид',
  'milan': 'Милан',
  'istanbul': 'Стамбул',
  'tbilisi': 'Тбилиси',
  'yerevan': 'Ереван',
  'almaty': 'Алма-Ата',
  'astana': 'Астана',
  'tel aviv': 'Тель-Авив',
  'dubai': 'Дубай',
  'new york': 'Нью-Йорк',
  'los angeles': 'Лос-Анджелес',
  'singapore': 'Сингапур',
  'tokyo': 'Токио',
  'hong kong': 'Гонконг',
};

String cityName(String city) => _cities[city.trim().toLowerCase()] ?? city;

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

/// The Russian names of [_names] in lower case, the same for every call.
final _lowNames = [for (final e in _names.entries) (e.value.toLowerCase(), e.key)];

/// The words of [_words] as the patterns that find them whole (not inside
/// another word), compiled once: a server list asks for every name on every
/// redraw, and building seventy patterns per name made a redraw of a few
/// hundred servers last seconds.
final _wordPatterns = [for (final e in _words.entries) (RegExp('(^|[^a-z])${RegExp.escape(e.key)}(\$|[^a-z])'), e.value)];

final _codeInName = RegExp(r'(?<![A-Za-z])([A-Z]{2})(?![A-Za-z])');
final _codeInHost = RegExp(r'^([a-z]{2})\d*[.-]', caseSensitive: false);
final _flagPair = RegExp('[\u{1F1E6}-\u{1F1FF}]{2}', unicode: true);
final _leadingJunk = RegExp(r'^[\s|:·\-–—]+');

/// A country or a place is named in the first words; a name far longer than
/// any real one (decorations repeated by the hundred) is read to here only.
const _readNameTo = 400;

/// What was worked out for a name already: a list gives the same names to
/// the same page again and again. Emptied when it outgrows [_memoLimit].
final _countryMemo = <String, String?>{};
final _cleanMemo = <String, String>{};
const _memoLimit = 4096;

/// The ISO code of the country a server is in, as far as its name or host
/// say: a flag emoji, a country in Russian or English, a city, a code such
/// as "DE" or a host like "nl1.example". Null when nothing does.
String? countryOf(String name, [String host = '']) {
  final key = '$host\u0000$name';
  if (_countryMemo.containsKey(key)) return _countryMemo[key];
  final code = _countryOf(name.length > _readNameTo ? name.substring(0, _readNameTo) : name, host.length > 255 ? host.substring(0, 255) : host);
  // A name this long is no real one: not kept, so a hostile list cannot fill the memory.
  if (key.length > 2 * _readNameTo) return code;
  if (_countryMemo.length >= _memoLimit) _countryMemo.clear();
  return _countryMemo[key] = code;
}

String? _countryOf(String name, String host) {
  // A flag is two regional-indicator letters.
  int? prev;
  for (final r in name.runes) {
    final b = r - 0x1F1E6;
    final isLetter = b >= 0 && b < 26;
    if (prev != null && isLetter) {
      final code = String.fromCharCodes([65 + prev, 65 + b]);
      if (_names.containsKey(code)) return code;
    }
    prev = isLetter ? b : null;
  }
  final low = name.toLowerCase();
  for (final (word, code) in _lowNames) {
    if (low.contains(word)) return code;
  }
  for (final (pattern, code) in _wordPatterns) {
    if (pattern.hasMatch(low)) return code;
  }
  for (final m in _codeInName.allMatches(name)) {
    if (_names.containsKey(m.group(1))) return m.group(1);
  }
  final fromHost = _codeInHost.firstMatch(host)?.group(1)?.toUpperCase();
  return fromHost != null && _names.containsKey(fromHost) ? fromHost : null;
}

/// A node's name without the flag emoji in front of it, which the country
/// badge shows instead (Windows draws such flags as two letters).
String cleanNodeName(String name) {
  final known = _cleanMemo[name];
  if (known != null) return known;
  final cleaned = name.replaceAll(_flagPair, '').replaceFirst(_leadingJunk, '').trim();
  final result = cleaned.isEmpty ? name : cleaned;
  if (name.length > 2 * _readNameTo) return result;
  if (_cleanMemo.length >= _memoLimit) _cleanMemo.clear();
  return _cleanMemo[name] = result;
}
