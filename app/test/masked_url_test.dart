import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/models.dart';

void main() {
  // The same cases as TestMaskURL in engine/internal/service/mask_test.go:
  // a link pasted again must look like the one the daemon lists.
  test('maskedUrl matches the daemon', () {
    const cases = {
      '': '',
      'https://Panel.Example.com/sub/AbCdEf1234567890': 'https://panel.example.com/…7890',
      'https://panel.example.com:8443/api/v1/client/subscribe?token=SECRETSECRET': 'https://panel.example.com:8443/…CRET',
      'https://user:pw@panel.example.com/sub/AbCdEf1234567890': 'https://panel.example.com/…7890',
      'http://panel.example.com/s/abc': 'http://panel.example.com/…',
      'https://panel.example.com': 'https://panel.example.com',
      'https://panel.example.com/': 'https://panel.example.com',
      'not a url': '…',
    };
    cases.forEach((input, want) => expect(maskedUrl(input), want, reason: input));
    // Already masked: as it is.
    expect(maskedUrl('https://panel.example.com/…7890'), 'https://panel.example.com/…7890');
  });

  test('a masked link still names its panel', () {
    final sub = Subscription.fromJson({'id': 'a', 'url': 'https://panel.example.com/…7890', 'insecure': true});
    expect(sub.host, 'panel.example.com');
    expect(sub.isLocal, isFalse);
    expect(sub.insecure, isTrue);
  });
}
