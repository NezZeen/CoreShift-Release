import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'linux_desktop.dart' as linux;

/// Whether this system can have its proxy set by CoreShift: Windows and
/// Linux. Android apps cannot set the proxy of the phone.
bool get systemProxySupported => Platform.isWindows || Platform.isLinux;

/// coreshiftd, which sets the proxy as the user running the app: beside
/// the app on Windows, where the packages put it on Linux, or on the PATH
/// (development runs).
String? _daemon() {
  final name = Platform.isWindows ? 'coreshiftd.exe' : 'coreshiftd';
  final here = File(Platform.resolvedExecutable).parent;
  final candidates = [
    '${here.path}${Platform.pathSeparator}$name',
    if (Platform.isLinux) ...['${here.parent.parent.path}/bin/$name', ...linux.daemonPaths],
    for (final dir in (Platform.environment['PATH'] ?? '').split(Platform.isWindows ? ';' : ':'))
      if (dir.isNotEmpty) '$dir${Platform.pathSeparator}$name',
  ];
  return candidates.where((p) => File(p).existsSync()).firstOrNull;
}

/// Runs `coreshiftd sysproxy <args>` and returns its report (the JSON line
/// it prints). On Windows it starts without a console of its own, which a
/// console program started by a window would flash.
Future<Map<String, dynamic>> runSystemProxy(List<String> args) async {
  final exe = _daemon();
  if (exe == null) {
    return {
      'action': args.first,
      'errors': ['не найден coreshiftd'],
    };
  }
  try {
    final String out;
    if (Platform.isWindows) {
      final p = await Process.start(exe, ['sysproxy', ...args], mode: ProcessStartMode.detachedWithStdio);
      p.stderr.drain<void>().ignore();
      out = await p.stdout.transform(utf8.decoder).join().timeout(const Duration(seconds: 30));
    } else {
      final r = await Process.run(exe, ['sysproxy', ...args]).timeout(const Duration(seconds: 30));
      out = '${r.stdout}';
    }
    final line = const LineSplitter().convert(out).lastWhere((l) => l.trim().startsWith('{'), orElse: () => '');
    if (line.isEmpty) {
      return {
        'action': args.first,
        'errors': ['coreshiftd ничего не ответил'],
      };
    }
    return (jsonDecode(line) as Map).cast<String, dynamic>();
  } catch (e) {
    return {
      'action': args.first,
      'errors': ['$e'],
    };
  }
}

/// Whether a journal of settings to put back is there (where the engine's
/// sysproxy package keeps it), so the app can tidy up after a crash.
bool systemProxyJournalExists() {
  final env = Platform.environment;
  String path;
  if (Platform.isWindows) {
    final local = env['LOCALAPPDATA'];
    if (local == null || local.isEmpty) return false;
    path = '$local\\CoreShift\\sysproxy.json';
  } else if (Platform.isLinux) {
    final state = env['XDG_STATE_HOME'];
    final home = env['HOME'] ?? '';
    path = '${state != null && state.isNotEmpty ? state : '$home/.local/state'}/coreshift/sysproxy.json';
  } else {
    return false;
  }
  try {
    return File(path).existsSync();
  } catch (_) {
    return false;
  }
}
