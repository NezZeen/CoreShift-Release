part of '../app_state.dart';

/// A subscription running out, by date or by traffic.
class SubWarning {
  final Subscription sub;

  /// The traffic limit, else the date.
  final bool traffic;

  /// 1 soon, 2 within a day, 3 already over.
  final int level;
  final String title;
  final String body;

  const SubWarning(this.sub, {required this.traffic, required this.level, required this.title, required this.body});

  bool get over => level >= 3;

  /// Where to renew: the subscription's page, else the panel's support.
  String get renewUrl => sub.info.webPageUrl.isNotEmpty ? sub.info.webPageUrl : sub.info.supportUrl;

  String get _key => '${sub.id}/${traffic ? 'traffic' : 'expire'}';

  /// Changes when the subscription is renewed: a new date or a new limit.
  String get _term => traffic ? '${sub.info.total}' : '${sub.info.expire?.millisecondsSinceEpoch}';
}

/// Warns before a subscription runs out: a banner while it lasts, and once
/// per step (3 days, a day, over) a notification.
extension AppStateSubAlerts on AppState {
  /// Subscriptions running out, the most urgent first.
  List<SubWarning> get subscriptionWarnings {
    final now = DateTime.now();
    final out = <SubWarning>[];
    for (final s in subscriptions) {
      final i = s.info, name = '«${s.displayName}»';
      if (i.expire != null) {
        final left = i.expire!.difference(now);
        if (left.isNegative) {
          out.add(
            SubWarning(s, traffic: false, level: 3, title: 'Подписка $name закончилась', body: 'Продлите её у провайдера: без этого серверы не работают.'),
          );
        } else if (left.inHours < 24) {
          // Within a day: tonight or tomorrow morning.
          final e = i.expire!.toLocal(), day = e.year == now.year && e.month == now.month && e.day == now.day ? 'сегодня' : 'завтра';
          out.add(SubWarning(s, traffic: false, level: 2, title: 'Подписка $name закончится $day', body: 'Продлите её у провайдера, чтобы VPN не отключился.'));
        } else if (left.inHours < 72) {
          final days = subscriptionDaysLeft(i.expire!, now);
          out.add(
            SubWarning(
              s,
              traffic: false,
              level: 1,
              title: 'Подписка $name закончится через $days ${days == 1 ? 'день' : 'дня'}',
              body: 'Продлите её у провайдера заранее.',
            ),
          );
        }
      }
      if (i.total > 0) {
        if (i.used >= i.total) {
          out.add(
            SubWarning(
              s,
              traffic: true,
              level: 3,
              title: 'Трафик подписки $name закончился',
              body: 'Докупите трафик у провайдера или дождитесь его обновления.',
            ),
          );
        } else if (i.used / i.total >= .9) {
          out.add(
            SubWarning(
              s,
              traffic: true,
              level: 1,
              title: 'Трафик подписки $name почти израсходован',
              body: 'Осталось ${formatQuota(i.total - i.used)} из ${formatQuota(i.total)}.',
            ),
          );
        }
      }
    }
    out.sort((a, b) => b.level - a.level);
    return out;
  }

  /// Notifies of warnings that got worse since they were last shown; the
  /// steps already notified are kept with the user's preferences. Runs on
  /// every load of the subscriptions and once an hour.
  void checkSubscriptions() {
    final warned = Map<String, dynamic>.of(prefs['sub_warned'] is Map ? (prefs['sub_warned'] as Map).cast() : const {});
    final now = <String, String>{};
    var changed = false;
    for (final w in subscriptionWarnings) {
      final was = '${warned[w._key] ?? ''}'.split('|');
      final before = was.length == 2 && was[1] == w._term ? int.tryParse(was[0]) ?? 0 : 0;
      now[w._key] = '${max(before, w.level)}|${w._term}';
      if (w.level <= before) continue;
      changed = true;
      toast(w.title, w.over ? ToastKind.err : ToastKind.info);
      _alerts.add(Alert(w.title, w.body));
      platform.notify(w.title, w.body);
    }
    // Forget what no longer applies, e.g. a subscription renewed or removed.
    if (changed || now.length != warned.length) setPref('sub_warned', now);
  }
}
