part of '../app_state.dart';

/// The server list's own state: favourites, the last servers used, how the
/// list is sorted and grouped. Kept in the app's preferences, per user. The
/// servers removed from the list are the daemon's (Subscription.hiddenNodes):
/// it leaves them out of the switch to another server too.
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

  /// The favourites, in the order they were starred, then the servers used
  /// last, the newest first: up to [limit] in all, though every favourite is
  /// there. The ones a subscription no longer has, or that were removed from
  /// the list, are left out.
  List<(Subscription, NodeView)> quickNodes({int limit = 6}) {
    final out = <(Subscription, NodeView)>[];
    final seen = <String>{};
    final favs = _prefList('favorites');
    for (final (i, k) in [...favs, ..._prefList('recent')].indexed) {
      if ((i >= favs.length && out.length >= limit) || !seen.add(k)) continue;
      final at = k.indexOf('/');
      if (at < 0) continue;
      final sub = subscriptionById(k.substring(0, at));
      final node = sub?.nodes.where((n) => n.fingerprint == k.substring(at + 1)).firstOrNull;
      if (sub != null && node != null) out.add((sub, node));
    }
    return out;
  }

  /// The working node with the lowest ping among all subscriptions, if any
  /// was tested. While the connected server does not answer it is left out,
  /// in every subscription that has it: its ping is from before, and
  /// «Другой сервер» offered it again.
  (Subscription, NodeView)? fastestNode() {
    (Subscription, NodeView)? best;
    var bestMs = 0;
    final skip = serverUnresponsive ? selection.fingerprint : null;
    for (final sub in subscriptions) {
      for (final n in sub.nodes) {
        if (n.fingerprint == skip) continue;
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

  /// Removes the servers [fingerprints] of subscription [id] from the list,
  /// or brings them back. Returns whether the daemon took it.
  Future<bool> setNodesHidden(String id, List<String> fingerprints, {required bool hidden}) => _act(() async {
    final sub = Subscription.fromJson(await backend.call('POST', '/v1/subscriptions/$id/hidden', {'fingerprints': fingerprints, 'hidden': hidden}) as Json);
    subscriptions = [for (final s in subscriptions) s.id == id ? sub : s];
    _notify();
  });

  /// «Удалить из подписки»: the server leaves the list, with a toast to
  /// take it back. [label] is its name as the list shows it.
  Future<void> hideNode(Subscription sub, NodeView n, {String? label}) => hideNodes([(sub, n)], label: label);

  /// Removes [rows] from the list, one request per subscription, with one
  /// toast that brings them all back. [label] names a single server.
  Future<void> hideNodes(List<(Subscription, NodeView)> rows, {String? label}) async {
    final bySub = <String, Set<String>>{};
    for (final (sub, n) in rows) {
      (bySub[sub.id] ??= {}).add(n.fingerprint);
    }
    if (bySub.isEmpty) return;
    final done = <String, List<String>>{};
    for (final MapEntry(key: id, value: fps) in bySub.entries) {
      if (await setNodesHidden(id, fps.toList(), hidden: true)) done[id] = fps.toList();
    }
    if (done.isEmpty) return;
    final count = done.values.fold(0, (n, fps) => n + fps.length);
    final names = {for (final (_, n) in rows) n.name};
    _logAction('удалить из подписки: ${names.join(', ')}');
    Future<void> undo() async {
      for (final MapEntry(key: id, value: fps) in done.entries) {
        await setNodesHidden(id, fps, hidden: false);
      }
    }

    toast(
      count == 1 ? '«${label ?? rows.first.$2.name}» удалён из подписки' : 'Удалено из подписки: $count ${ruPlural(count, 'сервер', 'сервера', 'серверов')}',
      ToastKind.info,
      ('Отменить', undo),
    );
  }
}
