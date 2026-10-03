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
            child: Row(
              children: [
                Flexible(
                  child: Text(title, style: display(16), overflow: TextOverflow.ellipsis),
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
      BtnKind.primary => (accent, onAccent, accent),
      BtnKind.ghost => (Colors.transparent, p.text, p.border2),
      BtnKind.danger => (Colors.transparent, errColor, errColor.withValues(alpha: .55)),
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
              style: TextStyle(color: fg, fontWeight: kind == BtnKind.primary ? FontWeight.w600 : FontWeight.w500, fontSize: small ? 12 : 13),
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
          borderRadius: BorderRadius.circular(small ? 6 : 8),
          side: BorderSide(color: border),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: enabled ? onPressed : null,
          hoverColor: kind == BtnKind.primary ? Colors.white.withValues(alpha: .18) : p.surface3,
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
        borderRadius: BorderRadius.circular(99),
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
                      padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 6),
                      // The chosen option is lit: the panel's ink as its fill.
                      decoration: BoxDecoration(color: on ? p.text : Colors.transparent, borderRadius: BorderRadius.circular(99)),
                      child: Text(
                        label,
                        style: TextStyle(
                          fontSize: 13,
                          fontWeight: on ? FontWeight.w600 : FontWeight.w500,
                          color: on ? p.bg : (disabled.contains(v) ? p.dim : p.muted),
                        ),
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
    final p = context.pal;
    // The light theme: the tint stays, the text and the edge darken so a
    // pale colour still reads.
    final ink = p.ink(color);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: p.isLight ? .14 : .13),
        borderRadius: BorderRadius.circular(4),
        border: Border.all(color: p.isLight ? ink.withValues(alpha: .35) : color.withValues(alpha: .28)),
      ),
      child: Text(
        text,
        style: TextStyle(color: ink, fontSize: 11, fontWeight: FontWeight.w600, fontFamily: mono ? monoFont : null),
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

/// CoreShift's mark: a railway switch, the straight track and the one it
/// shifts to, on the amber of the lamps.
class CoreShiftMark extends StatelessWidget {
  final double size;
  const CoreShiftMark({super.key, this.size = 24});

  @override
  Widget build(BuildContext context) => SizedBox(
    width: size,
    height: size,
    child: CustomPaint(painter: _MarkPainter()),
  );
}

class _MarkPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final s = size.width / 24;
    canvas.drawRRect(RRect.fromRectAndRadius(Offset.zero & size, Radius.circular(6 * s)), Paint()..color = accent);
    final line = Paint()
      ..color = onAccent
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2.4 * s
      ..strokeCap = StrokeCap.round
      ..strokeJoin = StrokeJoin.round;
    canvas.drawLine(Offset(5 * s, 8.5 * s), Offset(19 * s, 8.5 * s), line);
    canvas.drawPath(
      Path()
        ..moveTo(7.5 * s, 8.5 * s)
        ..cubicTo(11 * s, 8.5 * s, 11.5 * s, 15.5 * s, 15 * s, 15.5 * s)
        ..lineTo(19 * s, 15.5 * s),
      line,
    );
  }

  @override
  bool shouldRepaint(_MarkPainter old) => false;
}

/// A signal lamp: a dot in its colour, glowing while [lit].
class Lamp extends StatelessWidget {
  final Color color;
  final bool lit;
  final double size;
  const Lamp({super.key, required this.color, this.lit = false, this.size = 8});

  @override
  Widget build(BuildContext context) => Container(
    width: size,
    height: size,
    decoration: BoxDecoration(
      color: color,
      shape: BoxShape.circle,
      boxShadow: lit ? [BoxShadow(color: color.withValues(alpha: .55), blurRadius: size * 1.2, spreadRadius: size * .1)] : null,
    ),
  );
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
