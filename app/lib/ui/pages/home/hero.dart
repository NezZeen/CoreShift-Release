part of '../home_page.dart';

/// The state in words and the colour of its lamp.
(String, Color) _stateTitle(BuildContext context, ConnState st) => switch (st) {
  ConnState.connected => ('Подключено', okColor),
  ConnState.connecting => ('Подключение…', context.pal.accentInk),
  ConnState.disconnecting => ('Отключение…', context.pal.muted),
  ConnState.failed => ('Ошибка подключения', errColor),
  ConnState.idle => ('Отключено', context.pal.text),
};

/// The button, the state in big letters, and what to do about it: the
/// part of the page that connects.
class _Hero extends StatelessWidget {
  final AppState state;
  final double size;
  const _Hero({required this.state, this.size = 184});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status;
    final sel = state.selection;
    final (title, color) = _stateTitle(context, st.state);
    final canConnect = state.online && (st.active || (sel.available && !state.busy));
    final muted = TextStyle(color: p.muted, fontSize: 13.5);
    final Widget line = switch (st.state) {
      ConnState.connected when st.since != null => _Elapsed(since: st.since!, tun: st.tun, short: isCompact(context)),
      ConnState.failed => Tooltip(
        message: st.error,
        child: Text(humanError(st.error), textAlign: TextAlign.center, maxLines: 3, overflow: TextOverflow.ellipsis, style: muted),
      ),
      ConnState.idle when sel.isEmpty => Text('Сначала выберите сервер', style: muted),
      ConnState.idle when !sel.available => Text(
        'Сервер «${sel.name}» пропал из подписки',
        textAlign: TextAlign.center,
        style: const TextStyle(color: warnColor, fontSize: 13.5),
      ),
      ConnState.idle => Text('Нажмите, чтобы подключиться', style: muted),
      _ => const SizedBox(),
    };
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Center(
          child: _ConnectButton(state: st.state, enabled: canConnect, onTap: state.toggleConnect, size: size),
        ),
        const SizedBox(height: 14),
        Text(
          title,
          textAlign: TextAlign.center,
          style: display(isCompact(context) ? 30 : 34, color: color, spacing: -.4),
        ),
        const SizedBox(height: 6),
        ConstrainedBox(
          constraints: const BoxConstraints(minHeight: 20),
          child: Center(child: line),
        ),
      ],
    );
  }
}

/// The way the traffic takes, as stations on a line: this device and its
/// rules, the server, and the address sites see at the end. Lit green
/// while connected.
class _Route extends StatelessWidget {
  final AppState state;
  const _Route({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final on = state.status.state == ConnState.connected;
    final stations = <(Color, Widget)>[
      (p.text, _DeviceStation(state: state)),
      (on ? okColor : accent, _ServerStation(state: state)),
      if (!state.ipUnsupported) (on ? okColor : p.dim, _IpCard(state: state)),
    ];
    return Panel(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (final (i, (dot, child)) in stations.indexed)
            CustomPaint(
              painter: _RoutePainter(
                first: i == 0,
                last: i == stations.length - 1,
                dot: dot,
                line: on ? okColor.withValues(alpha: .7) : p.border2,
                lit: on && i > 0,
                hollow: p.surface,
              ),
              child: Padding(padding: const EdgeInsets.only(left: 42), child: child),
            ),
        ],
      ),
    );
  }
}

class _RoutePainter extends CustomPainter {
  final bool first, last, lit;
  final Color dot, line, hollow;
  _RoutePainter({required this.first, required this.last, required this.dot, required this.line, required this.lit, required this.hollow});

  static const x = 22.0, y = 30.0;

  @override
  void paint(Canvas canvas, Size size) {
    final track = Paint()
      ..color = line
      ..strokeWidth = 2;
    canvas.drawLine(Offset(x, first ? y : 0), Offset(x, last ? y : size.height), track);
    if (lit) canvas.drawCircle(const Offset(x, y), 10, Paint()..color = dot.withValues(alpha: .2));
    canvas.drawCircle(const Offset(x, y), 6, Paint()..color = hollow);
    canvas.drawCircle(
      const Offset(x, y),
      6,
      Paint()
        ..color = dot
        ..style = lit || first ? PaintingStyle.fill : PaintingStyle.stroke
        ..strokeWidth = 2.5,
    );
  }

  @override
  bool shouldRepaint(_RoutePainter old) =>
      old.first != first || old.last != last || old.lit != lit || old.dot != dot || old.line != line || old.hollow != hollow;
}

/// A station's row: a caption, what is there, and the way to change it.
class _Station extends StatelessWidget {
  final String caption;
  final Widget child;
  final Widget? trailing;
  final VoidCallback? onTap;
  const _Station({required this.caption, required this.child, this.trailing, this.onTap});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final body = Padding(
      padding: const EdgeInsets.fromLTRB(4, 12, 12, 12),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  caption,
                  style: TextStyle(fontSize: 11.5, color: p.dim, fontWeight: FontWeight.w500),
                ),
                const SizedBox(height: 3),
                child,
              ],
            ),
          ),
          ?trailing,
        ],
      ),
    );
    if (onTap == null) return body;
    return InkWell(borderRadius: BorderRadius.circular(10), onTap: onTap, hoverColor: p.surface2, child: body);
  }
}

/// This device: everything or only proxied programs, and the rules.
class _DeviceStation extends StatelessWidget {
  final AppState state;
  const _DeviceStation({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final tun = state.setting('tun', true);
    final selected = state.setting('routing.mode', 'all') == 'selected';
    final russia = state.setting('routing.russia_direct', false);
    final rules = !tun
        ? 'в VPN только программы с прокси'
        : selected
        ? 'в VPN только выбранные сайты и программы'
        : russia
        ? 'российские сайты напрямую, остальное в VPN'
        : 'весь трафик идёт в VPN';
    return _Station(
      caption: 'Это устройство',
      onTap: () => Nav.to(context, PageId.routing),
      trailing: Icon(Icons.chevron_right, size: 20, color: p.dim),
      child: Text.rich(
        TextSpan(
          children: [
            TextSpan(
              text: tun ? 'Все приложения' : 'Только прокси',
              style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 14),
            ),
            TextSpan(
              text: ' · $rules',
              style: TextStyle(color: p.muted, fontSize: 12.5),
            ),
          ],
        ),
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
      ),
    );
  }
}

/// The chosen server; a tap opens the quick pick.
class _ServerStation extends StatelessWidget {
  final AppState state;
  const _ServerStation({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final sel = state.selection;
    final n = sel.node;
    final sub = state.subscriptionById(sel.subscription);
    final l = n == null ? null : state.latencyOf(sel.subscription, n.fingerprint);
    return _Station(
      caption: 'Сервер',
      onTap: () => showQuickPick(context, state, onAll: () => Nav.to(context, PageId.servers)),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (l != null && l.ok) ...[
            Text('${l.ms} мс', style: figures(14, color: l.ms < 200 ? okColor : (l.ms < 500 ? warnColor : errColor))),
            const SizedBox(width: 8),
          ],
          Tooltip(
            message: 'Сменить сервер',
            child: Icon(Icons.unfold_more, size: 20, color: p.dim),
          ),
        ],
      ),
      child: n == null
          ? Text(
              sel.isEmpty ? 'Выбрать сервер' : sel.name,
              style: TextStyle(fontWeight: FontWeight.w600, color: p.muted),
            )
          : Row(
              children: [
                CountryBadge(countryOf(n.name, n.server), width: 30),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(cleanNodeName(n.name), style: display(17), overflow: TextOverflow.ellipsis),
                      const SizedBox(height: 3),
                      Row(
                        children: [
                          ProtoBadge(n.protocol),
                          if (sub != null) ...[
                            const SizedBox(width: 6),
                            Flexible(
                              child: Text(
                                sub.displayName,
                                style: TextStyle(color: p.muted, fontSize: 12),
                                overflow: TextOverflow.ellipsis,
                              ),
                            ),
                          ],
                        ],
                      ),
                    ],
                  ),
                ),
              ],
            ),
    );
  }
}

/// The connection is up but traffic does not get through: the server is
/// down, blocked or the network is bad. Says so, instead of "Подключено"
/// alone, and offers the one thing that can help from here.
class _UnresponsiveBanner extends StatelessWidget {
  final AppState state;
  const _UnresponsiveBanner({required this.state});

  @override
  Widget build(BuildContext context) => _Notice(
    color: errColor,
    icon: Icons.cloud_off_outlined,
    text:
        'Сервер не отвечает: связь через него не проходит. Он может быть недоступен или заблокирован. Попробуйте переподключиться или выберите другой сервер.',
    action: Btn(label: 'Переподключить', small: true, onPressed: state.busy ? null : state.reconnect),
  );
}

/// A coloured line on the home page: a warning and what to do about it.
class _Notice extends StatelessWidget {
  final Color color;
  final IconData icon;
  final String? title;
  final String text;
  final Widget? action;
  const _Notice({required this.color, required this.icon, this.title, required this.text, this.action});

  @override
  Widget build(BuildContext context) {
    final body = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (title != null) Text(title!, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
        Text(text, style: TextStyle(fontSize: 12, color: title == null ? null : context.pal.muted)),
      ],
    );
    return Container(
      clipBehavior: Clip.antiAlias,
      decoration: BoxDecoration(color: color.withValues(alpha: .09), borderRadius: BorderRadius.circular(10)),
      child: IntrinsicHeight(
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Container(width: 3, color: color),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.fromLTRB(10, 9, 9, 9),
                child: Row(
                  children: [
                    Icon(icon, size: 17, color: color),
                    const SizedBox(width: 10),
                    Expanded(child: body),
                    if (action != null) ...[const SizedBox(width: 8), action!],
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
