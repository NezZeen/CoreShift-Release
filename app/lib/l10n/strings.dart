/// The app's words in its languages. No packages: a dictionary per language
/// and a lookup.
///
/// How to move a text of the UI into the dictionary:
///
///  1. Give it a key, dotted by where it is shown: `area.what`, e.g.
///     `nav.home`, `home.state.connected`, `settings.language`. Words of
///     the engine have their own keys (codes) in engine_strings.dart; do not
///     reuse those for the UI.
///  2. Add the key with the Russian text to [ui_ru.dart] (the source of
///     truth) and with the English to [ui_en.dart]. Values in a number or a
///     name go in braces: `'Шаг {step} из {total}'`; a word that depends on
///     a number lists its forms after bars, Russian one|few|many, English
///     one|other: `'{n} {n|сервер|сервера|серверов}'`, `'{n} {n|server|servers}'`.
///  3. In the code write `tr('nav.home')`, or `tr('wizard.step', {'step': 2, 'total': 5})`.
///     A `const` with the text loses `const`; a list of `const` records may
///     keep the key and call [tr] where it is shown (see `mainPages`).
///  4. `flutter test test/l10n_test.dart`: every key used in lib/ must be in
///     the Russian dictionary; with `strictEnglish` in that test on, every
///     Russian key in the English one too.
///
/// Missing in English, a text is said in Russian. The language is the
/// system's unless the user chose one in the settings (pref "lang").
library;

import 'package:flutter/foundation.dart';

import 'ui_en.dart';
import 'ui_ru.dart';

/// The app's languages.
enum Lang { ru, en }

/// The language the app shows now; the app rebuilds when it changes.
final appLang = ValueNotifier<Lang>(Lang.ru);

/// The current language.
Lang get lang => appLang.value;

/// The user's choice in the settings, kept in the UI preferences: "system",
/// "ru" or "en".
const langPref = 'lang';

/// The language the app last showed, for the engine: Android's
/// notifications about subscriptions are worded by it (engine/mobile/
/// subwarn.go reads "lang_used" from ui.json).
const langUsedPref = 'lang_used';

/// Whether the English dictionary is complete. Until it is, the app speaks
/// Russian whatever the system's language, and the settings offer no
/// choice: a half-translated UI is worse than none.
const englishReady = false;

/// The language for a choice in the settings: "ru", "en", or the system's
/// for anything else; the system's is Russian unless it speaks English.
/// Russian always while English is not [ready].
Lang langFor(Object? pref, {String? systemLanguage, bool ready = englishReady}) {
  if (!ready) return Lang.ru;
  switch (pref) {
    case 'ru':
      return Lang.ru;
    case 'en':
      return Lang.en;
  }
  final sys = systemLanguage ?? PlatformDispatcher.instance.locale.languageCode;
  return sys == 'en' ? Lang.en : Lang.ru;
}

const _dictionaries = {Lang.ru: uiRu, Lang.en: uiEn};

/// The UI's text of [key] in the current language, with [args] put in: in
/// Russian when English lacks it, the key itself when Russian does too.
String tr(String key, [Map<String, Object?> args = const {}]) {
  var t = _dictionaries[lang]![key];
  var l = lang;
  if (t == null) {
    t = uiRu[key];
    l = Lang.ru;
  }
  if (t == null) return key;
  return fillTemplate(t, args, l, (v) => '$v');
}

/// Puts [args] into the template [t] of language [l]: `{name}` is the
/// argument as [say] tells it, `{n|one|few|many}` the word for the number
/// n. The result is trimmed, so an argument that says nothing may end it.
String fillTemplate(String t, Map<String, Object?> args, Lang l, String Function(Object? v) say) {
  final out = StringBuffer();
  var i = 0;
  while (i < t.length) {
    final open = t.indexOf('{', i);
    final close = open < 0 ? -1 : t.indexOf('}', open);
    if (open < 0 || close < 0) {
      out.write(t.substring(i));
      break;
    }
    out.write(t.substring(i, open));
    final spec = t.substring(open + 1, close).split('|');
    final v = args[spec[0]];
    if (spec.length > 1) {
      out.write(pluralWord(l, _number(v), spec.sublist(1)));
    } else if (v != null) {
      out.write(say(v));
    }
    i = close + 1;
  }
  return out.toString().trim();
}

int _number(Object? v) => v is num ? v.toInt() : int.tryParse('$v') ?? 0;

/// The word for [n] of [forms]: Russian one, few, many; English one, other.
String pluralWord(Lang l, int n, List<String> forms) {
  String pick(int i) => i < forms.length ? forms[i] : forms.last;
  n = n.abs();
  if (l != Lang.ru) return pick(n == 1 ? 0 : 1);
  final m10 = n % 10, m100 = n % 100;
  if (m10 == 1 && m100 != 11) return pick(0);
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return pick(1);
  return pick(2);
}
