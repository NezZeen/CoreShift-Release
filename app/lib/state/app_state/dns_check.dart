part of '../app_state.dart';

/// How a DNS field of the settings checked out: the engine tried the
/// server (POST /v1/dnscheck) from the physical network, or through the
/// tunnel for the one that lives there.
class DnsCheck {
  /// What was tried, as the settings had it; a result of another value is
  /// not the field's.
  final String server;
  final bool running;

  /// "ok", "fail" or "skipped" (it could not be tried now), empty while
  /// [running].
  final String status;

  /// Why not, as the engine names it: "refused", "timeout", "tls", …;
  /// "not_connected" for a server that waits for the VPN.
  final String kind;

  /// The result in the app's language.
  final String text;
  const DnsCheck({required this.server, this.running = false, this.status = '', this.kind = '', this.text = ''});

  bool get ok => status == 'ok';
  bool get failed => status == 'fail';
}

/// The resolvers the user types in the settings. A provider may block the
/// one for sites that go around the VPN (a white list, a filter on DNS over
/// HTTPS), and sites then stay blank for no visible reason: every change is
/// tried at once and the result shown under the field. While connected the
/// engine also sees the resolver fail and falls back to the network's own;
/// the home page then says so (see [dnsFallbackHint]).
extension AppStateDnsCheck on AppState {
  /// The title of the setting behind [which] ("direct" or "remote").
  static String dnsName(String which) => which == 'direct' ? 'DNS напрямую' : 'DNS через VPN';

  String _dnsSetting(String which) => setting('dns.$which', '').trim();

  /// Tries the server the field [which] has now. Nothing is tried for an
  /// empty one (the system's resolver is used), nor against a service that
  /// predates the check.
  Future<void> checkDns(String which) async {
    final server = _dnsSetting(which);
    if (server.isEmpty || !online || dnsCheckUnsupported) {
      if (dnsChecks.remove(which) != null) _notify();
      return;
    }
    // Saving a field can come twice (the keyboard's «Готово» and the focus
    // leaving): one check is enough.
    if (dnsChecks[which] case final c? when c.running && c.server == server) return;
    dnsChecks[which] = DnsCheck(server: server, running: true);
    _notify();
    DnsCheck? result;
    try {
      final r = await backend.call('POST', '/v1/dnscheck', {which: server}) as Json;
      final item = (r[which] as Map?)?.cast<String, dynamic>();
      if (item != null) {
        result = DnsCheck(
          server: server,
          status: '${item['status'] ?? ''}',
          kind: '${item['kind'] ?? ''}',
          text: engineText('${item['code'] ?? ''}', item['args'], '${item['line'] ?? ''}'),
        );
      }
    } on ApiError catch (e) {
      if (e.status == 404 || e.status == 405) {
        dnsCheckUnsupported = true;
      } else {
        result = DnsCheck(server: server, status: 'fail', text: 'не удалось проверить: ${e.message}');
      }
    } catch (e) {
      if (e is DaemonOffline) _lost(e);
    }
    // The field changed meanwhile: the newer check speaks.
    if (_dnsSetting(which) != server) return;
    if (result == null) {
      dnsChecks.remove(which);
    } else {
      dnsChecks[which] = result;
      _log(
        DateTime.now(),
        'DNS',
        '${dnsName(which)} ($server): ${result.text}',
        result.ok
            ? LogLevel.ok
            : result.failed
            ? LogLevel.warn
            : LogLevel.info,
      );
    }
    _notify();
  }

  /// The home page tells that «DNS напрямую» is out of reach in this
  /// network and the network's own answers instead: the engine found it
  /// ([Status.directDns], the "dns-direct" event) and the user has not put
  /// it off in this run of the app.
  bool get dnsFallbackHint => status.state == ConnState.connected && (status.directDns.isNotEmpty || _dnsDown.isNotEmpty) && !_dnsDownDismissed;

  /// The address of the resolver the hint is about.
  String get dnsFallbackAddress => status.directDns.isNotEmpty ? status.directDns : _dnsDown;

  /// Why it is out of reach, in the app's language: from the status, else
  /// from the event that came first.
  String get dnsFallbackWhy => status.directDns.isNotEmpty ? status.directDnsWhyText : _dnsDownWhy;

  void _onDnsDirectEvent(Event e) {
    if (e.reason == 'restored') {
      _dnsDown = _dnsDownWhy = '';
      _dnsDownDismissed = false;
      if (e.line.isNotEmpty) _log(e.time, 'сеть', e.lineText, LogLevel.ok);
    } else {
      _dnsDown = '${e.args['addr'] ?? ''}'.isEmpty ? 'DNS' : '${e.args['addr']}';
      _dnsDownWhy = engineJson(e.args['why']) ?? '';
      if (e.line.isNotEmpty) _log(e.time, 'сеть', e.lineText, LogLevel.warn);
    }
    _statusSoon();
    _notify();
  }

  /// «Не сейчас»: the hint does not come back until the resolver answers
  /// and fails again, or the app starts again.
  void dismissDnsFallbackHint() {
    _dnsDownDismissed = true;
    _notify();
  }

  /// «Настройки DNS»: the settings page opens with the DNS fields in view.
  void showDnsSettings() {
    dnsSettingsRequested = true;
    _notify();
  }

  /// Whether the settings page is to open the DNS fields; asking takes the
  /// request, so that the page opens that way once.
  bool takeDnsSettingsRequest() {
    final r = dnsSettingsRequested;
    dnsSettingsRequested = false;
    return r;
  }
}
