import 'models.dart';

/// The daemon as the UI talks to it: JSON requests plus the event stream.
/// [HttpBackend] talks to the real daemon; [DemoBackend] simulates one.
abstract class Backend {
  /// Sends a request and returns the decoded JSON response (null for none).
  /// Throws [ApiError] for error responses and [DaemonOffline] when the
  /// daemon cannot be reached.
  Future<dynamic> call(String method, String path, [Object? body]);

  /// Streams events, starting with the recent ones the daemon remembers.
  /// The stream ends or fails when the connection to the daemon is lost.
  Stream<Event> events();

  /// Where the backend connects, for the UI to show.
  String get description;

  /// The name [events] gives this window's stream at the service, for
  /// POST /v1/view; empty where there is none to tell.
  String get view;
}

class ApiError implements Exception {
  final int status;
  final String message;
  const ApiError(this.status, this.message);

  @override
  String toString() => message;
}

class DaemonOffline implements Exception {
  final String message;

  /// The user may not use the daemon (not in its group): starting it again
  /// would not help.
  final bool accessDenied;
  const DaemonOffline(this.message, {this.accessDenied = false});

  @override
  String toString() => message;
}
