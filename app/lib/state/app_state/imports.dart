part of '../app_state.dart';

/// Where a link to add came from.
enum ImportFrom {
  /// A panel's "add to app" button, or a link opened with CoreShift.
  link,
  clipboard,
  qr,
}

/// Subscriptions that come from outside the add dialog: links opened with
/// CoreShift, the clipboard, QR codes. Each is offered first ([pendingImport],
/// which the shell shows); nothing is added without the user.
extension AppStateImports on AppState {
  /// Offers to add what [text] links to. From the clipboard only links that
  /// look like subscriptions count, each offered once, and nothing is said
  /// about the rest; elsewhere a link that is no subscription says so.
  void offerImport(String text, ImportFrom from) {
    final quiet = from == ImportFrom.clipboard;
    final link = parseImportLink(text, strict: quiet);
    if (link == null) {
      if (!quiet) toast(from == ImportFrom.qr ? 'В QR-коде нет ссылки на подписку или сервер' : 'Это не ссылка на подписку или сервер', ToastKind.err);
      return;
    }
    if (link.error.isNotEmpty) {
      if (!quiet) toast(link.error, ToastKind.err);
      return;
    }
    if (link.url.isNotEmpty && subscriptions.any((s) => s.url == link.url || s.url == maskedUrl(link.url))) {
      if (!quiet) toast('Эта подписка уже добавлена');
      return;
    }
    if (quiet) {
      // Only a fingerprint: the link itself carries an access token.
      final fp = linkFingerprint(link.source);
      if (prefs['clipboard_offered'] == fp) return;
      setPref('clipboard_offered', fp);
    }
    pendingImport = link;
    importFrom = from;
    _notify();
  }

  /// Adds the offered subscription; returns the error, if any, for the
  /// window to show.
  Future<String?> acceptImport() async {
    final l = pendingImport;
    if (l == null) return null;
    final err = await addSubscription(source: l.url.isNotEmpty ? l.url : l.content, name: l.name);
    if (err == null) {
      pendingImport = null;
      _notify();
    }
    return err;
  }

  void dismissImport() {
    pendingImport = null;
    _notify();
  }

  /// "Ссылки из буфера обмена" in the settings, on unless turned off.
  bool get clipboardImport => prefs['clipboard_import'] != false;

  /// Offers a subscription link the user has just copied, as Happ does.
  /// Called when the app comes to the front.
  Future<void> checkClipboard() async {
    if (!loaded || !online || !clipboardImport || pendingImport != null || backend is DemoBackend) return;
    try {
      final text = (await Clipboard.getData(Clipboard.kTextPlain))?.text;
      if (text != null) offerImport(text, ImportFrom.clipboard);
    } catch (_) {
      // No access to the clipboard right now; next time.
    }
  }

  /// Scans a QR code with the phone's camera and offers what it links to.
  Future<void> scanQr() async {
    try {
      final text = await platform.scanQr();
      if (text != null) offerImport(text, ImportFrom.qr);
    } catch (e) {
      toast(platform.scanQrError(e), ToastKind.err);
    }
  }
}
