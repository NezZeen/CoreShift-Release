part of '../home_page.dart';

/// How long the connection has been up, under "Подключено". It ticks on
/// its own, each tick timed to the next whole second since [since], so no
/// second is skipped or shown twice. When [since] moves while connected (a
/// reconnect: the same server picked again, a switch to a spare one, a new
/// network) the count starts over with a note saying so for a moment,
/// rather than seeming to run backwards.
@visibleForTesting
class ConnectedTime extends StatefulWidget {
  final DateTime since;
  final bool tun;
  final bool short;

  /// The clock; a fake one in tests.
  final DateTime Function() now;
  const ConnectedTime({super.key, required this.since, required this.tun, this.short = false, this.now = DateTime.now});

  /// How long «переподключено» stays.
  static const noteFor = Duration(seconds: 3);

  @override
  State<ConnectedTime> createState() => _ConnectedTimeState();
}

class _ConnectedTimeState extends State<ConnectedTime> {
  Timer? _tick;
  Timer? _noteOff;
  bool _reconnected = false;

  /// Hidden (Android in the background, a hidden window) nothing is drawn,
  /// so the time does not tick until the app shows again.
  bool _shown = true;
  late final AppLifecycleListener _lifecycle;

  @override
  void initState() {
    super.initState();
    _lifecycle = AppLifecycleListener(
      onHide: () {
        _shown = false;
        _tick?.cancel();
      },
      onShow: () {
        _shown = true;
        if (mounted) setState(_schedule);
      },
    );
    _schedule();
  }

  @override
  void didUpdateWidget(ConnectedTime old) {
    super.didUpdateWidget(old);
    if (old.since.isAtSameMomentAs(widget.since)) return;
    _reconnected = true;
    _noteOff?.cancel();
    _noteOff = Timer(ConnectedTime.noteFor, () {
      if (mounted) setState(() => _reconnected = false);
    });
    _schedule();
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    _tick?.cancel();
    _noteOff?.cancel();
    super.dispose();
  }

  /// The next tick, just past the next whole second of the count: a timer
  /// that fires a little early would otherwise show the same second again,
  /// and one second later skip one.
  void _schedule() {
    _tick?.cancel();
    if (!_shown) return;
    final into = widget.now().difference(widget.since).inMilliseconds % 1000;
    _tick = Timer(Duration(milliseconds: 1000 - into + 15), () {
      if (!mounted) return;
      setState(_schedule);
    });
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final d = widget.now().difference(widget.since);
    final mode = widget.short ? (widget.tun ? '' : ' · только прокси') : ' · ${widget.tun ? 'все приложения через VPN' : 'прокси SOCKS5 127.0.0.1:17890'}';
    return AnimatedSwitcher(
      duration: const Duration(milliseconds: 250),
      child: Text.rich(
        key: ValueKey(_reconnected),
        TextSpan(
          children: [
            TextSpan(
              text: formatDuration(d.isNegative ? Duration.zero : d),
              style: figures(15, color: p.text),
            ),
            _reconnected
                ? TextSpan(
                    text: ' · переподключено',
                    style: TextStyle(color: p.okInk),
                  )
                : TextSpan(text: mode),
          ],
        ),
        style: TextStyle(color: p.muted, fontSize: 13.5),
      ),
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
    // Waiting for the network turns as connecting does.
    final offline = widget.state == ConnState.noNetwork;
    final busy = widget.state == ConnState.connecting || widget.state == ConnState.disconnecting || offline;
    final failed = widget.state == ConnState.failed;
    // The lamp's colour: amber stands by, green is through, yellow waits
    // for the network, red failed.
    final lamp = on ? okColor : (failed ? errColor : (offline ? warnColor : (widget.enabled || busy ? accent : p.border2)));
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
