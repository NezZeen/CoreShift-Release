part of '../servers_page.dart';

/// The colour of a download speed: green from [speedGoodMbps], amber from
/// [speedFairMbps], red below.
Color speedColor(double mbps) => mbps >= speedGoodMbps ? okColor : (mbps >= speedFairMbps ? warnColor : errColor);

/// «48 Мбит/с», one decimal place below ten.
String formatMbps(double mbps) => '${mbps >= 10 ? mbps.round() : mbps.toStringAsFixed(1)} Мбит/с';

/// A server's download speed as the list shows it, beside its latency.
class _SpeedCell extends StatelessWidget {
  final AppState state;
  final Subscription sub;
  final NodeView node;
  final bool narrow;
  const _SpeedCell({required this.state, required this.sub, required this.node, required this.narrow});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final key = '${sub.id}/${node.fingerprint}';
    final sp = state.nodeSpeeds[key];
    final size = narrow ? 11.5 : 13.0;
    final mono = figures(size);
    // The server being measured now.
    if (state.testingSpeed && state.speedCurrent == key) {
      return Align(
        alignment: Alignment.centerLeft,
        child: SizedBox(width: 12, height: 12, child: CircularProgressIndicator(strokeWidth: 1.5, color: p.dim)),
      );
    }
    if (sp == null) return Text('—', style: mono.copyWith(color: p.dim));
    if (!sp.ok) {
      return Tooltip(
        message: sp.errorText,
        child: Text('сбой', style: mono.copyWith(color: p.errInk, fontSize: 11)),
      );
    }
    final ago = sp.at == null ? '' : ', ${formatAgo(sp.at)}';
    return Tooltip(
      message: 'Скачивание через сервер, ${formatBytes(sp.bytes)} за пробу$ago',
      child: Text(formatMbps(sp.mbps), style: mono.copyWith(color: p.ink(speedColor(sp.mbps)))),
    );
  }
}

/// The latency and the download speed of a server in the list's last
/// column: side by side on the desktop, one above the other on a phone.
class _PingSpeedCell extends StatelessWidget {
  final AppState state;
  final Subscription sub;
  final NodeView node;
  final bool narrow;
  const _PingSpeedCell({required this.state, required this.sub, required this.node, required this.narrow});

  @override
  Widget build(BuildContext context) {
    final ping = _LatencyCell(latency: state.latencyOf(sub.id, node.fingerprint), testing: state.testingLatency);
    if (!state.speedSupported) return ping;
    final speed = _SpeedCell(state: state, sub: sub, node: node, narrow: narrow);
    if (narrow) {
      return Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [ping, const SizedBox(height: 3), speed]);
    }
    return Row(
      children: [
        SizedBox(width: 84, child: ping),
        Expanded(child: speed),
      ],
    );
  }
}

/// The column headings over [_PingSpeedCell].
class _PingSpeedHeader extends StatelessWidget {
  final AppState state;
  final TextStyle style;
  const _PingSpeedHeader({required this.state, required this.style});

  @override
  Widget build(BuildContext context) {
    final ping = Tooltip(
      message: 'Зелёный — до 200 мс, жёлтый — до 500 мс',
      child: Text('Пинг', style: style),
    );
    if (!state.speedSupported) return ping;
    return Row(
      children: [
        SizedBox(width: 84, child: ping),
        Expanded(
          child: Tooltip(
            message: 'Скорость скачивания через сервер: зелёный — от ${speedGoodMbps.round()} Мбит/с, жёлтый — от ${speedFairMbps.round()}',
            child: Text('Скорость', style: style),
          ),
        ),
      ],
    );
  }
}

/// «Скорость: 3 из 30», how much traffic the test has used so far, and the
/// way to stop it; shown over the list while the servers are tested.
class _SpeedBar extends StatelessWidget {
  final AppState state;
  const _SpeedBar({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final total = state.speedTotal;
    final done = state.speedDone.clamp(0, max(total, 1));
    return Container(
      padding: const EdgeInsets.fromLTRB(14, 10, 8, 10),
      decoration: BoxDecoration(
        color: accent.withValues(alpha: .08),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: accent.withValues(alpha: .35)),
      ),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text.rich(
                  TextSpan(
                    text: 'Скорость: $done из $total',
                    style: const TextStyle(fontWeight: FontWeight.w600),
                    children: [
                      if (state.speedBytes > 0)
                        TextSpan(
                          text: '  ·  ${formatBytes(state.speedBytes)}',
                          style: TextStyle(fontWeight: FontWeight.w400, fontSize: 12, color: p.dim),
                        ),
                    ],
                  ),
                ),
                const SizedBox(height: 7),
                ClipRRect(
                  borderRadius: BorderRadius.circular(3),
                  child: LinearProgressIndicator(value: total == 0 ? null : done / total, minHeight: 4, color: accent, backgroundColor: p.border),
                ),
              ],
            ),
          ),
          const SizedBox(width: 10),
          Btn(label: 'Остановить', icon: Icons.stop_rounded, small: true, onPressed: state.stopNodeSpeed),
        ],
      ),
    );
  }
}

/// Tests the download speed of [rows], the servers the list shows; the
/// ones no core can run are left out. A phone asks first, with what it
/// would cost in traffic.
Future<void> _startSpeedTest(BuildContext context, AppState s, List<(Subscription, NodeView)> rows) async {
  if (s.testingSpeed || s.testingLatency) return;
  final usable = [
    for (final r in rows)
      if (r.$2.cores.isNotEmpty) r,
  ];
  if (usable.isEmpty) {
    s.toast('Нет серверов, которые можно проверить');
    return;
  }
  if (platform.isAndroid || isCompact(context)) {
    final plan = await s.speedPlan(usable.length);
    if (!context.mounted) return;
    if (!await _confirmSpeedTest(context, plan)) return;
  }
  final subs = {for (final r in usable) r.$1.id};
  await s.testNodeSpeed(servers: usable.length, subscription: subs.length == 1 ? subs.single : null, fingerprints: [for (final r in usable) r.$2.fingerprint]);
}

/// «30 серверов × 10 МБ ≈ 300 МБ трафика. Продолжить?», with a word more on
/// mobile data.
Future<bool> _confirmSpeedTest(BuildContext context, SpeedPlan plan) async {
  final r = await showDialog<bool>(
    context: context,
    builder: (context) {
      final p = context.pal;
      return Dialog(
        insetPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 24),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 420),
          child: Padding(
            padding: const EdgeInsets.all(22),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('Проверить скорость?', style: dialogTitle),
                const SizedBox(height: 10),
                Text('${plan.estimate}. Продолжить?', style: TextStyle(color: p.muted, height: 1.4)),
                if (plan.mobile) ...[
                  const SizedBox(height: 10),
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Icon(Icons.signal_cellular_alt_rounded, size: 16, color: p.ink(warnColor)),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          'Вы на мобильной сети',
                          style: TextStyle(color: p.ink(warnColor), fontWeight: FontWeight.w600),
                        ),
                      ),
                    ],
                  ),
                ],
                const SizedBox(height: 20),
                Row(
                  mainAxisAlignment: MainAxisAlignment.end,
                  children: [
                    Btn(label: 'Отмена', onPressed: () => Navigator.pop(context, false)),
                    const SizedBox(width: 8),
                    Btn(label: 'Продолжить', kind: BtnKind.primary, onPressed: () => Navigator.pop(context, true)),
                  ],
                ),
              ],
            ),
          ),
        ),
      );
    },
  );
  return r == true;
}
