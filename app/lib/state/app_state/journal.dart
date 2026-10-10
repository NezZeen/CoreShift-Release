part of '../app_state.dart';

/// Журнал: добавление строки с обрезкой (вывод ядер уходит первым) и очистка.
extension AppStateJournal on AppState {
  /// Adds a line to the journal, in the order of time rather than of
  /// arrival: the app's own first lines (the update notice) come before the
  /// service's replay of what happened earlier. A line already there, the
  /// same event replayed when the event stream reconnects, is not added
  /// twice. The output of cores and of the TUN layer comes in bursts, so it
  /// redraws only now and then.
  void _log(DateTime t, String source, String msg, LogLevel level, {bool output = false}) {
    // Nearly always the end: the walk back is over the few later lines.
    var i = logs.length;
    while (i > 0 && logs[i - 1].time.isAfter(t)) {
      i--;
    }
    for (var j = i - 1; j >= 0 && logs[j].time.isAtSameMomentAs(t); j--) {
      if (logs[j].source == source && logs[j].message == msg) return;
    }
    final line = LogLine(t, source, msg, level, output: output);
    logs.insert(i, line);
    // The cores' plain output (a verbose journal: hundreds of lines a
    // minute, which the page hides) goes first when the journal is full,
    // so it cannot push the connection's own lines out.
    if (AppState._chatter(line)) _chatterLines++;
    if (_chatterLines > AppState.chatterKeep) {
      final j = logs.indexWhere(AppState._chatter);
      if (j >= 0) {
        logs.removeAt(j);
        _chatterLines--;
      }
    }
    if (logs.length > AppState.logsKeep) {
      final gone = logs.length - AppState.logsKeep;
      _chatterLines -= logs.take(gone).where(AppState._chatter).length;
      logs.removeRange(0, gone);
    }
    logsRevision++;
    if (!output || logsRevision % 20 == 0) _notify();
  }

  void clearLogs() {
    logs.clear();
    _chatterLines = 0;
    logsRevision++;
    _notify();
  }
}
