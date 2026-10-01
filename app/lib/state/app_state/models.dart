part of '../app_state.dart';

enum LogLevel { info, ok, warn, err, swap }

class LogLine {
  final DateTime time;
  final String source;
  final String message;
  final LogLevel level;
  const LogLine(this.time, this.source, this.message, this.level);
}

enum ToastKind { info, ok, err, swap }

class Toast {
  final int id;
  final String message;
  final ToastKind kind;
  const Toast(this.id, this.message, this.kind);
}

/// Something worth a system notification when the window is out of sight.
class Alert {
  final String title;
  final String body;
  const Alert(this.title, this.body);
}
