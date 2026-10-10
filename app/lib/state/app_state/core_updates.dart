part of '../app_state.dart';

/// Автоматическая проверка и установка новых ядер: когда проверять и что делать после неудачи.
extension AppStateCoreUpdates on AppState {
  /// Runs the automatic check of the cores now, as its timer would.
  @visibleForTesting
  Future<void> autoCoreUpdatesNow() => _autoCoreUpdates();

  void _scheduleCoreCheck(Duration after) {
    _coreTimer?.cancel();
    coreCheckScheduled = after;
    if (_disposed) return;
    _coreTimer = Timer(after, () => unawaited(_autoCoreUpdates()));
  }

  /// How long the automatic check has to wait. Not while the service
  /// connects or disconnects, nor in the first seconds of a connection:
  /// a check started then, as the app opens with a service that connects
  /// by itself, failed for every core at once.
  Duration _coreCheckWait() {
    if (!online || busy || checkingUpdates || updatingCore.isNotEmpty) return const Duration(minutes: 1);
    if (status.state == ConnState.connecting || status.state == ConnState.disconnecting) return const Duration(seconds: 5);
    // The status the app loaded tells it before the events do.
    final changed = [_stateChangedAt, status.since].nonNulls.fold<DateTime?>(null, (a, b) => a == null || b.isAfter(a) ? b : a);
    final since = changed == null ? null : DateTime.now().difference(changed);
    if (since != null && since < AppState._coreCheckSettle) return AppState._coreCheckSettle - since;
    return Duration.zero;
  }

  /// Looks for newer cores and installs them, without a word. A check that
  /// fails is tried again in an hour, and only one that keeps failing is
  /// told, in one line of the journal for every core. Android updates its
  /// cores with the app.
  Future<void> _autoCoreUpdates() async {
    if (platform.isAndroid || _disposed) return;
    _coresChecked = true;
    final wait = _coreCheckWait();
    if (wait > Duration.zero) {
      _scheduleCoreCheck(wait);
      return;
    }
    final failed = await checkCoreUpdates();
    final temporary = failed.values.any(coreCheckTemporary);
    if (failed.isEmpty) {
      _coreCheckFailures = 0;
    } else if (++_coreCheckFailures == AppState._coreCheckFailuresToLog) {
      _log(DateTime.now(), 'ядра', coreCheckFailedText(failed, _coreCheckFailures, retryInHour: temporary), LogLevel.warn);
    }
    await installCoreUpdates();
    _scheduleCoreCheck(temporary ? AppState._coreCheckRetry : AppState._coreCheckEvery);
  }
}
