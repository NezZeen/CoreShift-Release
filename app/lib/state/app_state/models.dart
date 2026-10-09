part of '../app_state.dart';

enum LogLevel { info, ok, warn, err, swap }

class LogLine {
  final DateTime time;
  final String source;
  final String message;
  final LogLevel level;

  /// A line a core or the TUN layer printed itself, as opposed to an event
  /// the app put into words.
  final bool output;
  const LogLine(this.time, this.source, this.message, this.level, {this.output = false});
}

enum ToastKind { info, ok, err, swap }

class Toast {
  final int id;
  final String message;
  final ToastKind kind;

  /// A button on the toast, such as «Отменить»: its label and what it does.
  final (String, VoidCallback)? action;
  const Toast(this.id, this.message, this.kind, {this.action});
}

/// Something worth a system notification when the window is out of sight.
class Alert {
  final String title;
  final String body;
  const Alert(this.title, this.body);
}
