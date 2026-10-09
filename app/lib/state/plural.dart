/// The Russian word for a count: [one] for 1, 21, 101 («запись»), [few] for
/// 2–4, 22–24 («записи»), [many] for 0, 5–20, 11–14, 111 («записей»).
String ruPlural(int n, String one, String few, String many) {
  final m10 = n.abs() % 10, m100 = n.abs() % 100;
  if (m10 == 1 && m100 != 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few;
  return many;
}

/// "1 запись", "3 записи", "5 записей".
String entriesCount(int n) => '$n ${ruPlural(n, 'запись', 'записи', 'записей')}';
