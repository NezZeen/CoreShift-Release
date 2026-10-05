part of '../app_state.dart';

/// The server list's own state: the last servers used and how the list is
/// grouped. Kept in the app's preferences, per user.
extension AppStateServers on AppState {
  static String key(String subscription, String fingerprint) => '$subscription/$fingerprint';

  List<String> _prefList(String name) => [for (final v in (prefs[name] as List?) ?? const []) '$v'];

  /// Remembers the server connected to, the newest first.
  void noteRecent(String subscription, String fingerprint) {
    if (subscription.isEmpty || fingerprint.isEmpty) return;
    final k = key(subscription, fingerprint);
    final list = _prefList('recent')
      ..remove(k)
      ..insert(0, k);
    prefs['recent'] = list.take(8).toList();
    savePrefs?.call(prefs);
  }

  /// The servers used last, the newest first, up to [limit]; the ones a
  /// subscription no longer has are left out.
  List<(Subscription, NodeView)> quickNodes({int limit = 6}) {
    final out = <(Subscription, NodeView)>[];
    final seen = <String>{};
    for (final k in _prefList('recent')) {
      if (out.length >= limit || !seen.add(k)) continue;
      final i = k.indexOf('/');
      final sub = subscriptionById(k.substring(0, i));
      final node = sub?.nodes.where((n) => n.fingerprint == k.substring(i + 1)).firstOrNull;
      if (sub != null && node != null) out.add((sub, node));
    }
    return out;
  }

  /// The working node with the lowest ping among all subscriptions, if any
  /// was tested. While the connected server does not answer it is left out:
  /// its ping is from before, and «Другой сервер» offered it again.
  (Subscription, NodeView)? fastestNode() {
    (Subscription, NodeView)? best;
    var bestMs = 0;
    final skipSelected = serverUnresponsive;
    for (final sub in subscriptions) {
      for (final n in sub.nodes) {
        if (skipSelected && isSelected(sub, n)) continue;
        final l = latencyOf(sub.id, n.fingerprint);
        if (l != null && l.ok && n.cores.isNotEmpty && (best == null || l.ms < bestMs)) {
          best = (sub, n);
          bestMs = l.ms;
        }
      }
    }
    return best;
  }

  bool get serverGroup => prefs['server_group'] == true;
}
