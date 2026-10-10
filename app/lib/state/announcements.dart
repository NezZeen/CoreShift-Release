import '../api/models.dart';
import 'app_state.dart';

/// A provider's announcement, as shown on the home page.
class Announcement {
  final Subscription sub;
  final String text;

  /// The link that goes with it; empty when the panel sent none.
  final String url;

  const Announcement(this.sub, this.text, this.url);
}

/// What the subscription providers announce (the `announce` header, see
/// engine/internal/subscription/announce.go). Hiding an announcement keeps
/// its text with the user's preferences, per subscription: the same text
/// does not come back, a new one does.
extension AppStateAnnouncements on AppState {
  static const _prefKey = 'announce_hidden';

  Map<String, String> get _hiddenAnnouncements {
    final raw = prefs[_prefKey];
    if (raw is! Map) return {};
    return {for (final e in raw.entries) '${e.key}': '${e.value}'};
  }

  /// Announcements not hidden yet, a subscription each.
  List<Announcement> get announcements {
    final hidden = _hiddenAnnouncements;
    return [
      for (final s in subscriptions)
        if (s.info.announce.trim().isNotEmpty && hidden[s.id] != s.info.announce) Announcement(s, s.info.announce, s.info.announceUrl),
    ];
  }

  void hideAnnouncement(Announcement a) {
    final hidden = _hiddenAnnouncements;
    // One text per subscription; forget the removed ones.
    hidden.removeWhere((id, _) => !subscriptions.any((s) => s.id == id));
    hidden[a.sub.id] = a.text;
    setPref(_prefKey, hidden);
  }
}
