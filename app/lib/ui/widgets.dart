import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'countries.dart';
import 'theme.dart';

/// A bordered surface, the prototype's `.card`.
class Panel extends StatelessWidget {
  final Widget child;
  final EdgeInsets? padding;
  final Color? borderColor;
  final VoidCallback? onTap;
  final bool dashed;

  const Panel({super.key, required this.child, this.padding, this.borderColor, this.onTap, this.dashed = false});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final box = Container(
      padding: padding ?? EdgeInsets.all(isCompact(context) ? 14 : 18),
      decoration: BoxDecoration(
        color: dashed ? Colors.transparent : p.surface,
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: borderColor ?? p.border),
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
        padding: isCompact(context) ? const EdgeInsets.fromLTRB(14, 14, 14, 28) : const EdgeInsets.fromLTRB(28, 24, 28, 40),
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
            child: Row(
              children: [
                Flexible(
                  child: Text(
                    title,
                    style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 14),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (compact && info != null) InfoIcon(title: title, text: info!),
                if (sub != null && !compact) ...[
                  const SizedBox(width: 10),
                  Flexible(
                    child: Text(
                      sub!,
                      style: TextStyle(color: context.pal.muted, fontSize: 12),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
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
  const PageHeader(this.title, {super.key, this.subtitle, this.actions = const []});

  @override
  Widget build(BuildContext context) {
    final compact = isCompact(context);
    return Padding(
      padding: EdgeInsets.only(bottom: compact ? 14 : 18),
      child: Wrap(
        crossAxisAlignment: WrapCrossAlignment.end,
        alignment: WrapAlignment.spaceBetween,
        runSpacing: 12,
        spacing: 16,
        children: [
          Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                title,
                style: TextStyle(fontSize: compact ? 20 : 22, fontWeight: FontWeight.w600, letterSpacing: -.2),
              ),
              if (subtitle != null && !compact)
                Padding(
                  padding: const EdgeInsets.only(top: 3),
                  child: Text(subtitle!, style: TextStyle(color: context.pal.muted)),
                ),
            ],
          ),
          // Wraps rather than overflows when the window is narrow.
          Wrap(spacing: 8, runSpacing: 8, crossAxisAlignment: WrapCrossAlignment.center, children: actions),
        ],
      ),
    );
  }
}

enum BtnKind { normal, primary, ghost, danger }

class Btn extends StatelessWidget {
  final String? label;
  final IconData? icon;
  final VoidCallback? onPressed;
  final BtnKind kind;
  final bool small;
  final bool loading;
  final String? tooltip;

  const Btn({super.key, this.label, this.icon, this.onPressed, this.kind = BtnKind.normal, this.small = false, this.loading = false, this.tooltip});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final enabled = onPressed != null && !loading;
    final (bg, fg, border) = switch (kind) {
      BtnKind.primary => (accent, Colors.white, accent),
      BtnKind.ghost => (Colors.transparent, p.text, p.border2),
      BtnKind.danger => (Colors.transparent, errColor, errColor.withValues(alpha: .5)),
      BtnKind.normal => (p.surface2, p.text, p.border2),
    };
    final iconSize = small ? 14.0 : 16.0;
    Widget content = Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (loading)
          SizedBox(
            width: iconSize,
            height: iconSize,
            child: CircularProgressIndicator(strokeWidth: 2, color: fg),
          )
        else if (icon != null)
          Icon(icon, size: iconSize, color: fg),
        if ((icon != null || loading) && label != null) SizedBox(width: small ? 6 : 7),
        if (label != null)
          Flexible(
            child: Text(
              label!,
              style: TextStyle(color: fg, fontWeight: FontWeight.w500, fontSize: small ? 12 : 13),
              overflow: TextOverflow.ellipsis,
            ),
          ),
      ],
    );
    final btn = Opacity(
      opacity: enabled || loading ? 1 : .45,
      child: Material(
        color: bg,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(small ? 8 : 10),
          side: BorderSide(color: border),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: enabled ? onPressed : null,
          hoverColor: kind == BtnKind.primary ? Colors.white.withValues(alpha: .08) : p.surface3,
          child: Padding(
            padding: label == null ? EdgeInsets.all(small ? 6 : 8) : EdgeInsets.symmetric(horizontal: small ? 10 : 14, vertical: small ? 6 : 8),
            child: content,
          ),
        ),
      ),
    );
    return tooltip == null ? btn : Tooltip(message: tooltip!, child: btn);
  }
}

/// Segmented control, the prototype's `.seg`.
class Seg<T> extends StatelessWidget {
  final List<(T, String)> options;
  final T value;
  final ValueChanged<T>? onChanged;
  final Set<T> disabled;
  final Map<T, String> tooltips;

  const Seg({super.key, required this.options, required this.value, this.onChanged, this.disabled = const {}, this.tooltips = const {}});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final seg = Container(
      padding: const EdgeInsets.all(3),
      decoration: BoxDecoration(
        color: p.bg2,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: p.border),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          for (final (v, label) in options)
            Builder(
              builder: (context) {
                final on = v == value;
                final off = disabled.contains(v) || onChanged == null;
                Widget b = MouseRegion(
                  cursor: off ? SystemMouseCursors.basic : SystemMouseCursors.click,
                  child: GestureDetector(
                    onTap: off || on ? null : () => onChanged!(v),
                    child: AnimatedContainer(
                      duration: const Duration(milliseconds: 150),
                      margin: const EdgeInsets.symmetric(horizontal: 1),
                      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                      decoration: BoxDecoration(color: on ? p.surface3 : Colors.transparent, borderRadius: BorderRadius.circular(7)),
                      child: Text(
                        label,
                        style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500, color: on ? p.text : (disabled.contains(v) ? p.dim : p.muted)),
                      ),
                    ),
                  ),
                );
                final tip = tooltips[v];
                return tip == null ? b : Tooltip(message: tip, child: b);
              },
            ),
        ],
      ),
    );
    // Too wide for a phone: it scrolls sideways rather than overflowing.
    return LayoutBuilder(
      builder: (context, c) => c.hasBoundedWidth ? SingleChildScrollView(scrollDirection: Axis.horizontal, child: seg) : seg,
    );
  }
}

class Pill extends StatelessWidget {
  final String text;
  final Color color;
  final bool mono;
  const Pill(this.text, {super.key, required this.color, this.mono = false});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(color: color.withValues(alpha: .15), borderRadius: BorderRadius.circular(6)),
      child: Text(
        text,
        style: TextStyle(color: color, fontSize: 11, fontWeight: FontWeight.w600, letterSpacing: .2, fontFamily: mono ? monoFont : null),
      ),
    );
  }
}

class ProtoBadge extends StatelessWidget {
  final String protocol;
  const ProtoBadge(this.protocol, {super.key});

  @override
  Widget build(BuildContext context) => Pill(protocolLabel(protocol), color: protocolColor(protocol));
}

/// A core's letter in its colour; [size] 20 is the node-list dot.
class CoreLogo extends StatelessWidget {
  final String kind;
  final double size;
  final bool off;
  const CoreLogo(this.kind, {super.key, this.size = 20, this.off = false});

  @override
  Widget build(BuildContext context) {
    final s = coreStyle(kind);
    final p = context.pal;
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: off ? Colors.transparent : s.color,
        borderRadius: BorderRadius.circular(size * .28),
        border: off ? Border.all(color: p.border2) : null,
        boxShadow: size >= 40 && !off
            ? [BoxShadow(color: s.color.withValues(alpha: .45), blurRadius: 24, offset: const Offset(0, 8), spreadRadius: -10)]
            : null,
      ),
      child: Text(
        s.letter,
        style: TextStyle(fontWeight: FontWeight.w800, fontSize: size * .45, color: off ? p.dim : const Color(0xFF0B0D12), height: 1),
      ),
    );
  }
}

class SectionLabel extends StatelessWidget {
  final String text;
  const SectionLabel(this.text, {super.key});

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: 8),
    child: Text(
      text.toUpperCase(),
      style: TextStyle(fontSize: 11, letterSpacing: .7, color: context.pal.dim, fontWeight: FontWeight.w600),
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
      padding: EdgeInsets.only(top: first ? 0 : 12, bottom: 12),
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
        backgroundColor: p.bg2,
        builder: (context) => SafeArea(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(20, 20, 20, 24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
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

/// A text field that saves on Enter or when it loses focus, and shows the
/// saved value again if saving fails.
class SavingField extends StatefulWidget {
  final String value;
  final Future<String?> Function(String) onSave;
  final double width;
  final bool mono;
  final bool numeric;
  final String? hint;
  final TextAlign align;
  final bool enabled;

  const SavingField({
    super.key,
    required this.value,
    required this.onSave,
    this.width = 160,
    this.mono = false,
    this.numeric = false,
    this.hint,
    this.align = TextAlign.start,
    this.enabled = true,
  });

  @override
  State<SavingField> createState() => _SavingFieldState();
}

class _SavingFieldState extends State<SavingField> {
  late final TextEditingController _c = TextEditingController(text: widget.value);
  final _focus = FocusNode();
  bool _error = false;

  @override
  void initState() {
    super.initState();
    _focus.addListener(() {
      if (!_focus.hasFocus) _save();
    });
  }

  @override
  void didUpdateWidget(SavingField old) {
    super.didUpdateWidget(old);
    if (!_focus.hasFocus && old.value != widget.value) _c.text = widget.value;
  }

  Future<void> _save() async {
    if (_c.text == widget.value) {
      setState(() => _error = false);
      return;
    }
    final err = await widget.onSave(_c.text);
    if (!mounted) return;
    setState(() => _error = err != null);
    if (err != null && !_focus.hasFocus) _c.text = widget.value;
  }

  @override
  void dispose() {
    _c.dispose();
    _focus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: widget.width,
      child: TextField(
        controller: _c,
        focusNode: _focus,
        enabled: widget.enabled,
        textAlign: widget.align,
        onSubmitted: (_) => _save(),
        keyboardType: widget.numeric ? TextInputType.number : null,
        inputFormatters: widget.numeric ? [FilteringTextInputFormatter.digitsOnly] : null,
        style: TextStyle(fontSize: 13, fontFamily: widget.mono ? monoFont : null, fontFamilyFallback: widget.mono ? monoFallback : null),
        decoration: InputDecoration(
          hintText: widget.hint,
          enabledBorder: _error
              ? OutlineInputBorder(
                  borderRadius: BorderRadius.circular(9),
                  borderSide: const BorderSide(color: errColor),
                )
              : null,
        ),
      ),
    );
  }
}

/// A server's country as a small tag with its two-letter code. Windows has
/// no flag emoji, so one tag looks the same on every system; each country
/// keeps one colour, which makes a list scan faster.
class CountryBadge extends StatelessWidget {
  final String? code;
  final double width;
  const CountryBadge(this.code, {super.key, this.width = 30});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final c = code;
    final h = width * .68;
    if (c == null || c.isEmpty) {
      return Container(
        width: width,
        height: h,
        alignment: Alignment.center,
        decoration: BoxDecoration(color: p.surface3, borderRadius: BorderRadius.circular(5)),
        child: Icon(Icons.public, size: h * .65, color: p.dim),
      );
    }
    final hue = (c.codeUnitAt(0) * 37 + c.codeUnitAt(c.length > 1 ? 1 : 0) * 91) % 360;
    final dark = Theme.of(context).brightness == Brightness.dark;
    return Tooltip(
      message: countryName(c),
      child: Container(
        width: width,
        height: h,
        alignment: Alignment.center,
        decoration: BoxDecoration(color: HSLColor.fromAHSL(1, hue.toDouble(), .5, dark ? .34 : .42).toColor(), borderRadius: BorderRadius.circular(5)),
        child: Text(
          c,
          style: TextStyle(color: Colors.white, fontSize: width * .36, fontWeight: FontWeight.w700, letterSpacing: .4, height: 1),
        ),
      ),
    );
  }
}

/// Megabits per second, the unit internet plans are sold in.
String formatRate(int bytesPerSecond) {
  final bits = bytesPerSecond * 8.0;
  if (bits >= 1e9) return '${(bits / 1e9).toStringAsFixed(1)} Гбит/с';
  if (bits >= 1e6) return '${(bits / 1e6).toStringAsFixed(bits >= 1e8 ? 0 : 1)} Мбит/с';
  if (bits >= 1e3) return '${(bits / 1e3).round()} Кбит/с';
  return bits == 0 ? '0' : '${bits.round()} бит/с';
}

String formatBytes(num b) {
  if (b >= 1e12) return '${(b / 1e12).toStringAsFixed(1)} ТБ';
  if (b >= 1e9) return '${(b / 1e9).toStringAsFixed(b >= 1e11 ? 0 : 1)} ГБ';
  if (b >= 1e6) return '${(b / 1e6).toStringAsFixed(0)} МБ';
  return '${(b / 1e3).toStringAsFixed(0)} КБ';
}

String formatAgo(DateTime? t) {
  if (t == null) return 'никогда';
  final d = DateTime.now().difference(t);
  if (d.isNegative) return formatIn(t);
  if (d.inMinutes < 1) return 'только что';
  if (d.inHours < 1) return '${d.inMinutes} мин назад';
  if (d.inDays < 1) return '${d.inHours} ч назад';
  return '${d.inDays} дн назад';
}

String formatIn(DateTime t) {
  final d = t.difference(DateTime.now());
  if (d.inMinutes < 1) return 'сейчас';
  if (d.inHours < 1) return 'через ${d.inMinutes} мин';
  if (d.inDays < 1) return 'через ${d.inHours} ч';
  return 'через ${d.inDays} дн';
}

String formatDate(DateTime t) => '${t.day.toString().padLeft(2, '0')}.${t.month.toString().padLeft(2, '0')}.${t.year}';

String formatDuration(Duration d) {
  String two(int n) => n.toString().padLeft(2, '0');
  return '${two(d.inHours)}:${two(d.inMinutes % 60)}:${two(d.inSeconds % 60)}';
}
