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

/// The speed test: download, upload and delay, live while it runs.
class _SpeedTestCard extends StatelessWidget {
  final AppState state;
  const _SpeedTestCard({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final t = state.speedTest;
    final shown = t.phase != SpeedPhase.idle && t.phase != SpeedPhase.failed;
    final compact = isCompact(context);

    Widget metric(IconData icon, Color color, String label, String value, bool active) => Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 14, color: active ? color : p.dim),
            const SizedBox(width: 4),
            Text(label, style: TextStyle(fontSize: 11.5, color: p.muted)),
          ],
        ),
        const SizedBox(height: 2),
        Text(value, style: figures(compact ? 20 : 24, color: active ? p.text : p.muted)),
      ],
    );

    String rate(int bps, SpeedPhase from) => shown && (t.phase.index >= from.index) && bps > 0 ? formatRate(bps) : '—';
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(child: Text('Тест скорости', style: display(16))),
              Btn(
                label: t.phase == SpeedPhase.done ? 'Ещё раз' : 'Проверить',
                icon: Icons.speed,
                small: true,
                kind: BtnKind.primary,
                loading: t.running,
                onPressed: state.online && !state.busy && !t.running ? state.runSpeedTest : null,
              ),
            ],
          ),
          const SizedBox(height: 14),
          Wrap(
            spacing: 28,
            runSpacing: 10,
            children: [
              metric(Icons.south, okColor, 'Загрузка', rate(t.downBps, SpeedPhase.download), t.phase == SpeedPhase.download || t.phase == SpeedPhase.done),
              metric(Icons.north, accent, 'Отдача', rate(t.upBps, SpeedPhase.upload), t.phase == SpeedPhase.upload || t.phase == SpeedPhase.done),
              metric(Icons.timer_outlined, p.muted, 'Задержка', shown && t.latencyMs > 0 ? '${t.latencyMs} мс' : '—', shown && t.latencyMs > 0),
            ],
          ),
          const SizedBox(height: 12),
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

/// A subscription running out: what is wrong and where to renew it.
class _SubWarningBanner extends StatelessWidget {
  final AppState state;
  const _SubWarningBanner({required this.state});

  @override
  Widget build(BuildContext context) {
    final warnings = state.subscriptionWarnings;
    if (warnings.isEmpty) return const SizedBox();
    final w = warnings.first;
    return _Notice(
      color: w.over ? errColor : warnColor,
      icon: w.traffic ? Icons.data_usage : Icons.event_busy,
      title: w.title,
      text: w.body,
      action: w.renewUrl.isEmpty ? null : Btn(label: 'Продлить', small: true, kind: BtnKind.primary, onPressed: () => state.openLink(w.renewUrl)),
    );
  }
}
