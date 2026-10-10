part of '../app_state.dart';

/// Что видит пользователь: всплывающие сообщения, неудачные действия и открытие ссылок.
extension AppStateFeedback on AppState {
  /// Opens a link the panel sent (its support chat, the subscription's
  /// page). Only web, Telegram and mail links: another scheme could start
  /// a program. The link is not shown, it may carry the subscription's token.
  Future<void> openLink(String url) async {
    final u = Uri.tryParse(url.trim());
    final ok = u != null && (u.scheme == 'tg' || u.scheme == 'mailto' || ((u.scheme == 'https' || u.scheme == 'http') && u.host.isNotEmpty));
    if (!ok) {
      toast('Панель прислала ссылку, которую нельзя открыть', ToastKind.err);
      return;
    }
    if (linkOpener == null && backend is DemoBackend) {
      toast('В демо-режиме ссылки не открываются');
      return;
    }
    try {
      if (await (linkOpener ?? platform.openUrl)(u.toString())) return;
    } catch (_) {}
    toast('Не удалось открыть ссылку: нет приложения, которое её откроет', ToastKind.err);
  }

  // ---------------------------------------------------------------- toasts

  /// Shows [message] for a few seconds; one with an [action] («Отменить»)
  /// stays a little longer, to give time to press it.
  void toast(String message, [ToastKind kind = ToastKind.info, (String, VoidCallback)? action]) {
    // A failed connect reports its error twice: as the response and as an
    // event of the daemon.
    if (toasts.any((t) => t.message == message)) return;
    final t = Toast(++_toastSeq, message, kind, action: action);
    toasts.add(t);
    _notify();
    Timer(Duration(seconds: action == null ? 5 : 8), () {
      toasts.remove(t);
      _notify();
    });
  }

  void dismissToast(Toast t) {
    toasts.remove(t);
    _notify();
  }

  /// Runs an action, turning failures into an error toast. Returns whether
  /// it succeeded.
  Future<bool> _act(Future<void> Function() f) async {
    try {
      await f();
      return true;
    } catch (e) {
      toast(humanError('$e'), ToastKind.err);
      if (e is DaemonOffline) _lost(e);
      return false;
    }
  }
}
