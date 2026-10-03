// What the app does with the Linux desktop: the XDG autostart entry
// ("Автозапуск") and starting the systemd service, which the packages
// install and enable (packaging/linux).

import 'dart:io';

/// The desktop entry's id: the GTK application id of linux/CMakeLists.txt,
/// so that docks match the window to dev.coreshift.coreshift.desktop.
const desktopId = 'dev.coreshift.coreshift';

/// The systemd unit of the daemon.
const serviceUnit = 'coreshift.service';

/// The group whose members may read the daemon's api.json.
const apiGroup = 'coreshift';

/// What to tell a user who may not read the daemon's api.json.
const groupHint =
    'Нет доступа к службе CoreShift: ваш пользователь не в группе $apiGroup. '
    'Выполните в терминале «sudo usermod -aG $apiGroup \$USER», затем выйдите из системы и войдите снова.';

/// Where the autostart entry lives: $XDG_CONFIG_HOME/autostart, by default
/// ~/.config/autostart.
File autostartFile(Map<String, String> env) {
  final config = env['XDG_CONFIG_HOME'];
  final base = config != null && config.isNotEmpty ? config : '${env['HOME'] ?? '.'}/.config';
  return File('$base/autostart/$desktopId.desktop');
}

/// Quotes [arg] for the Exec key of a desktop entry: as is when it needs
/// nothing, else in double quotes with ", `, $ and \ escaped, and every
/// backslash doubled once more, as the key is itself an escaped string.
/// A % is doubled either way: alone it starts a field code.
String desktopExecArg(String arg) {
  if (RegExp(r'^[A-Za-z0-9_/.+,:@=-]+$').hasMatch(arg)) return arg;
  final quoted = arg.replaceAllMapped(RegExp(r'["`$\\]'), (m) => '\\${m[0]}');
  return '"${quoted.replaceAll(r'\', r'\\').replaceAll('%', '%%')}"';
}

/// The autostart entry: CoreShift in the tray when the user signs in.
String autostartEntry(String exe) =>
    '''[Desktop Entry]
Type=Application
Name=CoreShift
Comment=VPN-клиент CoreShift в трее
Exec=${desktopExecArg(exe)} --tray
Icon=coreshift
Terminal=false
NoDisplay=true
X-GNOME-Autostart-enabled=true
''';

/// The executable to start: an AppImage's own file rather than its mount,
/// which changes on every start.
String appExecutable(Map<String, String> env) {
  final appImage = env['APPIMAGE'];
  return appImage != null && appImage.isNotEmpty ? appImage : Platform.resolvedExecutable;
}

/// Makes the session start CoreShift, in the tray, at sign-in, or stops it
/// doing so. Returns whether that worked.
bool setAutostart(bool on, {Map<String, String>? env, String? exe}) {
  final e = env ?? Platform.environment;
  final f = autostartFile(e);
  try {
    if (!on) {
      if (f.existsSync()) f.deleteSync();
      return true;
    }
    f.parent.createSync(recursive: true);
    f.writeAsStringSync(autostartEntry(exe ?? appExecutable(e)));
    return true;
  } catch (_) {
    return false;
  }
}

/// Starts the CoreShift service, asking for the administrator's password
/// through polkit. Returns why it could not, or null once it started.
Future<String?> startService() async {
  try {
    final r = await Process.run('pkexec', ['systemctl', 'start', serviceUnit]);
    if (r.exitCode == 0) return null;
    // pkexec: 126, the dialog was dismissed; 127, not authorized.
    if (r.exitCode == 126 || r.exitCode == 127) return 'Запуск отменён';
    final err = '${r.stderr}'.trim();
    return err.isEmpty ? 'Не удалось запустить службу' : 'Не удалось запустить службу: $err';
  } on ProcessException {
    return 'Не найден pkexec. Запустите службу в терминале: sudo systemctl start coreshift';
  }
}
