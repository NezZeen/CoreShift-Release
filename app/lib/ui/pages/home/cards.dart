part of '../home_page.dart';

/// The address sites see: the VPN server's while connected, else the
/// user's own. It is looked up again as the connection changes.
class _IpCard extends StatelessWidget {
  final AppState state;
  const _IpCard({required this.state});

  static String _masked(String ip) {
    if (ip.contains(':')) return '${ip.split(':').take(2).join(':')}:…';
    final parts = ip.split('.');
    return parts.length == 4 ? '${parts[0]}.${parts[1]}.•.•' : ip;
  }

  @override
  Widget build(BuildContext context) {
    WidgetsBinding.instance.addPostFrameCallback((_) => state.watchIp());
    final p = context.pal;
    final info = state.publicIp;
    final st = state.status.state;
    // An answer from before the connection changed says nothing now.
    final current = info != null && info.vpn == (st == ConnState.connected);
    final vpn = current && info.vpn;
    final (icon, color, note) = switch (st) {
      _ when !current && state.ipLoading => (Icons.public, p.muted, 'Узнаём адрес…'),
      _ when !current && state.ipError.isNotEmpty => (Icons.public_off, warnColor, state.ipError),
      _ when !current => (Icons.public, p.muted, st == ConnState.connecting ? 'Подключение…' : ''),
      _ when vpn => (Icons.verified_user_outlined, okColor, 'Сайты видят адрес VPN-сервера'),
      _ => (Icons.public, p.muted, 'VPN выключен: сайты видят ваш настоящий адрес'),
    };
    final ip = current ? (state.hideIp ? _masked(info.ip) : info.ip) : '—';

    final compact = isCompact(context);
    final ipText = ip;
    final ipStyle = TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 17, fontWeight: FontWeight.w500);
    final country = current && info.country.isNotEmpty ? countryName(info.country) : '';
    Widget action(IconData i, String tip, VoidCallback? onTap) => IconButton(
      tooltip: tip,
      onPressed: onTap,
      icon: Icon(i, size: 18),
      color: p.muted,
      visualDensity: VisualDensity.compact,
      // A phone is narrow: the address needs the room more.
      constraints: compact ? const BoxConstraints.tightFor(width: 32, height: 36) : null,
      padding: compact ? EdgeInsets.zero : null,
    );

    return _Station(
      caption: 'Ваш IP-адрес',
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          action(
            state.hideIp ? Icons.visibility_off_outlined : Icons.visibility_outlined,
            state.hideIp ? 'Показать адрес' : 'Скрыть адрес, например для скриншота',
            () => state.setPref('hide_ip', !state.hideIp),
          ),
          action(
            Icons.copy,
            'Скопировать адрес',
            current
                ? () {
                    Clipboard.setData(ClipboardData(text: info.ip));
                    state.toast('Адрес скопирован');
                  }
                : null,
          ),
          state.ipLoading
              ? const Padding(
                  padding: EdgeInsets.all(12),
                  child: SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2)),
                )
              : action(Icons.refresh, 'Проверить ещё раз', state.online ? state.refreshIp : null),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // A phone puts the country under the address, which needs the width.
          if (compact) ...[
            Text(ipText, maxLines: 1, overflow: TextOverflow.ellipsis, style: ipStyle),
            if (country.isNotEmpty)
              Text(
                country,
                style: TextStyle(fontSize: 13, color: p.muted),
                overflow: TextOverflow.ellipsis,
              ),
          ] else
            Row(
              crossAxisAlignment: CrossAxisAlignment.baseline,
              textBaseline: TextBaseline.alphabetic,
              children: [
                Flexible(
                  child: Text(ipText, maxLines: 1, overflow: TextOverflow.ellipsis, style: ipStyle),
                ),
                if (country.isNotEmpty) ...[
                  const SizedBox(width: 10),
                  Flexible(
                    child: Text(
                      country,
                      style: TextStyle(fontSize: 13, color: p.muted),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ],
            ),
          if (note.isNotEmpty) ...[
            const SizedBox(height: 2),
            Row(
              children: [
                Icon(icon, size: 13, color: color),
                const SizedBox(width: 5),
                Expanded(
                  child: Text(
                    note,
                    style: TextStyle(fontSize: 12, color: vpn ? p.okInk : (color == warnColor ? p.warnInk : p.muted)),
                    overflow: TextOverflow.ellipsis,
                    maxLines: 2,
                  ),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

class _SpeedCard extends StatelessWidget {
  final AppState state;
  const _SpeedCard({required this.state});

  // Redrawn with every traffic sample, unlike the page around it.
  @override
  Widget build(BuildContext context) => ListenableBuilder(listenable: state.traffic, builder: (context, _) => _build(context));

  Widget _build(BuildContext context) {
    final p = context.pal;
    final active = state.status.state == ConnState.connected;
    final data = active ? state.speed : const <(int, int)>[];
    final (up, down) = data.isEmpty ? (0, 0) : data.last;

    Widget metric(IconData icon, Color color, String label, int rate) => Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 16, color: color),
        const SizedBox(width: 6),
        Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(label, style: TextStyle(fontSize: 11, color: p.muted)),
            Text(active ? formatRate(rate) : '—', style: figures(20)),
          ],
        ),
      ],
    );

    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PanelTitle(
            'Скорость',
            trailing: active && (state.sessionDown > 0 || state.sessionUp > 0)
                ? Tooltip(
                    message: 'Скачано и отправлено за это подключение',
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(Icons.south, size: 12, color: p.muted),
                        Text(' ${formatBytes(state.sessionDown)}   ', style: TextStyle(fontSize: 12, color: p.muted)),
                        Icon(Icons.north, size: 12, color: p.muted),
                        Text(' ${formatBytes(state.sessionUp)}', style: TextStyle(fontSize: 12, color: p.muted)),
                      ],
                    ),
                  )
                : null,
          ),
          // The figures only: a chart of the last minutes says little more.
          Wrap(spacing: 28, runSpacing: 8, children: [metric(Icons.south, okColor, 'Загрузка', down), metric(Icons.north, accent, 'Отдача', up)]),
        ],
      ),
    );
  }
}
