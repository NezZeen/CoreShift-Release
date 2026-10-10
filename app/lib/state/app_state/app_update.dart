part of '../app_state.dart';

/// Обновление самого CoreShift: проверка, установка, предложение окну и ответ службы.
extension AppStateAppUpdate on AppState {
  /// Services before 0.3.0 have no self-update: it stays off for them.
  Future<void> _loadAppUpdate() async {
    try {
      appUpdate = AppUpdateInfo.fromJson(await backend.call('GET', '/v1/app-update') as Json);
    } catch (_) {
      appUpdate = const AppUpdateInfo();
    }
  }

  Future<bool> checkAppUpdate() => _act(() async {
    appUpdate = AppUpdateInfo.fromJson(await backend.call('POST', '/v1/app-update/check') as Json);
    _notify();
  });

  /// Installs the downloaded update now; a connection comes back after it.
  /// On Android the system's installer asks the user first, once Android
  /// lets CoreShift install apps at all.
  Future<bool> installAppUpdate() async {
    if (!await platform.canInstallUpdates()) {
      _installWhenAllowed = true;
      await platform.allowInstallUpdates();
      toast('Разрешите CoreShift устанавливать приложения и вернитесь: обновление продолжится.');
      return false;
    }
    return _act(() async {
      appUpdate = AppUpdateInfo.fromJson(await backend.call('POST', '/v1/app-update/install') as Json);
      _notify();
    });
  }

  /// Back in the app: the update goes on if the user allowed installing.
  Future<void> resumed() async {
    if (!_installWhenAllowed) return;
    _installWhenAllowed = false;
    if (appUpdate.state == 'ready' && await platform.canInstallUpdates()) await installAppUpdate();
  }

  /// On the desktop an update that installs by itself right away (automatic
  /// updates on, VPN off) is not offered: the installer is already starting.
  /// On Linux a newer version is only announced ('available'), to be
  /// downloaded from its page.
  bool get offerUpdate =>
      appUpdate.label != _updateOffered &&
      ((appUpdate.state == 'ready' && (platform.isAndroid || appUpdate.waiting || !setting('app_update.auto', true))) || appUpdate.state == 'available');

  /// Opens the page of an announced update (Linux) in the browser; false
  /// when there is none or it could not be opened. Only GitHub (or GitLab mirror) release
  /// pages, which the daemon has checked too.
  Future<bool> openUpdatePage() async {
    final url = appUpdate.url;
    if (!isReleasePage(url)) return false;
    return platform.openUrl(url);
  }

  void updateOffered() => _updateOffered = appUpdate.label;

  Future<void> _onAppUpdate(Event e, bool live) async {
    if (e.error.isNotEmpty) _log(e.time, 'обновление', journalCodedError(e.code, e.args, e.error), LogLevel.warn);
    // A check that found nothing says so too: the journal shows it ran.
    final checked = _appUpdateWas == 'checking' && e.reason == 'idle' && e.error.isEmpty;
    _appUpdateWas = e.reason;
    if (checked && version.known) _log(e.time, 'обновление', 'проверено: CoreShift ${version.label} — последняя версия', LogLevel.info);
    if (e.reason == 'installed') return; // the new app says so itself
    await _loadAppUpdate();
    if (live && e.reason == 'ready') {
      final label = appUpdate.label;
      // The window offering it says the rest (see Shell).
      if (appUpdate.waiting) {
        _log(e.time, 'обновление', 'скачана версия $label, установится после отключения VPN', LogLevel.info);
      } else if (platform.isAndroid) {
        _log(e.time, 'обновление', 'скачана версия $label', LogLevel.info);
      } else {
        _log(e.time, 'обновление', 'скачана версия $label, устанавливается', LogLevel.info);
      }
    }
    _notify();
  }
}
