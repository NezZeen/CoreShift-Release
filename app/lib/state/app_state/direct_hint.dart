part of '../app_state.dart';

/// Networks where direct connections do not get through: a mobile operator
/// that lets only a white list of addresses through, or Russian sites out of
/// reach from abroad. The engine sees the TUN layer's refusals while the
/// tunnel works and says so ([Status.directBlocked], the "direct" event);
/// the home page then offers to send everything through the VPN. Nothing
/// changes without the user's tap.
extension AppStateDirectHint on AppState {
  /// The home page shows the hint: connected, the engine found direct
  /// connections failing, and the user has not put it off in this run of
  /// the app (the app cannot tell one network from another).
  bool get directHint => status.state == ConnState.connected && (status.directBlocked || _directBlocked) && !_directDismissed;

  /// The settings send something direct that [fixDirect] can change: the
  /// Russian preset or the mode «Только выбранное». Otherwise only the
  /// user's own direct lists are left, which are theirs to edit.
  bool get directFixable => setting('routing.russia_direct', false) == true || setting('routing.mode', 'all') == 'selected';

  void _onDirectEvent(Event e) {
    _directBlocked = true;
    if (e.line.isNotEmpty) _log(e.time, 'сеть', e.line, LogLevel.warn);
    _notify();
  }

  /// Sends everything through the VPN: the mode «Всё через VPN», Russian
  /// sites no longer direct; then reconnects, so that it applies at once.
  Future<void> fixDirect() async {
    _logAction('всё через VPN: прямые соединения в этой сети не проходят');
    final err = await updateSettings((s) {
      final r = (s['routing'] as Map?) ?? (s['routing'] = <String, dynamic>{});
      r['mode'] = 'all';
      r['russia_direct'] = false;
    });
    if (err != null) return;
    _directBlocked = false;
    await reconnect();
  }

  /// «Не сейчас»: the hint does not come back until the app starts again.
  void dismissDirectHint() {
    _directDismissed = true;
    _notify();
  }
}
