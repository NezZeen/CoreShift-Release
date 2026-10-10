part of '../widgets.dart';

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
