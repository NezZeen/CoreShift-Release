part of '../app_state.dart';

/// What differs between two settings objects, one entry per changed value:
/// "tun: да → нет", "routing.direct_domains: 3 → 4 записей". Lists are
/// counted, not listed: they can be long.
List<String> settingsChanges(Map before, Map after, [String prefix = '']) {
  String show(Object? v) => switch (v) {
    true => 'да',
    false => 'нет',
    null => '—',
    List l => '${l.length} записей',
    String s when s.isEmpty => '«»',
    _ => '$v',
  };
  final out = <String>[];
  for (final k in {...before.keys, ...after.keys}) {
    final a = before[k], b = after[k];
    final path = '$prefix$k';
    if (a is Map && b is Map) {
      out.addAll(settingsChanges(a, b, '$path.'));
    } else if (a is List && b is List) {
      if (jsonEncode(a) != jsonEncode(b)) {
        out.add(a.length == b.length ? '$path: изменён список (${b.length} записей)' : '$path: ${show(a)} → ${show(b)}');
      }
    } else if (a != b) {
      out.add('$path: ${show(a)} → ${show(b)}');
    }
  }
  return out;
}
