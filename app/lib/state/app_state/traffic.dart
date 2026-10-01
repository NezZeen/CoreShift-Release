part of '../app_state.dart';

/// The traffic history: bytes per day through the VPN, kept by the service.
extension AppStateTraffic on AppState {
  /// Loads the history again when the connection changed since the last
  /// look. Called by the page that shows it, so no other page causes it.
  void watchStats() {
    final k = '${status.state.name}|${DateTime.now().day}';
    if (!online || statsUnsupported || k == _statsKey) return;
    if (status.state != ConnState.connected && status.state != ConnState.idle) return;
    _statsKey = k;
    loadStats();
  }

  Future<void> loadStats() async {
    try {
      final j = await backend.call('GET', '/v1/stats?days=30') as Json;
      stats = [for (final d in (j['days'] as List? ?? const [])) TrafficDay.fromJson((d as Map).cast())];
      // What the connection has moved so far is already in the totals.
      _statsBaseUp = sessionUp;
      _statsBaseDown = sessionDown;
      statsLoaded = true;
    } on ApiError catch (e) {
      if (e.status == 404 || e.status == 405) statsUnsupported = true;
    } catch (_) {}
    _notify();
  }

  /// The last [n] days with today's live figure: the totals the service
  /// reported plus what the connection moved since.
  List<TrafficDay> statsDays(int n) {
    if (stats.isEmpty) return const [];
    final days = stats.length > n ? stats.sublist(stats.length - n) : List.of(stats);
    final last = days.last;
    final up = last.up + (sessionUp - _statsBaseUp).clamp(0, 1 << 62);
    final down = last.down + (sessionDown - _statsBaseDown).clamp(0, 1 << 62);
    return [...days.take(days.length - 1), TrafficDay(last.date, up, down)];
  }

  void _statsSoon() {
    _statsTimer?.cancel();
    // The service writes the totals when the connection ends.
    _statsTimer = Timer(const Duration(milliseconds: 600), loadStats);
  }
}
