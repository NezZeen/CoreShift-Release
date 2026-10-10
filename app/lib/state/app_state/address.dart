part of '../app_state.dart';

/// Адрес, который видят сайты: узнаётся при изменении подключения.
extension AppStateAddress on AppState {
  /// Looks the address up again when the connection changed since the last
  /// lookup: on connecting, disconnecting and moving to another server.
  /// Called by the page that shows it, so no other page causes lookups.
  void watchIp() {
    final st = status;
    if (!online || ipUnsupported || (st.state != ConnState.connected && st.state != ConnState.idle)) return;
    final key = '${st.state.name}|${st.node}';
    if (key == _ipKey) return;
    _ipKey = key;
    // A moment after connecting, for the routes to settle.
    _ipTimer?.cancel();
    _ipTimer = Timer(Duration(milliseconds: st.state == ConnState.connected ? 800 : 0), refreshIp);
  }

  Future<void> refreshIp() async {
    if (ipLoading || _disposed) return;
    ipLoading = true;
    _notify();
    try {
      publicIp = IpInfo.fromJson(await backend.call('GET', '/v1/ip') as Json);
      ipError = '';
    } on ApiError catch (e) {
      if (e.status == 404 || e.status == 405) ipUnsupported = true;
      ipError = 'Не удалось узнать адрес';
    } catch (_) {
      ipError = 'Не удалось узнать адрес';
    }
    ipLoading = false;
    _notify();
  }
}
