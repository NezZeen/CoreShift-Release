part of '../app_state.dart';

/// Подписки и выбранный сервер: какой узел выбран и участвует ли он в автоматическом переходе.
extension AppStateSelection on AppState {
  int get nodeCount => subscriptions.fold(0, (n, s) => n + s.nodes.length);

  /// The selected server is in its subscription's automatic selection, so a
  /// server that stops answering is replaced by the next one by itself.
  bool get autoSwitching => subscriptionById(selection.subscription)?.auto.contains(selection.fingerprint) ?? false;

  Subscription? subscriptionById(String id) {
    for (final s in subscriptions) {
      if (s.id == id) return s;
    }
    return null;
  }

  /// Whether node [n] of [sub] is the selected one. Nodes that differ only
  /// in name share a fingerprint, so the name decides between them.
  bool isSelected(Subscription sub, NodeView n) {
    final sel = selection;
    if (sel.subscription != sub.id || sel.fingerprint != n.fingerprint) return false;
    if (n.name == sel.name) return true;
    // A stale name: the first node with the fingerprint stands in.
    final twins = sub.nodes.where((m) => m.fingerprint == n.fingerprint);
    return !twins.any((m) => m.name == sel.name) && identical(twins.first, n);
  }
}
