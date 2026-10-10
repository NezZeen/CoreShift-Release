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

/// Under the route on the home page, quiet: the speed test and the
/// traffic, a line each. The week in bars opens from its line.
class _HomeTools extends StatelessWidget {
  final AppState state;
  const _HomeTools({required this.state});

  @override
  Widget build(BuildContext context) {
    final rows = [
      _CheckupRow(state: state),
      if (!state.speedUnsupported) _SpeedTestRow(state: state),
      if (!state.statsUnsupported && state.statsLoaded) _TrafficRow(state: state),
    ];
    if (rows.isEmpty) return const SizedBox();
    return Panel(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (final (i, r) in rows.indexed) ...[if (i > 0) Divider(height: 1, indent: 16, endIndent: 16, color: context.pal.border), r],
        ],
      ),
    );
  }
}

/// A tool's line: an icon, what it says, and its button.
class _ToolRow extends StatelessWidget {
  final IconData icon;
  final Color? iconColor;
  final Widget title;
  final String note;
  final Color? noteColor;

  /// A quieter line under the note, when there is one.
  final String detail;
  final Widget action;
  const _ToolRow({required this.icon, this.iconColor, required this.title, required this.note, this.noteColor, this.detail = '', required this.action});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 11, 12, 11),
      child: Row(
        children: [
          Icon(icon, size: 20, color: iconColor ?? p.muted),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                title,
                const SizedBox(height: 2),
                Text(
                  note,
                  style: TextStyle(fontSize: 12, color: noteColor ?? p.dim),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
                if (detail.isNotEmpty) ...[
                  const SizedBox(height: 1),
                  Text(
                    detail,
                    style: TextStyle(fontSize: 11.5, color: p.dim),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ],
              ],
            ),
          ),
          const SizedBox(width: 10),
          action,
        ],
      ),
    );
  }
}

/// «Проверить всё»: what the last checkup found, and the button that runs
/// one and shows it.
class _CheckupRow extends StatelessWidget {
  final AppState state;
  const _CheckupRow({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final c = state.checkup;
    final v = c.verdict;
    final (note, color) = c.running
        ? ('Проверяем сеть, сервер и VPN…', null)
        : v != null
        ? ('${v.title}, ${formatAgo(c.at)}', v.status == 'ok' ? p.okInk : (v.status == 'warn' ? p.warnInk : p.errInk))
        : c.error.isNotEmpty
        ? (c.error, p.errInk)
        : ('Если что-то не работает: сеть, сервер и VPN разом, с советом, что делать', null);
    return _ToolRow(
      icon: Icons.fact_check_outlined,
      iconColor: c.running ? p.accentInk : null,
      title: const Text('Проверить всё', style: TextStyle(fontWeight: FontWeight.w600)),
      note: note,
      noteColor: color,
      action: Btn(label: c.running ? 'Открыть' : (c.done ? 'Ещё раз' : 'Запустить'), small: true, onPressed: () => showCheckup(context, state)),
    );
  }
}

/// The speed test: download, upload and delay in one line, live while it
/// runs.
class _SpeedTestRow extends StatelessWidget {
  final AppState state;
  const _SpeedTestRow({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final t = state.speedTest;
    final bits = max(t.downBps, t.upBps) * 8.0;
    final (unit, scale) = bits >= 1e9 ? ('Гбит/с', 1e9) : (bits >= 1e6 ? ('Мбит/с', 1e6) : ('Кбит/с', 1e3));
    String figure(int bps) {
      if (bps <= 0) return '—';
      final v = bps * 8 / scale;
      return v.toStringAsFixed(v >= 100 ? 0 : 1);
    }

    // One unit for both; the line shrinks as a whole rather than cutting
    // the unit when the width is short.
    final measured = t.phase != SpeedPhase.idle && t.phase != SpeedPhase.failed;
    final title = measured
        ? FittedBox(
            fit: BoxFit.scaleDown,
            alignment: Alignment.centerLeft,
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Icon(Icons.south, size: 14, color: okColor),
                Text(' ${figure(t.downBps)}  ', style: figures(17)),
                const Icon(Icons.north, size: 14, color: accent),
                Text(' ${figure(t.upBps)} ', style: figures(17)),
                Text('$unit${t.latencyMs > 0 ? ' · ${t.latencyMs} мс' : ''}', style: TextStyle(fontSize: 12, color: p.muted)),
              ],
            ),
          )
        : const Text('Тест скорости', style: TextStyle(fontWeight: FontWeight.w600));
    return _ToolRow(
      icon: Icons.speed,
      iconColor: t.running ? p.accentInk : null,
      title: title,
      note: _speedNote(state),
      noteColor: t.phase == SpeedPhase.failed ? p.errInk : null,
      detail: t.phase == SpeedPhase.done && t.testServerName.isNotEmpty ? 'Сервер теста: ${t.testServerName}' : '',
      action: Btn(
        label: t.phase == SpeedPhase.done ? 'Ещё раз' : 'Проверить',
        small: true,
        loading: t.running,
        onPressed: state.online && !state.busy && !t.running ? state.runSpeedTest : null,
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
