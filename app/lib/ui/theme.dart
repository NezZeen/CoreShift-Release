import 'package:flutter/material.dart';

/// The prototype's colour tokens (prototype/index.html).
@immutable
class Palette extends ThemeExtension<Palette> {
  final Color bg, bg2, surface, surface2, surface3, border, border2, text, muted, dim;

  const Palette({
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
  });

  static const dark = Palette(
    bg: Color(0xFF0A0C11),
    bg2: Color(0xFF0E1117),
    surface: Color(0xFF131720),
    surface2: Color(0xFF1A1F2A),
    surface3: Color(0xFF222838),
    border: Color(0xFF232938),
    border2: Color(0xFF2F3647),
    text: Color(0xFFE7EAF0),
    muted: Color(0xFF8B93A7),
    dim: Color(0xFF5B6275),
  );

  static const light = Palette(
    bg: Color(0xFFF3F5F9),
    bg2: Color(0xFFECEFF5),
    surface: Color(0xFFFFFFFF),
    surface2: Color(0xFFF4F6FA),
    surface3: Color(0xFFE6EAF1),
    border: Color(0xFFE0E4EC),
    border2: Color(0xFFCFD5E0),
    text: Color(0xFF131722),
    muted: Color(0xFF5B6376),
    dim: Color(0xFF8C93A4),
  );

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
    );
  }
}

const accent = Color(0xFF6D8CFF);
const okColor = Color(0xFF34D399);
const warnColor = Color(0xFFFBBF24);
const errColor = Color(0xFFF87171);
const swapColor = Color(0xFFC084FC);

const monoFont = 'Consolas';
const monoFallback = ['Cascadia Mono', 'DejaVu Sans Mono', 'monospace'];

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
  'mihomo' => const CoreStyle('mihomo', 'M', Color(0xFFF59E0B)),
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
    ).copyWith(primary: accent, onPrimary: Colors.white, surface: p.surface, onSurface: p.text, error: errColor),
    scaffoldBackgroundColor: p.bg,
    fontFamily: 'Segoe UI',
    extensions: [p],
  );
  return base.copyWith(
    textTheme: base.textTheme.apply(bodyColor: p.text, displayColor: p.text),
    dividerColor: p.border,
    tooltipTheme: TooltipThemeData(
      decoration: BoxDecoration(
        color: p.surface3,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: p.border2),
      ),
      textStyle: TextStyle(color: p.text, fontSize: 12),
      waitDuration: const Duration(milliseconds: 400),
    ),
    switchTheme: SwitchThemeData(
      thumbColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? Colors.white : p.muted),
      trackColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? accent : p.surface3),
      trackOutlineColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? accent : p.border2),
      // No halo around the thumb on hover, focus or press.
      overlayColor: const WidgetStatePropertyAll(Colors.transparent),
    ),
    inputDecorationTheme: InputDecorationTheme(
      isDense: true,
      filled: true,
      fillColor: p.bg2,
      hintStyle: TextStyle(color: p.dim),
      contentPadding: const EdgeInsets.symmetric(horizontal: 10, vertical: 9),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(9),
        borderSide: BorderSide(color: p.border),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(9),
        borderSide: BorderSide(color: p.border),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(9),
        borderSide: const BorderSide(color: accent),
      ),
      errorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(9),
        borderSide: const BorderSide(color: errColor),
      ),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: p.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(16),
        side: BorderSide(color: p.border2),
      ),
    ),
    popupMenuTheme: PopupMenuThemeData(
      color: p.surface2,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(10),
        side: BorderSide(color: p.border2),
      ),
      textStyle: TextStyle(color: p.text, fontSize: 13),
    ),
    scrollbarTheme: ScrollbarThemeData(
      thumbColor: WidgetStatePropertyAll(p.surface3),
      radius: const Radius.circular(10),
      thickness: const WidgetStatePropertyAll(6),
    ),
  );
}
