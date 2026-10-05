import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/state/app_state.dart';

/// A panel's traffic limit reads as the panel shows it (panels count a
/// gigabyte as 1024³ bytes), and a subscription ending within a day says
/// whether that is today or tomorrow.
void main() {
  const gib = 1024 * 1024 * 1024;

  test('panel quotas in the panel\'s gigabytes', () {
    expect(formatQuota(100 * gib), '100 ГБ');
    expect(formatQuota(50 * gib), '50 ГБ');
    expect(formatQuota(5 * gib + gib ~/ 2), '5.5 ГБ');
    expect(formatQuota(1024 * gib), '1 ТБ');
    expect(formatQuota(300 * 1024 * 1024), '300 МБ');
    expect(formatQuota(0), '0 КБ');
  });

  Subscription sub({int used = 0, int total = 0, DateTime? expire}) => Subscription.fromJson({
    'id': 's1',
    'display_name': 'Тест',
    'url': 'https://panel.example/sub/…abcd',
    'info': {'download': used, 'total': total, if (expire != null) 'expire': expire.toUtc().toIso8601String()},
  });

  test('traffic warning: «5 ГБ из 100 ГБ», as the panel says', () {
    final state = AppState(DemoBackend())..subscriptions = [sub(used: 95 * gib, total: 100 * gib)];
    final w = state.subscriptionWarnings.single;
    expect(w.body, 'Осталось 5 ГБ из 100 ГБ.');
  });

  test('ending within a day: today or tomorrow', () {
    final now = DateTime.now();
    final midnight = DateTime(now.year, now.month, now.day + 1);
    final state = AppState(DemoBackend());

    // Tomorrow at 00:30, less than a day away.
    state.subscriptions = [sub(expire: midnight.add(const Duration(minutes: 30)))];
    final tomorrow = state.subscriptionWarnings;
    if (midnight.add(const Duration(minutes: 30)).difference(now).inHours < 24) {
      expect(tomorrow.single.title, 'Подписка «Тест» закончится завтра');
    }

    // Today, a minute before midnight (or right now plus a minute).
    final today = now.add(const Duration(minutes: 1)).isBefore(midnight) ? midnight.subtract(const Duration(minutes: 1)) : null;
    if (today != null && today.isAfter(now)) {
      state.subscriptions = [sub(expire: today)];
      expect(state.subscriptionWarnings.single.title, 'Подписка «Тест» закончится сегодня');
    }
  });
}
