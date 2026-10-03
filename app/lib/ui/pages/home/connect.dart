part of '../home_page.dart';

class _Elapsed extends StatefulWidget {
  final DateTime since;
  final bool tun;
  final bool short;
  const _Elapsed({required this.since, required this.tun, this.short = false});

  @override
  State<_Elapsed> createState() => _ElapsedState();
}

class _ElapsedState extends State<_Elapsed> {
  late final Timer _t = Timer.periodic(const Duration(seconds: 1), (_) => setState(() {}));

  @override
  void dispose() {
    _t.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final d = DateTime.now().difference(widget.since);
    final mode = widget.short ? (widget.tun ? '' : ' · только прокси') : ' · ${widget.tun ? 'все приложения через VPN' : 'прокси SOCKS5 127.0.0.1:17890'}';
    return Text.rich(
      TextSpan(
        children: [
          TextSpan(
            text: formatDuration(d.isNegative ? Duration.zero : d),
            style: figures(15, color: context.pal.text),
          ),
          TextSpan(text: mode),
        ],
      ),
      style: TextStyle(color: context.pal.muted, fontSize: 13.5),
    );
  }
}

class _ConnectButton extends StatefulWidget {
  final ConnState state;
  final bool enabled;
  final VoidCallback onTap;
  final double size;
  const _ConnectButton({required this.state, required this.enabled, required this.onTap, this.size = 176});

  @override
  State<_ConnectButton> createState() => _ConnectButtonState();
}

class _ConnectButtonState extends State<_ConnectButton> with SingleTickerProviderStateMixin {
  late final AnimationController _anim = AnimationController(vsync: this, duration: const Duration(milliseconds: 2400))..repeat();
  bool hover = false;

  @override
  void dispose() {
    _anim.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final on = widget.state == ConnState.connected;
    final busy = widget.state == ConnState.connecting || widget.state == ConnState.disconnecting;
    final failed = widget.state == ConnState.failed;
    // The lamp's colour: amber stands by, green is through, red failed.
    final lamp = on ? okColor : (failed ? errColor : (widget.enabled || busy ? accent : p.border2));
    final glyph = on ? const Color(0xFF052A1D) : (failed ? errColor : (widget.enabled || busy ? p.accentInk : p.dim));
    return MouseRegion(
      cursor: widget.enabled ? SystemMouseCursors.click : SystemMouseCursors.basic,
      onEnter: (_) => setState(() => hover = true),
      onExit: (_) => setState(() => hover = false),
      child: GestureDetector(
        onTap: widget.enabled ? widget.onTap : null,
        child: AnimatedBuilder(
          animation: _anim,
          builder: (context, _) => CustomPaint(
            painter: _RingPainter(t: _anim.value, busy: busy, on: on, lamp: lamp, track: p.border),
            child: Padding(
              // Room for the rings around the button.
              padding: EdgeInsets.all(widget.size * .16),
              child: AnimatedScale(
                scale: hover && widget.enabled ? 1.03 : 1,
                duration: const Duration(milliseconds: 200),
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 350),
                  width: widget.size,
                  height: widget.size,
                  decoration: BoxDecoration(
                    shape: BoxShape.circle,
                    border: Border.all(
                      color: on ? okColor : lamp.withValues(alpha: widget.enabled || busy || failed ? .9 : .6),
                      width: on ? 0 : 2.5,
                    ),
                    gradient: RadialGradient(
                      center: const Alignment(0, -.35),
                      radius: .95,
                      colors: on ? const [Color(0xFF5FE0B2), Color(0xFF2DB585)] : [p.surface2, p.surface],
                    ),
                    boxShadow: [
                      if (on) BoxShadow(color: okColor.withValues(alpha: .5), blurRadius: 60, spreadRadius: -8),
                      if (!on && widget.enabled && hover) BoxShadow(color: accent.withValues(alpha: .28), blurRadius: 40, spreadRadius: -6),
                    ],
                  ),
                  child: Icon(Icons.power_settings_new, size: widget.size * .34, color: glyph),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// The rings around the button: a resting track, a turning amber arc
/// while it connects, and a ripple going out while connected.
class _RingPainter extends CustomPainter {
  final double t;
  final bool busy;
  final bool on;
  final Color lamp;
  final Color track;
  _RingPainter({required this.t, required this.busy, required this.on, required this.lamp, required this.track});

  @override
  void paint(Canvas canvas, Size size) {
    final c = size.center(Offset.zero);
    final outer = size.width / 2 - 2;
    final inner = size.width / 2 / 1.32 + size.width * .04;
    final rest = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.5;
    canvas.drawCircle(c, outer, rest..color = on ? okColor.withValues(alpha: .35) : track);
    canvas.drawCircle(c, inner + (outer - inner) * .45, rest..color = on ? okColor.withValues(alpha: .2) : track.withValues(alpha: .6));
    if (busy) {
      final paint = Paint()
        ..color = accent
        ..style = PaintingStyle.stroke
        ..strokeWidth = 3
        ..strokeCap = StrokeCap.round;
      canvas.drawArc(Rect.fromCircle(center: c, radius: outer), t * 2 * pi * 2.6, pi / 2.2, false, paint);
    }
    if (on) {
      final paint = Paint()
        ..color = okColor.withValues(alpha: .55 * (1 - t))
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2;
      canvas.drawCircle(c, inner + (outer - inner) * t, paint);
    }
  }

  @override
  bool shouldRepaint(_RingPainter old) => old.t != t || old.busy != busy || old.on != on || old.lamp != lamp || old.track != track;
}
