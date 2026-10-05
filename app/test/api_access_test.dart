import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/platform/api_access.dart';
import 'package:coreshift/platform/linux_desktop.dart' as linux;
import 'package:coreshift/platform/platform_io.dart';

void main() {
  test('names the account the way net localgroup takes it', () {
    expect(windowsAccount({'USERNAME': 'nik', 'USERDOMAIN': 'DESKTOP-1', 'COMPUTERNAME': 'DESKTOP-1'}), 'nik');
    expect(windowsAccount({'USERNAME': 'nik', 'USERDOMAIN': 'desktop-1', 'COMPUTERNAME': 'DESKTOP-1'}), 'nik');
    expect(windowsAccount({'USERNAME': 'nik', 'USERDOMAIN': 'CORP', 'COMPUTERNAME': 'DESKTOP-1'}), r'CORP\nik');
    expect(windowsAccount({}), 'ИМЯ');
  });

  test('tells a Windows user outside the group what to do', () {
    final hint = windowsGroupHint('nik');
    expect(hint, startsWith(accessDeniedPrefix));
    expect(hint, contains('учётная запись nik не входит в группу «CoreShift Users»'));
    expect(hint, contains('net localgroup "CoreShift Users" "nik" /add'));
    expect(hint, contains('выходить из системы не нужно'));
  });

  test('access refused is told apart from a service that is down', () {
    // Both messages start alike, so the start button hides for either.
    expect(linux.groupHint, startsWith(accessDeniedPrefix));
    final desktop = Platform.isWindows || Platform.isLinux;
    expect(daemonAccessDenied(windowsGroupHint('nik')), desktop);
    expect(daemonAccessDenied(linux.groupHint), desktop);
    expect(daemonAccessDenied('Служба CoreShift не запущена'), isFalse);
    expect(daemonAccessDenied('Служба CoreShift не отвечает'), isFalse);
  });
}
