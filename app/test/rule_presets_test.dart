import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/punycode.dart';
import 'package:coreshift/ui/pages/rule_lists.dart';

void main() {
  // The service keeps names in other scripts in punycode; the rule lists
  // show them as typed, the demo stores them as the service does.
  test('punycode of domain names', () {
    const cases = {
      'кремль.рф': 'xn--e1ajeds9e.xn--p1ai',
      'госуслуги.рф': 'xn--c1aapkosapc.xn--p1ai',
      'пример.испытание': 'xn--e1afmkfd.xn--80akhbyknj4f',
      'bücher.example': 'xn--bcher-kva.example',
      'example.com': 'example.com',
    };
    cases.forEach((unicode, ascii) {
      expect(domainToAscii(unicode), ascii, reason: unicode);
      expect(domainToUnicode(ascii), unicode, reason: ascii);
    });
    expect(domainToAscii('Кремль.РФ'), 'xn--e1ajeds9e.xn--p1ai');
    // Not punycode after all: left alone.
    expect(domainToUnicode('xn--!!.example'), 'xn--!!.example');
  });

  // Linux matches programs by path, …/Discord: "Discord.exe" in the list
  // never matched, so Discord's voice went around the VPN there.
  test('preset programs are named as each system names them', () {
    final discord = servicePresets.firstWhere((p) => p.name == 'Discord').apps;
    expect(presetApps(discord, linux: false, android: false), ['Discord.exe']);
    expect(presetApps(discord, linux: true, android: false), ['Discord']);
    expect(presetApps(discord, linux: false, android: true), isEmpty);
  });
}
