part of '../widgets.dart';

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
