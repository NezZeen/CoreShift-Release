import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/state/redact.dart';

void main() {
  test('sites, links, user IDs and public addresses are hidden', () {
    final cases = {
      'ERROR [74448989 5.3s] connection: open connection to www.example.org:443 using outbound/socks[proxy]: i/o timeout':
          'ERROR [74448989 5.3s] connection: open connection to <домен>:443 using outbound/socks[proxy]: i/o timeout',
      'dns: lookup failed for tracker.example.net: context deadline exceeded': 'dns: lookup failed for <домен>: context deadline exceeded',
      'fetch subscription: dial tcp: lookup panel.example.com on 127.0.0.53:53: no such host':
          'fetch subscription: dial tcp: lookup <домен> on 127.0.0.53:53: no such host',
      'proxy/vless/outbound: failed to dial to tcp:203.0.113.5:443 > i/o timeout': 'proxy/vless/outbound: failed to dial to tcp:203.0.x.x:443 > i/o timeout',
      'invalid user 11111111-2222-3333-4444-555555555555': 'invalid user <uuid>',
      'GET https://sub.example.com/api/sub/AbCdEf123456789?flag=1 failed': 'GET https://… failed',
      'dial [2001:db8:85a3::8a2e:370:7334]:443: refused': 'dial [2001:db8:…]:443: refused',
    };
    cases.forEach((input, want) => expect(redactForSupport(input), want, reason: input));
  });

  test('what explains a failure stays', () {
    for (final line in [
      '2026/10/03 12:26:51.000000 [Warning] transport/internet/splithttp: slow response',
      'inbound/tun[tun-in]: connection from 172.19.0.1:50312 to 198.18.0.7:443',
      'listen tcp 127.0.0.1:17890: bind: address already in use',
      'dial [fdfe:dcba:9876::2]:53 and [::1]:53 and fe80::1',
      'panic: runtime error\n\t/build/sing-box/route/conn.go:123 +0x1f',
      'Xray 26.3.27 (go1.26.1 windows/amd64), router 192.168.1.1, 10.8.0.1, 100.64.0.1',
    ]) {
      expect(redactForSupport(line), line);
    }
  });

  test('the copied journal is redacted, the page is not', () {
    final state = AppState(DemoBackend());
    addTearDown(state.dispose);
    final t0 = DateTime(2026, 10, 5, 10);
    state.injectEvent(Event(time: t0, kind: 'state', state: 'connected', core: 'xray'));
    state.injectEvent(Event(time: t0, kind: 'log', source: 'tun', line: 'ERROR connection: open connection to secret-site.example:443: refused'));
    final copy = state.journalForSupport().join('\n');
    expect(copy, contains('<домен>:443'));
    expect(copy, isNot(contains('secret-site')));
    expect(state.logs.any((l) => l.message.contains('secret-site')), isTrue);
  });
}
