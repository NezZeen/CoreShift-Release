import 'package:flutter/material.dart';

import '../l10n/strings.dart';
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
                        tr('announce.title', {'name': a.sub.displayName}),
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
                                child: Text(_whole ? tr('announce.collapse') : tr('announce.expand'), style: TextStyle(fontSize: 12, color: p.ink(color))),
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
                    if (a.url.isNotEmpty) Btn(label: tr('announce.details'), small: true, kind: BtnKind.primary, onPressed: () => widget.state.openLink(a.url)),
                    Btn(label: tr('announce.hide'), small: true, onPressed: () => widget.state.hideAnnouncement(a)),
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

/// The phone's announcements: a line each above the button, the
/// subscription's name and the start of the text; a tap opens the whole
/// text in a sheet with «Подробнее» and «Скрыть». So the button and the
/// server stay on the first screen.
class AnnouncementStrips extends StatelessWidget {
  final AppState state;
  const AnnouncementStrips({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    final all = state.announcements;
    if (all.isEmpty) return const SizedBox();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (final a in all) ...[_AnnouncementStrip(key: ValueKey('announce-strip/${a.sub.id}'), state: state, announcement: a), const SizedBox(height: 12)],
      ],
    );
  }
}

class _AnnouncementStrip extends StatelessWidget {
  final AppState state;
  final Announcement announcement;
  const _AnnouncementStrip({super.key, required this.state, required this.announcement});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final a = announcement;
    return Material(
      color: Color.alphaBlend(accent.withValues(alpha: .10), p.surface),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(14),
        side: BorderSide(color: accent.withValues(alpha: .35)),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () => _open(context),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(12, 9, 8, 9),
          child: Row(
            children: [
              Icon(Icons.campaign_outlined, size: 20, color: p.accentInk),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      a.sub.displayName,
                      style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w700),
                      overflow: TextOverflow.ellipsis,
                    ),
                    Text(
                      a.text.replaceAll('\n', ' '),
                      style: TextStyle(fontSize: 12, color: p.muted),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ],
                ),
              ),
              Icon(Icons.expand_less, size: 22, color: p.muted),
            ],
          ),
        ),
      ),
    );
  }

  void _open(BuildContext context) {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (context) {
        final p = context.pal;
        final a = announcement;
        return SafeArea(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(22, 0, 22, 20),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  children: [
                    Container(
                      width: 40,
                      height: 40,
                      decoration: BoxDecoration(color: accent.withValues(alpha: .14), borderRadius: BorderRadius.circular(11)),
                      child: Icon(Icons.campaign_outlined, size: 22, color: p.accentInk),
                    ),
                    const SizedBox(width: 12),
                    Expanded(child: Text(tr('announce.title', {'name': a.sub.displayName}), style: dialogTitle)),
                  ],
                ),
                const SizedBox(height: 14),
                Text(a.text, style: TextStyle(fontSize: 15, height: 1.5, color: p.text)),
                const SizedBox(height: 18),
                Row(
                  children: [
                    if (a.url.isNotEmpty) ...[
                      Expanded(
                        child: Btn(
                          label: tr('announce.details'),
                          kind: BtnKind.primary,
                          onPressed: () {
                            Navigator.of(context).pop();
                            state.openLink(a.url);
                          },
                        ),
                      ),
                      const SizedBox(width: 10),
                    ],
                    Expanded(
                      child: Btn(
                        label: tr('announce.hide'),
                        onPressed: () {
                          Navigator.of(context).pop();
                          state.hideAnnouncement(a);
                        },
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        );
      },
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
      message: tr('announce.site.tooltip'),
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
                    tr('announce.site'),
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
