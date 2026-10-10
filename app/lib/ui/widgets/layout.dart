part of '../widgets.dart';

/// A bordered surface, the prototype's `.card`.
class Panel extends StatelessWidget {
  final Widget child;
  final EdgeInsets? padding;
  final Color? borderColor;
  final VoidCallback? onTap;
  final bool dashed;
  final Color? color;

  const Panel({super.key, required this.child, this.padding, this.borderColor, this.onTap, this.dashed = false, this.color});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final box = Container(
      padding: padding ?? EdgeInsets.all(isCompact(context) ? 14 : 20),
      decoration: BoxDecoration(
        color: dashed ? Colors.transparent : Color.alphaBlend(color ?? Colors.transparent, p.surface),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: borderColor ?? p.border, width: borderColor == null ? 1 : 1.5),
      ),
      child: child,
    );
    if (onTap == null) return box;
    return MouseRegion(
      cursor: SystemMouseCursors.click,
      child: GestureDetector(onTap: onTap, child: box),
    );
  }
}

/// The scrolling, width-limited area every page lives in.
class PageFrame extends StatelessWidget {
  final List<Widget> children;
  const PageFrame({super.key, required this.children});

  @override
  Widget build(BuildContext context) {
    return Scrollbar(
      child: SingleChildScrollView(
        padding: isCompact(context) ? const EdgeInsets.fromLTRB(16, 16, 16, 28) : const EdgeInsets.fromLTRB(32, 28, 32, 40),
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 1180),
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: children),
          ),
        ),
      ),
    );
  }
}

/// A phone's screen, or a window as narrow: pages switch with a bottom bar
/// instead of the sidebar, and wide tables become lists.
bool isCompact(BuildContext context) => MediaQuery.sizeOf(context).width < 720;

/// A panel's heading. On a phone the [sub] note is left out and [info], the
/// explanation a desktop page shows under the heading, hides behind an icon.
class PanelTitle extends StatelessWidget {
  final String title;
  final String? sub;
  final Widget? trailing;
  final String? info;
  const PanelTitle(this.title, {super.key, this.sub, this.trailing, this.info});

  @override
  Widget build(BuildContext context) {
    final compact = isCompact(context);
    if (trailing != null && compact) {
      // On a phone the control gets a line of its own.
      return Padding(
        padding: const EdgeInsets.only(bottom: 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            PanelTitle(title, info: info),
            SingleChildScrollView(scrollDirection: Axis.horizontal, child: trailing),
          ],
        ),
      );
    }
    return Padding(
      padding: EdgeInsets.only(bottom: compact ? 12 : 14),
      child: Row(
        children: [
          Expanded(
            child: sub != null && !compact
                // The title first, the subtitle in what is left: sharing the
                // width, a two-column page at 1280 px cut both short,
                // «Сайты и адреса без …».
                ? LayoutBuilder(
                    builder: (context, c) => Row(
                      children: [
                        ConstrainedBox(
                          constraints: BoxConstraints(maxWidth: c.maxWidth),
                          child: Text(title, style: display(16), overflow: TextOverflow.ellipsis),
                        ),
                        Expanded(
                          child: Padding(
                            padding: const EdgeInsets.only(left: 10),
                            child: Text(
                              sub!,
                              style: TextStyle(color: context.pal.muted, fontSize: 12),
                              overflow: TextOverflow.ellipsis,
                              maxLines: 1,
                            ),
                          ),
                        ),
                      ],
                    ),
                  )
                : Row(
                    children: [
                      Flexible(
                        child: Text(title, style: display(16), overflow: TextOverflow.ellipsis),
                      ),
                      if (compact && info != null) InfoIcon(title: title, text: info!),
                    ],
                  ),
          ),
          ?trailing,
        ],
      ),
    );
  }
}

/// A page's title. On a phone the subtitle is left out: the bottom bar and
/// the page itself say enough.
class PageHeader extends StatelessWidget {
  final String title;
  final String? subtitle;
  final List<Widget> actions;

  /// A page opened from another one: the way back, named.
  final (String, VoidCallback)? back;
  const PageHeader(this.title, {super.key, this.subtitle, this.actions = const [], this.back});

  @override
  Widget build(BuildContext context) {
    final compact = isCompact(context);
    final p = context.pal;
    // Wraps rather than overflows when the window is narrow.
    final tools = Wrap(spacing: 8, runSpacing: 8, crossAxisAlignment: WrapCrossAlignment.center, children: actions);
    final heading = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        if (back != null)
          InkWell(
            borderRadius: BorderRadius.circular(6),
            onTap: back!.$2,
            child: Padding(
              padding: const EdgeInsets.only(right: 6, top: 2, bottom: 6),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(Icons.arrow_back, size: 16, color: p.muted),
                  const SizedBox(width: 6),
                  Text(
                    back!.$1,
                    style: TextStyle(fontSize: 13, color: p.muted, fontWeight: FontWeight.w500),
                  ),
                ],
              ),
            ),
          ),
        Text(title, style: display(compact ? 24 : 30, spacing: -.3)),
        if (subtitle != null && !compact)
          Padding(
            padding: const EdgeInsets.only(top: 6),
            child: Text(subtitle!, style: TextStyle(color: context.pal.muted, fontSize: 13)),
          ),
      ],
    );
    // A page opened from another one on a phone: its tools under the title,
    // where they do not crowd the way back.
    if (compact && back != null && actions.isNotEmpty) {
      return Padding(
        padding: const EdgeInsets.only(bottom: 16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [heading, const SizedBox(height: 12), tools]),
      );
    }
    return Padding(
      padding: EdgeInsets.only(bottom: compact ? 16 : 22),
      child: Wrap(crossAxisAlignment: WrapCrossAlignment.end, alignment: WrapAlignment.spaceBetween, runSpacing: 12, spacing: 16, children: [heading, tools]),
    );
  }
}

/// A panel that shows only its heading until it is opened: for what few
/// people need.
class Fold extends StatefulWidget {
  final String title;
  final String? sub;
  final Widget child;
  const Fold({super.key, required this.title, this.sub, required this.child});

  @override
  State<Fold> createState() => _FoldState();
}

class _FoldState extends State<Fold> {
  bool open = false;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Panel(
      padding: EdgeInsets.zero,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          InkWell(
            borderRadius: BorderRadius.circular(12),
            onTap: () => setState(() => open = !open),
            child: Padding(
              padding: EdgeInsets.symmetric(horizontal: isCompact(context) ? 14 : 20, vertical: 14),
              child: Row(
                children: [
                  Expanded(
                    child: Row(
                      children: [
                        Flexible(
                          child: Text(widget.title, style: display(16), overflow: TextOverflow.ellipsis),
                        ),
                        if (widget.sub != null && !isCompact(context)) ...[
                          const SizedBox(width: 10),
                          Flexible(
                            child: Text(
                              widget.sub!,
                              style: TextStyle(color: p.muted, fontSize: 12),
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                        ],
                      ],
                    ),
                  ),
                  const SizedBox(width: 10),
                  Text(
                    open ? 'Скрыть' : 'Показать',
                    style: TextStyle(fontSize: 12, color: p.accentInk, fontWeight: FontWeight.w600),
                  ),
                  Icon(open ? Icons.keyboard_arrow_up : Icons.keyboard_arrow_down, size: 18, color: p.accentInk),
                ],
              ),
            ),
          ),
          if (open) Padding(padding: EdgeInsets.fromLTRB(isCompact(context) ? 14 : 20, 0, isCompact(context) ? 14 : 20, 16), child: widget.child),
        ],
      ),
    );
  }
}
