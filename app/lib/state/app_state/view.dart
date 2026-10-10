part of '../app_state.dart';

/// Видно ли окно: от этого зависит, как часто служба присылает трафик и перерисовывается ли он.
extension AppStateView on AppState {
  /// Says whether the window is on screen: DesktopFrame, from the window's
  /// events. Shown again, the speed and the graph are brought up to date at
  /// once, and so is the service's pace.
  void setShown(bool v) {
    if (_disposed || _shown.value == v) return;
    _shown.value = v;
    if (v && _trafficMissed) {
      _trafficMissed = false;
      _traffic.value++;
    }
    if (online) unawaited(_sendView());
  }

  /// Tells the service whether this window is hidden (POST /v1/view), so
  /// that it samples the traffic seldom while no window shows it.
  Future<void> _sendView() async {
    final view = backend.view;
    if (view.isEmpty || _disposed) return;
    try {
      await backend.call('POST', '/v1/view', {'view': view, 'hidden': !_shown.value});
    } catch (_) {
      // A service before 0.8.3, or its stream just reopening: it samples
      // at its usual pace, which costs it little.
    }
  }
}
