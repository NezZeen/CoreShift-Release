import 'dart:io';

import 'package:coreshift/api/models.dart';
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
    // Skipped by the session once the package is removed.
    expect(e, contains('\nTryExec=/opt/coreshift/coreshift\n'));
    expect(autostartEntry('/home/me/My Apps/coreshift'), contains('\nTryExec=/home/me/My Apps/coreshift\n'));
  });

  test('an AppImage starts from its own file', () {
    expect(appExecutable({'APPIMAGE': '/home/me/CoreShift.AppImage'}), '/home/me/CoreShift.AppImage');
    expect(appExecutable({}), Platform.resolvedExecutable);
  });

  test('a tray is there only when gdbus says yes', () {
    expect(parseNameHasOwner('(true,)\n'), isTrue);
    expect(parseNameHasOwner('(false,)\n'), isFalse);
    expect(parseNameHasOwner(''), isFalse);
  });

  test('an announced update may open only a GitHub release page', () {
    expect(isReleasePage('https://github.com/NezZeen/CoreShift-Release/releases/tag/v0.7.0'), isTrue);
    expect(isReleasePage('https://github.com/NezZeen/CoreShift-Release/releases'), isTrue);
    // The mirror on GitLab, where the service found the release when
    // GitHub was out of reach.
    expect(isReleasePage('https://gitlab.com/NezZeen/coreshift/-/releases/v0.9.0'), isTrue);
    expect(isReleasePage('https://gitlab.com/NezZeen/coreshift/-/releases'), isTrue);
    for (final bad in [
      'https://gitlab.com/NezZeen/coreshift/-/raw/main/x',
      'https://gitlab.com/NezZeen/coreshift/-/releases/v1?x=1',
      'https://gitlab.com.evil.example/o/r/-/releases/v1',
      'http://gitlab.com/o/r/-/releases/v1',
      'https://gitlab.com/o/r/-/releases/../../x',
      'http://github.com/o/r/releases/tag/v1',
      'https://github.com.evil.example/o/r/releases/tag/v1',
      'https://user@github.com/o/r/releases/tag/v1',
      'https://github.com/o/r/releases/../../x',
      'https://github.com/o/r/blob/main/x',
      'https://github.com/o/r/releases/tag/v1?x=1',
      'https://github.com:8443/o/r/releases',
      'file:///etc/passwd',
      '',
    ]) {
      expect(isReleasePage(bad), isFalse, reason: bad);
    }
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
