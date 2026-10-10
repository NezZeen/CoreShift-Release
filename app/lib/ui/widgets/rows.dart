part of '../widgets.dart';

class SectionLabel extends StatelessWidget {
  final String text;
  const SectionLabel(this.text, {super.key});

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 8),
    child: Text(
      text,
      style: TextStyle(fontSize: 12, color: context.pal.dim, fontWeight: FontWeight.w600),
    ),
  );
}

/// A settings line: title and description on the left, a control on the right.
class SettingRow extends StatelessWidget {
  final String title;
  final String? description;
  final Widget? descriptionWidget;
  final Widget? trailing;
  final bool first;

  const SettingRow({super.key, required this.title, this.description, this.descriptionWidget, this.trailing, this.first = false});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final compact = isCompact(context);
    final text = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (compact && description != null)
          // On a phone the explanation waits behind an icon.
          Row(
            children: [
              Flexible(
                child: Text(title, style: const TextStyle(fontWeight: FontWeight.w500)),
              ),
              InfoIcon(title: title, text: description!),
            ],
          )
        else
          Text(title, style: const TextStyle(fontWeight: FontWeight.w500)),
        if (description != null && !compact)
          Padding(
            padding: const EdgeInsets.only(top: 2),
            child: Text(description!, style: TextStyle(fontSize: 12, color: p.muted)),
          ),
        if (descriptionWidget != null) Padding(padding: const EdgeInsets.only(top: 4), child: descriptionWidget!),
      ],
    );
    // On a phone a wide control goes under the text; a switch, a word or a short field stays beside it.
    final t = trailing;
    final narrow = t is Switch || t is Text || (t is SavingField && t.width <= 100);
    final below = t != null && !narrow && compact;
    return Container(
      padding: EdgeInsets.only(top: first ? 0 : 13, bottom: 13),
      decoration: BoxDecoration(
        border: first ? null : Border(top: BorderSide(color: p.border)),
      ),
      child: below
          ? Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                text,
                const SizedBox(height: 10),
                Align(
                  alignment: Alignment.centerLeft,
                  child: SingleChildScrollView(scrollDirection: Axis.horizontal, child: trailing),
                ),
              ],
            )
          : Row(
              children: [
                Expanded(child: text),
                if (trailing != null) ...[const SizedBox(width: 14), trailing!],
              ],
            ),
    );
  }
}

/// An "i" that shows [text] in a sheet: a phone's stand-in for the
/// explanations a desktop page shows inline.
class InfoIcon extends StatelessWidget {
  final String title;
  final String text;
  const InfoIcon({super.key, required this.title, required this.text});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return InkResponse(
      radius: 18,
      onTap: () => showModalBottomSheet<void>(
        context: context,
        backgroundColor: p.surface,
        builder: (context) => SafeArea(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(20, 22, 20, 24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: dialogTitle),
                const SizedBox(height: 8),
                Text(text, style: TextStyle(color: p.muted, height: 1.45)),
              ],
            ),
          ),
        ),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
        child: Icon(Icons.info_outline, size: 15, color: p.dim),
      ),
    );
  }
}
