import 'package:coreshift/state/import_link.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  const sub = 'https://sub.example.com/api/sub/AbC123?token=x&b=1';

  test('panel buttons of the common apps', () {
    final cases = {
      'coreshift://add/$sub': sub,
      'coreshift://import/$sub#Мой%20VPN': sub,
      'happ://add/$sub': sub,
      'v2raytun://import/$sub': sub,
      'hiddify://import/$sub#Home': sub,
      'streisand://import/$sub': sub,
      'v2rayng://install-config?url=${Uri.encodeComponent(sub)}&name=Home': sub,
      'v2rayng://install-sub?url=${Uri.encodeComponent(sub)}': sub,
      'sing-box://import-remote-profile?url=${Uri.encodeComponent(sub)}#Home': sub,
      'clash://install-config?url=${Uri.encodeComponent(sub)}': sub,
      'karing://install-config?url=${Uri.encodeComponent(sub)}&name=Home': sub,
      'happ://add/${Uri.encodeComponent(sub)}': sub,
    };
    cases.forEach((link, want) {
      final l = parseImportLink(link);
      expect(l?.url, want, reason: link);
      expect(l?.error, isEmpty, reason: link);
    });
    expect(parseImportLink('hiddify://import/$sub#Home')!.name, 'Home');
    // Names are not always encoded.
    expect(parseImportLink('hiddify://import/$sub#Мой провайдер')!.name, 'Мой провайдер');
    expect(parseImportLink('coreshift://import/$sub#Мой%20VPN')!.name, 'Мой VPN');
    expect(parseImportLink('v2rayng://install-config?url=${Uri.encodeComponent(sub)}&name=Home')!.name, 'Home');
  });

  test('server links, alone, in lines or behind a button', () {
    const a = 'vless://uuid@1.2.3.4:443?security=reality#A', b = 'hy2://pw@5.6.7.8:443#B';
    expect(parseImportLink(a)!.content, a);
    expect(parseImportLink('$a\n$b\n')!.content, '$a\n$b');
    expect(parseImportLink('coreshift://add/$a')!.content, a);
    expect(parseImportLink('$a\nhello'), isNull);
    expect(parseImportLink('vless://uuid@1.2.3.4:443#Мой сервер')!.content, 'vless://uuid@1.2.3.4:443#Мой сервер');
    expect(parseImportLink('see https://sub.example.com/abc'), isNull);
    expect(parseImportLink('https://sub.example.com/a b'), isNull);
  });

  test('what is not a subscription', () {
    for (final s in ['', 'hello', 'ftp://x/y', 'unknown://add/$sub', 'happ://add/', 'https://', 'mailto:me@example.com']) {
      expect(parseImportLink(s), isNull, reason: s);
    }
    expect(parseImportLink('happ://crypt3/AbCdEf')!.error, isNotEmpty);
  });

  test('the clipboard takes only links that look like subscriptions', () {
    for (final s in [
      'https://sub.example.com/abc',
      'https://panel.example.org:2096/sub/7f3c9a1e',
      'https://example.com/api/v1/client/subscribe?token=abc',
      'https://example.com/link/abcdef?sub=3',
      'https://vpn.example.net/AbCdEf1234567890',
    ]) {
      expect(parseImportLink(s, strict: true)?.url, s, reason: s);
    }
    for (final s in ['https://www.youtube.com/watch?v=dQw4w9WgXcQ', 'https://example.com/news/today', 'https://github.com/NezZeen']) {
      expect(parseImportLink(s, strict: true), isNull, reason: s);
      expect(parseImportLink(s)?.url, s, reason: 'not strict: $s');
    }
  });

  test('fingerprints tell links apart without keeping them', () {
    expect(linkFingerprint(sub), linkFingerprint(sub));
    expect(linkFingerprint(sub), isNot(linkFingerprint('${sub}2')));
    expect(linkFingerprint(sub), isNot(contains('example')));
  });
}
