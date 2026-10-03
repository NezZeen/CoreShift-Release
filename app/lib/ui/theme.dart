import 'package:flutter/material.dart';

/// The colour tokens. The look is a signal box: graphite enclosures, and
/// lamps that mean one thing each: amber stands by and asks for an action,
/// green says the line is clear, red stops.
@immutable
class Palette extends ThemeExtension<Palette> {
  final Color bg, bg2, surface, surface2, surface3, border, border2, text, muted, dim;

  /// The lamps as text and thin lines: on the light theme the lamps
  /// themselves are too pale to read. [ink] picks them.
  final Color accentInk, okInk, warnInk, errInk;

  const Palette({
    required this.okInk,
    required this.warnInk,
    required this.errInk,
    required this.bg,
    required this.bg2,
    required this.surface,
    required this.surface2,
    required this.surface3,
    required this.border,
    required this.border2,
    required this.text,
    required this.muted,
    required this.dim,
    required this.accentInk,
  });

  static const dark = Palette(
    bg: Color(0xFF161A1D),
    bg2: Color(0xFF101315),
    surface: Color(0xFF1D2225),
    surface2: Color(0xFF242A2E),
    surface3: Color(0xFF2E353A),
    border: Color(0xFF293034),
    border2: Color(0xFF384146),
    text: Color(0xFFECEFEC),
    muted: Color(0xFF9AA5A7),
    dim: Color(0xFF6A777C),
    accentInk: Color(0xFFF5B25E),
    okInk: okColor,
    warnInk: warnColor,
    errInk: errColor,
  );

  static const light = Palette(
    bg: Color(0xFFECEFED),
    bg2: Color(0xFFE2E6E4),
    surface: Color(0xFFFBFCFB),
    surface2: Color(0xFFF1F4F2),
    surface3: Color(0xFFE1E6E4),
    border: Color(0xFFD6DCD9),
    border2: Color(0xFFC0C9C6),
    text: Color(0xFF172024),
    muted: Color(0xFF4E5D62),
    dim: Color(0xFF7D8B90),
    // At least 4.5:1 on the light panels.
    accentInk: Color(0xFF8F4C06),
    okInk: Color(0xFF0F7552),
    warnInk: Color(0xFF856000),
    errInk: Color(0xFFC0322D),
  );

  /// A lamp's colour fit for text on this theme's panels.
  Color ink(Color lamp) => switch (lamp) {
    accent => accentInk,
    okColor => okInk,
    warnColor => warnInk,
    errColor => errInk,
    _ => lamp,
  };

  @override
  Palette copyWith() => this;

  @override
  Palette lerp(ThemeExtension<Palette>? other, double t) {
    if (other is! Palette) return this;
    Color c(Color a, Color b) => Color.lerp(a, b, t)!;
    return Palette(
      bg: c(bg, other.bg),
      bg2: c(bg2, other.bg2),
      surface: c(surface, other.surface),
      surface2: c(surface2, other.surface2),
      surface3: c(surface3, other.surface3),
      border: c(border, other.border),
      border2: c(border2, other.border2),
      text: c(text, other.text),
      muted: c(muted, other.muted),
      dim: c(dim, other.dim),
      accentInk: c(accentInk, other.accentInk),
      okInk: c(okInk, other.okInk),
      warnInk: c(warnInk, other.warnInk),
      errInk: c(errInk, other.errInk),
    );
  }
}

/// The amber lamp: the action to take, the selected thing.
const accent = Color(0xFFF0A23B);

/// Text and icons on an amber fill.
const onAccent = Color(0xFF241604);

/// The green lamp: connected, working, fast.
const okColor = Color(0xFF3CCB98);
const warnColor = Color(0xFFF2CB4C);
const errColor = Color(0xFFEE5D58);
const swapColor = Color(0xFFB794F6);

const monoFont = 'Consolas';
const monoFallback = ['Cascadia Mono', 'DejaVu Sans Mono', 'monospace'];

/// Headings and figures: Bahnschrift, the DIN of road and rail signs, which
/// Windows ships. Android has no Bahnschrift: "sans-serif-condensed" is the
/// name its fonts.xml gives Roboto Condensed, the nearest it has; failing
/// that, the plain UI face of each system.
const displayFont = 'Bahnschrift';
const displayFallback = ['sans-serif-condensed', 'Roboto Condensed', 'Segoe UI', 'Roboto', 'sans-serif'];

/// A heading in the display face.
TextStyle display(double size, {FontWeight weight = FontWeight.w600, Color? color, double spacing = 0}) => TextStyle(
  fontFamily: displayFont,
  fontFamilyFallback: displayFallback,
  fontSize: size,
  fontWeight: weight,
  color: color,
  letterSpacing: spacing,
  height: 1.15,
);

/// Speeds, amounts and delays: the display face with even-width digits, so
/// a changing number does not jitter.
TextStyle figures(double size, {FontWeight weight = FontWeight.w500, Color? color}) => TextStyle(
  fontFamily: displayFont,
  fontFamilyFallback: displayFallback,
  fontSize: size,
  fontWeight: weight,
  color: color,
  fontFeatures: const [FontFeature.tabularFigures()],
);

/// A dialog's or a sheet's title.
final dialogTitle = display(19);

extension PaletteOf on BuildContext {
  Palette get pal => Theme.of(this).extension<Palette>()!;
}

class CoreStyle {
  final String name;
  final String letter;
  final Color color;
  const CoreStyle(this.name, this.letter, this.color);
}

CoreStyle coreStyle(String kind) => switch (kind) {
  'xray' => const CoreStyle('Xray-core', 'X', Color(0xFFA78BFA)),
  'sing-box' => const CoreStyle('sing-box', 'S', Color(0xFF2DD4BF)),
  'mihomo' => const CoreStyle('mihomo', 'M', Color(0xFFF28BA8)),
  _ => CoreStyle(kind, kind.isEmpty ? '?' : kind[0].toUpperCase(), const Color(0xFF8B93A7)),
};

const allCores = ['xray', 'sing-box', 'mihomo'];

String protocolLabel(String p) => switch (p) {
  'vless' => 'VLESS',
  'vmess' => 'VMess',
  'trojan' => 'Trojan',
  'shadowsocks' => 'Shadowsocks',
  'hysteria2' => 'Hysteria2',
  'tuic' => 'TUIC',
  'anytls' => 'AnyTLS',
  'wireguard' => 'WireGuard',
  _ => p,
};

Color protocolColor(String p) => switch (p) {
  'vless' => const Color(0xFF6D8CFF),
  'vmess' => const Color(0xFF60A5FA),
  'trojan' => const Color(0xFFF472B6),
  'shadowsocks' => const Color(0xFFA3E635),
  'hysteria2' => const Color(0xFFFB923C),
  'tuic' => const Color(0xFF22D3EE),
  'wireguard' => const Color(0xFFF87171),
  'anytls' => const Color(0xFFC084FC),
  _ => const Color(0xFF8B93A7),
};

ThemeData buildTheme(Brightness b) {
  final p = b == Brightness.dark ? Palette.dark : Palette.light;
  final base = ThemeData(
    brightness: b,
    useMaterial3: true,
    colorScheme: ColorScheme.fromSeed(
      seedColor: accent,
      brightness: b,
    ).copyWith(primary: accent, onPrimary: onAccent, secondary: okColor, surface: p.surface, onSurface: p.text, error: errColor),
    scaffoldBackgroundColor: p.bg,
    fontFamily: 'Segoe UI',
    extensions: [p],
  );
  final dark = b == Brightness.dark;
  return base.copyWith(
    textTheme: base.textTheme.apply(bodyColor: p.text, displayColor: p.text),
    dividerColor: p.border,
    textSelectionTheme: TextSelectionThemeData(cursorColor: p.accentInk, selectionColor: accent.withValues(alpha: .3), selectionHandleColor: accent),
    progressIndicatorTheme: ProgressIndicatorThemeData(color: accent, linearTrackColor: p.surface3, circularTrackColor: Colors.transparent),
    tooltipTheme: TooltipThemeData(
      decoration: BoxDecoration(color: dark ? const Color(0xFFE9ECE9) : const Color(0xFF1C2326), borderRadius: BorderRadius.circular(6)),
      textStyle: TextStyle(color: dark ? const Color(0xFF15191C) : const Color(0xFFECEFEC), fontSize: 12),
      waitDuration: const Duration(milliseconds: 400),
    ),
    switchTheme: SwitchThemeData(
      thumbColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? onAccent : p.muted),
      trackColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? accent : p.bg2),
      trackOutlineColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? accent : p.border2),
      trackOutlineWidth: const WidgetStatePropertyAll(1.5),
      // No halo around the thumb on hover, focus or press.
      overlayColor: const WidgetStatePropertyAll(Colors.transparent),
    ),
    checkboxTheme: CheckboxThemeData(
      fillColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? accent : Colors.transparent),
      checkColor: const WidgetStatePropertyAll(onAccent),
      side: BorderSide(color: p.border2, width: 1.5),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(4)),
    ),
    radioTheme: RadioThemeData(fillColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? accent : p.dim)),
    inputDecorationTheme: InputDecorationTheme(
      isDense: true,
      filled: true,
      fillColor: p.bg2,
      hintStyle: TextStyle(color: p.dim),
      contentPadding: const EdgeInsets.symmetric(horizontal: 11, vertical: 10),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(8),
        borderSide: BorderSide(color: p.border),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(8),
        borderSide: BorderSide(color: p.border),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(8),
        borderSide: BorderSide(color: p.accentInk, width: 1.5),
      ),
      errorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(8),
        borderSide: const BorderSide(color: errColor),
      ),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: p.surface,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(14),
        side: BorderSide(color: p.border2),
      ),
    ),
    bottomSheetTheme: BottomSheetThemeData(
      backgroundColor: p.surface,
      surfaceTintColor: Colors.transparent,
      dragHandleColor: p.border2,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(18))),
    ),
    popupMenuTheme: PopupMenuThemeData(
      color: p.surface2,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(10),
        side: BorderSide(color: p.border2),
      ),
      textStyle: TextStyle(color: p.text, fontSize: 13),
    ),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: p.bg2,
      surfaceTintColor: Colors.transparent,
      indicatorColor: accent,
      indicatorShape: const StadiumBorder(),
      iconTheme: WidgetStateProperty.resolveWith((s) => IconThemeData(size: 22, color: s.contains(WidgetState.selected) ? onAccent : p.muted)),
      labelTextStyle: WidgetStateProperty.resolveWith(
        (s) => TextStyle(
          fontSize: 12,
          fontWeight: s.contains(WidgetState.selected) ? FontWeight.w600 : FontWeight.w500,
          color: s.contains(WidgetState.selected) ? p.text : p.muted,
        ),
      ),
    ),
    listTileTheme: ListTileThemeData(iconColor: p.muted, textColor: p.text),
    scrollbarTheme: ScrollbarThemeData(
      thumbColor: WidgetStatePropertyAll(p.border2),
      radius: const Radius.circular(10),
      thickness: const WidgetStatePropertyAll(6),
    ),
  );
}
