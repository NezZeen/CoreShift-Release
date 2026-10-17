part of '../app_state.dart';

/// What differs between two settings objects, one entry per changed value:
/// "tun: да → нет", "routing.direct_domains: 3 записи → 4 записи". Lists
/// are counted, not listed: they can be long.
/// Settings whose values never go into the journal.
const _secretSettings = {'proxy.user', 'proxy.pass'};

List<String> settingsChanges(Map before, Map after, [String prefix = '']) {
  String show(Object? v) => switch (v) {
    true => tr('state.settingsDiff.yes'),
    false => tr('state.settingsDiff.no'),
    null => '—',
    List l => tr('state.settingsDiff.entries', {'n': l.length}),
    String s when s.isEmpty => tr('state.settingsDiff.emptyText'),
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
        out.add(
          a.length == b.length
              ? tr('state.settingsDiff.listChanged', {
                  'path': path,
                  'count': tr('state.settingsDiff.entries', {'n': b.length}),
                })
              : '$path: ${show(a)} → ${show(b)}',
        );
      }
    } else if (a != b && _secretSettings.contains(path)) {
      // The proxy's credentials: the journal is copied for support.
      out.add(tr('state.settingsDiff.changed', {'path': path}));
    } else if (a != b) {
      out.add('$path: ${show(a)} → ${show(b)}');
    }
  }
  return out;
}
