import 'dart:io';

import 'package:coreshift/platform/linux_desktop.dart';
import 'package:flutter_test/flutter_test.dart';

// The Linux desktop pieces that need no Linux: they run on every host.
void main() {
  test('the autostart entry lives in XDG_CONFIG_HOME, else ~/.config', () {
    expect(autostartFile({'HOME': '/home/me'}).path, '/home/me/.config/autostart/dev.coreshift.coreshift.desktop');
    expect(autostartFile({'HOME': '/home/me', 'XDG_CONFIG_HOME': '/cfg'}).path, '/cfg/autostart/dev.coreshift.coreshift.desktop');
    expect(autostartFile({'HOME': '/home/me', 'XDG_CONFIG_HOME': ''}).path, '/home/me/.config/autostart/dev.coreshift.coreshift.desktop');
  });

  test('Exec arguments are quoted the way desktop entries want', () {
    expect(desktopExecArg('/opt/coreshift/coreshift'), '/opt/coreshift/coreshift');
    expect(desktopExecArg('/home/me/My Apps/coreshift'), '"/home/me/My Apps/coreshift"');
    expect(desktopExecArg(r'/a/$b"c`d'), r'"/a/\\$b\\"c\\`d"');
    expect(desktopExecArg(r'/a\b'), r'"/a\\\\b"');
    expect(desktopExecArg('/a/100%/x'), '"/a/100%%/x"');
  });

  test('the autostart entry starts CoreShift in the tray', () {
    final e = autostartEntry('/opt/coreshift/coreshift');
    expect(e, startsWith('[Desktop Entry]\n'));
    expect(e, contains('\nExec=/opt/coreshift/coreshift --tray\n'));
    expect(e, contains('\nType=Application\n'));
  });

  test('an AppImage starts from its own file', () {
    expect(appExecutable({'APPIMAGE': '/home/me/CoreShift.AppImage'}), '/home/me/CoreShift.AppImage');
    expect(appExecutable({}), Platform.resolvedExecutable);
  });

  test('autostart is written and removed', () {
    final dir = Directory.systemTemp.createTempSync('coreshift-autostart');
    addTearDown(() => dir.deleteSync(recursive: true));
    final env = {'HOME': dir.path, 'XDG_CONFIG_HOME': '${dir.path}/cfg'};
    final f = autostartFile(env);
    expect(setAutostart(true, env: env, exe: '/opt/coreshift/coreshift'), isTrue);
    expect(f.readAsStringSync(), contains('Exec=/opt/coreshift/coreshift --tray'));
    expect(setAutostart(false, env: env), isTrue);
    expect(f.existsSync(), isFalse);
    // Removing what is not there is no failure.
    expect(setAutostart(false, env: env), isTrue);
  });
}
