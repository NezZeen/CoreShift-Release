part of '../app_state.dart';

/// The server list's own state: favourites, the last servers used, how the
/// list is sorted and grouped. Kept in the app's preferences, per user.
extension AppStateServers on AppState {
  static String key(String subscription, String fingerprint) => '$subscription/$fingerprint';

  List<String> _prefList(String name) => [for (final v in (prefs[name] as List?) ?? const []) '$v'];

  Set<String> get favorites => _prefList('favorites').toSet();

  bool isFavorite(Subscription sub, NodeView n) => favorites.contains(key(sub.id, n.fingerprint));

  void toggleFavorite(Subscription sub, NodeView n) {
    final list = _prefList('favorites');
    final k = key(sub.id, n.fingerprint);
    if (!list.remove(k)) list.add(k);
    setPref('favorites', list);
  }

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

  /// The favourites, then the servers used last, up to [limit] in all; the
  /// ones a subscription no longer has are left out.
  List<(Subscription, NodeView)> quickNodes({int limit = 6}) {
    final out = <(Subscription, NodeView)>[];
    final seen = <String>{};
    for (final k in [..._prefList('favorites'), ..._prefList('recent')]) {
      if (out.length >= limit || !seen.add(k)) continue;
      final i = k.indexOf('/');
      final sub = subscriptionById(k.substring(0, i));
      final node = sub?.nodes.where((n) => n.fingerprint == k.substring(i + 1)).firstOrNull;
      if (sub != null && node != null) out.add((sub, node));
    }
    return out;
  }

  /// The working node with the lowest ping among all subscriptions, if any
  /// was tested.
  (Subscription, NodeView)? fastestNode() {
    (Subscription, NodeView)? best;
    var bestMs = 0;
    for (final sub in subscriptions) {
      for (final n in sub.nodes) {
        final l = latencyOf(sub.id, n.fingerprint);
        if (l != null && l.ok && n.cores.isNotEmpty && (best == null || l.ms < bestMs)) {
          best = (sub, n);
          bestMs = l.ms;
        }
      }
    }
    return best;
  }

  /// How the server list is ordered: "sub" (as the subscription has it),
  /// "ping" or "name".
  String get serverSort => const {'sub', 'ping', 'name'}.contains(prefs['server_sort']) ? prefs['server_sort'] as String : 'sub';

  bool get serverGroup => prefs['server_group'] == true;
}
