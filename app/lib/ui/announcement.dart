import 'package:flutter/material.dart';

import '../state/announcements.dart';
import '../state/app_state.dart';
import 'theme.dart';
import 'widgets.dart';

/// What a subscription's provider announces, above the rest of the home page:
/// the subscription's name, the text, the link («Подробнее») if the panel sent
/// one, and «Скрыть». The text hidden does not come back, a new one does
/// ([AppStateAnnouncements.hideAnnouncement]).
class AnnouncementCard extends StatefulWidget {
  final AppState state;
  final Announcement announcement;
  const AnnouncementCard({super.key, required this.state, required this.announcement});

  @override
  State<AnnouncementCard> createState() => _AnnouncementCardState();
}

class _AnnouncementCardState extends State<AnnouncementCard> {
  /// A long text is cut to a few lines until asked for whole.
  static const _lines = 4;
  bool _whole = false;

  @override
  void didUpdateWidget(AnnouncementCard old) {
    super.didUpdateWidget(old);
    if (old.announcement.text != widget.announcement.text) _whole = false;
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final a = widget.announcement;
    final compact = isCompact(context);
    const color = accent;
    final style = TextStyle(fontSize: 13, height: 1.35, color: p.text);
    return Container(
      clipBehavior: Clip.antiAlias,
      decoration: BoxDecoration(color: color.withValues(alpha: .09), borderRadius: BorderRadius.circular(10)),
      child: Stack(
        children: [
          Positioned(left: 0, top: 0, bottom: 0, width: 3, child: ColoredBox(color: color)),
          Padding(
            padding: EdgeInsets.fromLTRB(15, 10, 12, compact ? 10 : 12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Icon(Icons.campaign_outlined, size: 17, color: p.ink(color)),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        'Объявление от «${a.sub.displayName}»',
                        style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 6),
                LayoutBuilder(
                  builder: (context, c) {
                    final cut =
                        !_whole &&
                        (TextPainter(
                          text: TextSpan(text: a.text, style: style),
                          maxLines: _lines,
                          textDirection: TextDirection.ltr,
                        )..layout(maxWidth: c.maxWidth)).didExceedMaxLines;
                    final long = cut || _whole;
                    return Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(a.text, style: style, maxLines: _whole ? null : _lines, overflow: _whole ? TextOverflow.clip : TextOverflow.ellipsis),
                        if (long)
                          Padding(
                            padding: const EdgeInsets.only(top: 4),
                            child: GestureDetector(
                              onTap: () => setState(() => _whole = !_whole),
                              child: MouseRegion(
                                cursor: SystemMouseCursors.click,
                                child: Text(_whole ? 'Свернуть' : 'Показать полностью', style: TextStyle(fontSize: 12, color: p.ink(color))),
                              ),
                            ),
                          ),
                      ],
                    );
                  },
                ),
                const SizedBox(height: 10),
                Wrap(
                  spacing: 8,
                  runSpacing: 6,
                  children: [
                    if (a.url.isNotEmpty) Btn(label: 'Подробнее', small: true, kind: BtnKind.primary, onPressed: () => widget.state.openLink(a.url)),
                    Btn(label: 'Скрыть', small: true, onPressed: () => widget.state.hideAnnouncement(a)),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// The announcements not hidden yet, a card each, with room below.
class AnnouncementCards extends StatelessWidget {
  final AppState state;
  final double gap;
  const AnnouncementCards({super.key, required this.state, this.gap = 14});

  @override
  Widget build(BuildContext context) {
    final all = state.announcements;
    if (all.isEmpty) return const SizedBox();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (final a in all) ...[AnnouncementCard(key: ValueKey('announce/${a.sub.id}'), state: state, announcement: a), SizedBox(height: gap)],
      ],
    );
  }
}

/// «Сайт»: the subscription's page (the panel's profile-web-page-url), a
/// small pill beside «Поддержка». On a phone the word is left out.
class SiteButton extends StatelessWidget {
  final AppState state;
  final String url;
  final bool iconOnly;
  const SiteButton({super.key, required this.state, required this.url, this.iconOnly = false});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Tooltip(
      message: 'Открыть страницу подписки',
      child: Material(
        color: p.surface2,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(99),
          side: BorderSide(color: p.border2),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: () => state.openLink(url),
          hoverColor: p.surface3,
          child: Padding(
            padding: EdgeInsets.fromLTRB(iconOnly ? 6 : 8, 4, iconOnly ? 6 : 10, 4),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(Icons.language, size: 18, color: p.muted),
                if (!iconOnly) ...[
                  const SizedBox(width: 6),
                  Text(
                    'Сайт',
                    style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: p.text),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }
}
