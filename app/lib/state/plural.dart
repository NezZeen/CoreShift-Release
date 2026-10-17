import '../l10n/strings.dart';

/// The Russian word for a count: [one] for 1, 21, 101 («запись»), [few] for
/// 2–4, 22–24 («записи»), [many] for 0, 5–20, 11–14, 111 («записей»).
/// For the words of the UI use a template of the dictionary instead, with
/// `{n|one|few|many}` (English `{n|one|other}`): it follows the language.
String ruPlural(int n, String one, String few, String many) => pluralWord(Lang.ru, n, [one, few, many]);

/// "1 запись", "3 записи", "5 записей"; "1 entry", "3 entries".
String entriesCount(int n) => tr('fmt.entries', {'n': n});
