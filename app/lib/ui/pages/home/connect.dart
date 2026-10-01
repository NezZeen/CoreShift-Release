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
    return Text(
      '${formatDuration(d.isNegative ? Duration.zero : d)}$mode',
      style: TextStyle(color: context.pal.muted, fontSize: 13, fontFeatures: const [FontFeature.tabularFigures()]),
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
    return MouseRegion(
      cursor: widget.enabled ? SystemMouseCursors.click : SystemMouseCursors.basic,
      onEnter: (_) => setState(() => hover = true),
      onExit: (_) => setState(() => hover = false),
      child: GestureDetector(
        onTap: widget.enabled ? widget.onTap : null,
        child: AnimatedBuilder(
          animation: _anim,
          builder: (context, _) => CustomPaint(
            painter: _RingPainter(t: _anim.value, busy: busy, on: on),
            child: AnimatedScale(
              scale: hover && widget.enabled ? 1.02 : 1,
              duration: const Duration(milliseconds: 200),
              child: AnimatedContainer(
                duration: const Duration(milliseconds: 350),
                width: widget.size,
                height: widget.size,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  border: Border.all(color: on ? const Color(0xFF2E8566) : p.border2),
                  gradient: RadialGradient(
                    center: const Alignment(0, -.4),
                    radius: .9,
                    colors: on ? const [Color(0xFF1D6B52), Color(0xFF0F3A2D)] : [p.surface3, p.surface2],
                  ),
                  boxShadow: [
                    BoxShadow(color: (on ? okColor : p.text).withValues(alpha: on ? .07 : .03), spreadRadius: 12),
                    if (on) BoxShadow(color: okColor.withValues(alpha: .45), blurRadius: 70, spreadRadius: -12),
                  ],
                ),
                child: Icon(
                  Icons.power_settings_new,
                  size: widget.size * .32,
                  color: on ? const Color(0xFFB8F5DC) : (busy ? accent : p.muted.withValues(alpha: widget.enabled ? 1 : .5)),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _RingPainter extends CustomPainter {
  final double t;
  final bool busy;
  final bool on;
  _RingPainter({required this.t, required this.busy, required this.on});

  @override
  void paint(Canvas canvas, Size size) {
    final c = size.center(Offset.zero);
    final r = size.width / 2;
    if (busy) {
      final paint = Paint()
        ..color = accent
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2
        ..strokeCap = StrokeCap.round;
      canvas.drawArc(Rect.fromCircle(center: c, radius: r + 13), t * 2 * pi * 2.6, pi / 2, false, paint);
    }
    if (on) {
      final k = t;
      final paint = Paint()
        ..color = okColor.withValues(alpha: .5 * (1 - k))
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2;
      canvas.drawCircle(c, r * (1 + .35 * k), paint);
    }
  }

  @override
  bool shouldRepaint(_RingPainter old) => old.t != t || old.busy != busy || old.on != on;
}

class _NodePick extends StatelessWidget {
  final AppState state;
  const _NodePick({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final sel = state.selection;
    final n = sel.node;
    final sub = state.subscriptionById(sel.subscription);
    return Material(
      color: p.surface2,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: BorderSide(color: p.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () => showQuickPick(context, state, onAll: () => Nav.to(context, PageId.servers)),
        hoverColor: p.surface3,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
          child: Row(
            children: [
              CountryBadge(n == null ? null : countryOf(n.name, n.server), width: 38),
              const SizedBox(width: 12),
              Expanded(
                child: n == null
                    ? Text(
                        sel.isEmpty ? 'Выбрать сервер' : sel.name,
                        style: TextStyle(fontWeight: FontWeight.w600, color: p.muted),
                      )
                    : Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            cleanNodeName(n.name),
                            style: const TextStyle(fontWeight: FontWeight.w600),
                            overflow: TextOverflow.ellipsis,
                          ),
                          const SizedBox(height: 3),
                          Row(
                            children: [
                              ProtoBadge(n.protocol),
                              const SizedBox(width: 6),
                              Flexible(
                                child: Text(
                                  '${n.transport} · ${n.security}${sub != null ? ' · ${sub.displayName}' : ''}',
                                  style: TextStyle(color: p.muted, fontSize: 12),
                                  overflow: TextOverflow.ellipsis,
                                ),
                              ),
                            ],
                          ),
                        ],
                      ),
              ),
              if (n != null && state.latencyOf(sel.subscription, n.fingerprint)?.ok == true) ...[
                Text(
                  '${state.latencyOf(sel.subscription, n.fingerprint)!.ms} мс',
                  style: TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 12, color: p.muted),
                ),
                const SizedBox(width: 6),
              ],
              Icon(Icons.unfold_more, size: 20, color: p.dim),
            ],
          ),
        ),
      ),
    );
  }
}
