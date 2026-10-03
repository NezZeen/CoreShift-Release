part of '../home_page.dart';

/// What the speed test is doing, or what it measured and when.
String _speedNote(AppState state) {
  final t = state.speedTest;
  final via = t.vpn ? (t.server.isEmpty ? 'через VPN' : 'через «${cleanNodeName(t.server)}»') : 'без VPN';
  return switch (t.phase) {
    SpeedPhase.latency => 'Замеряем задержку $via…',
    SpeedPhase.download => 'Проверяем загрузку $via…',
    SpeedPhase.upload => 'Проверяем отдачу $via…',
    SpeedPhase.done => '$via, ${formatAgo(t.at)}',
    SpeedPhase.failed => t.error,
    SpeedPhase.idle => state.status.state == ConnState.connected ? 'Скорость через подключённый сервер' : 'Скорость вашего интернета без VPN',
  };
}

/// The speed test on the desktop: download, upload and delay, live while
/// it runs.
class _SpeedTestCard extends StatelessWidget {
  final AppState state;
  const _SpeedTestCard({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final t = state.speedTest;
    final shown = t.phase != SpeedPhase.idle && t.phase != SpeedPhase.failed;

    Widget metric(IconData icon, Color color, String label, String value, bool active) => Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 16, color: active ? color : p.dim),
        const SizedBox(width: 6),
        Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(label, style: TextStyle(fontSize: 11, color: p.muted)),
            Text(
              value,
              style: TextStyle(
                fontFamily: monoFont,
                fontFamilyFallback: monoFallback,
                fontSize: 17,
                fontWeight: FontWeight.w500,
                color: active ? p.text : p.muted,
              ),
            ),
          ],
        ),
      ],
    );

    String rate(int bps, SpeedPhase from) => shown && (t.phase.index >= from.index) && bps > 0 ? formatRate(bps) : '—';
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PanelTitle(
            'Тест скорости',
            trailing: Btn(
              label: t.phase == SpeedPhase.done ? 'Ещё раз' : 'Проверить',
              icon: Icons.speed,
              small: true,
              loading: t.running,
              onPressed: state.online && !state.busy && !t.running ? state.runSpeedTest : null,
            ),
          ),
          Wrap(
            spacing: 28,
            runSpacing: 8,
            children: [
              metric(Icons.south, okColor, 'Загрузка', rate(t.downBps, SpeedPhase.download), t.phase == SpeedPhase.download || t.phase == SpeedPhase.done),
              metric(Icons.north, accent, 'Отдача', rate(t.upBps, SpeedPhase.upload), t.phase == SpeedPhase.upload || t.phase == SpeedPhase.done),
              metric(Icons.timer_outlined, p.muted, 'Задержка', shown && t.latencyMs > 0 ? '${t.latencyMs} мс' : '—', shown && t.latencyMs > 0),
            ],
          ),
          const SizedBox(height: 10),
          if (t.running) ...[
            ClipRRect(
              borderRadius: BorderRadius.circular(9),
              child: LinearProgressIndicator(
                minHeight: 4,
                value: switch (t.phase) {
                  SpeedPhase.latency => .05,
                  SpeedPhase.download => .45,
                  _ => .85,
                },
                backgroundColor: p.surface3,
              ),
            ),
            const SizedBox(height: 8),
          ],
          Text(
            _speedNote(state),
            style: TextStyle(fontSize: 12, color: t.phase == SpeedPhase.failed ? errColor : p.dim),
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
          ),
        ],
      ),
    );
  }
}

/// The speed test on the phone: one line with the figures and the button.
class _CompactSpeedTest extends StatelessWidget {
  final AppState state;
  const _CompactSpeedTest({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final t = state.speedTest;
    final mono = TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 14, fontWeight: FontWeight.w500, color: p.text);
    final bits = max(t.downBps, t.upBps) * 8.0;
    final (unit, scale) = bits >= 1e9 ? ('Гбит/с', 1e9) : (bits >= 1e6 ? ('Мбит/с', 1e6) : ('Кбит/с', 1e3));
    String figure(int bps) {
      if (bps <= 0) return '—';
      final v = bps * 8 / scale;
      return v.toStringAsFixed(v >= 100 ? 0 : 1);
    }

    // One unit for both: a phone has no room for two. The line shrinks as
    // a whole rather than cutting the unit when the width is short.
    final figures = t.phase == SpeedPhase.idle || t.phase == SpeedPhase.failed
        ? null
        : FittedBox(
            fit: BoxFit.scaleDown,
            alignment: Alignment.centerLeft,
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(Icons.south, size: 14, color: okColor),
                Text(' ${figure(t.downBps)}  ', style: mono),
                Icon(Icons.north, size: 14, color: accent),
                Text(' ${figure(t.upBps)} ', style: mono),
                Text('$unit${t.latencyMs > 0 ? ' · ${t.latencyMs} мс' : ''}', style: TextStyle(fontSize: 12, color: p.muted)),
              ],
            ),
          );
    return Panel(
      padding: const EdgeInsets.fromLTRB(14, 10, 10, 10),
      child: Row(
        children: [
          Icon(Icons.speed, size: 20, color: t.running ? accent : p.muted),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                figures ?? Text('Тест скорости', style: const TextStyle(fontWeight: FontWeight.w600)),
                const SizedBox(height: 2),
                Text(
                  _speedNote(state),
                  style: TextStyle(fontSize: 11.5, color: t.phase == SpeedPhase.failed ? errColor : p.dim),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          Btn(
            label: t.phase == SpeedPhase.done ? 'Ещё раз' : 'Проверить',
            small: true,
            loading: t.running,
            onPressed: state.online && !state.busy && !t.running ? state.runSpeedTest : null,
          ),
        ],
      ),
    );
  }
}

/// A subscription running out: what is wrong and where to renew it.
class _SubWarningBanner extends StatelessWidget {
  final AppState state;
  const _SubWarningBanner({required this.state});

  @override
  Widget build(BuildContext context) {
    final warnings = state.subscriptionWarnings;
    if (warnings.isEmpty) return const SizedBox();
    final w = warnings.first;
    final color = w.over ? errColor : warnColor;
    return Container(
      padding: const EdgeInsets.fromLTRB(12, 9, 8, 9),
      decoration: BoxDecoration(
        color: color.withValues(alpha: .08),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: color.withValues(alpha: .35)),
      ),
      child: Row(
        children: [
          Icon(w.traffic ? Icons.data_usage : Icons.event_busy, size: 18, color: color),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(w.title, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
                Text(w.body, style: TextStyle(fontSize: 12, color: context.pal.muted)),
              ],
            ),
          ),
          if (w.renewUrl.isNotEmpty) ...[const SizedBox(width: 8), Btn(label: 'Продлить', small: true, onPressed: () => state.openLink(w.renewUrl))],
        ],
      ),
    );
  }
}
