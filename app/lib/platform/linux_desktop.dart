// What the app does with the Linux desktop: the XDG autostart entry
// ("Автозапуск") and starting the systemd service, which the packages
// install and enable (packaging/linux).

import 'dart:io';

/// The desktop entry's id: the GTK application id of linux/CMakeLists.txt,
/// so that docks match the window to dev.coreshift.coreshift.desktop.
const desktopId = 'dev.coreshift.coreshift';

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
///
/// TryExec makes the session skip it once CoreShift is removed: a package
/// cannot delete files in the users' homes, so the entry outlives it.
String autostartEntry(String exe) =>
    '''[Desktop Entry]
Type=Application
Name=CoreShift
Comment=VPN-клиент CoreShift в трее
TryExec=${exe.replaceAll(r'\', r'\\')}
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

/// Where the packages put the daemon: /usr/bin from the .deb, .rpm and
/// Arch packages, /usr/local/bin from install.sh.
const daemonPaths = ['/usr/bin/coreshiftd', '/usr/local/bin/coreshiftd'];

/// Whether gdbus's answer to NameHasOwner is yes: "(true,)".
bool parseNameHasOwner(String out) => out.trim().startsWith('(true');

/// Whether the session has a tray (a StatusNotifierWatcher on the session
/// bus): KDE, XFCE, Cinnamon, MATE and Ubuntu's GNOME have one; plain
/// GNOME without the AppIndicator extension, and WSLg, do not. Without a
/// tray the window must never hide: nothing could bring it back.
Future<bool> trayAvailable() async {
  try {
    final r = await Process.run('gdbus', [
      'call',
      '--session',
      '--dest',
      'org.freedesktop.DBus',
      '--object-path',
      '/org/freedesktop/DBus',
      '--method',
      'org.freedesktop.DBus.NameHasOwner',
      'org.kde.StatusNotifierWatcher',
    ]).timeout(const Duration(seconds: 3));
    return r.exitCode == 0 && parseNameHasOwner('${r.stdout}');
  } catch (_) {
    // No gdbus or no session bus: assume none, the safe way.
    return false;
  }
}

/// Starts the CoreShift service, asking for the administrator's password
/// through polkit; the daemon asks whichever init system runs (systemd,
/// OpenRC, runit). Returns why it could not, or null once it started.
Future<String?> startService() async {
  final daemon = daemonPaths.where((p) => File(p).existsSync()).firstOrNull;
  if (daemon == null) return 'CoreShift не установлен: нет $daemonPaths';
  try {
    final r = await Process.run('pkexec', [daemon, 'service', 'start']);
    if (r.exitCode == 0) return null;
    // pkexec: 126, the dialog was dismissed; 127, not authorized.
    if (r.exitCode == 126 || r.exitCode == 127) return 'Запуск отменён';
    final err = '${r.stderr}'.trim();
    return err.isEmpty ? 'Не удалось запустить службу' : 'Не удалось запустить службу: $err';
  } on ProcessException {
    return 'Не найден pkexec. Запустите службу в терминале: sudo coreshiftd service start';
  }
}
