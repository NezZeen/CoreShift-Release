part of '../home_page.dart';

/// The state in words and the colour of its lamp.
(String, Color) _stateTitle(BuildContext context, Status st) => switch (st.state) {
  ConnState.connected => (tr('home.state.connected'), context.pal.okInk),
  ConnState.connecting => (tr('home.state.connecting'), context.pal.accentInk),
  ConnState.disconnecting => (tr('home.state.disconnecting'), context.pal.muted),
  ConnState.failed => (tr('home.state.failed'), context.pal.errInk),
  ConnState.blocked => (tr('home.state.blocked'), context.pal.errInk),
  ConnState.noNetwork => (tr(st.waiting ? 'home.state.waiting' : 'home.state.no_network'), context.pal.warnInk),
  ConnState.idle => (tr('home.state.idle'), context.pal.text),
};

/// The button, the state in big letters, and what to do about it: the
/// part of the page that connects.
class _Hero extends StatelessWidget {
  final AppState state;
  final double size;

  /// The phone: the state goes into a chip above the button instead of big
  /// letters under it.
  final bool compact;
  const _Hero({required this.state, this.size = 184, this.compact = false});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status;
    final sel = state.selection;
    final (title, color) = _stateTitle(context, st);
    final canConnect = state.online && (st.active || (sel.available && !state.busy));
    final muted = TextStyle(color: p.muted, fontSize: 13.5);
    final Widget line = switch (st.state) {
      ConnState.connected when st.since != null => ConnectedTime(
        since: st.since!,
        tun: st.tun,
        systemProxy: !platform.isAndroid && state.setting('proxy.system', false) == true,
        short: isCompact(context),
      ),
      ConnState.failed => Tooltip(
        message: st.error,
        child: Text(
          humanCodedError(st.errorCode, st.errorArgs, st.error),
          textAlign: TextAlign.center,
          maxLines: 3,
          overflow: TextOverflow.ellipsis,
          style: muted,
        ),
      ),
      ConnState.idle when sel.isEmpty => Text(tr('home.hero.pick_first'), style: muted),
      ConnState.idle when !sel.available => Text(
        tr('home.hero.gone', {'name': sel.name}),
        textAlign: TextAlign.center,
        style: TextStyle(color: p.warnInk, fontSize: 13.5),
      ),
      ConnState.idle => Text(tr('home.hero.tap'), style: muted),
      ConnState.blocked => Text(tr('home.hero.blocked'), textAlign: TextAlign.center, style: muted),
      ConnState.noNetwork => Text(tr(st.waiting ? 'home.hero.wait_net' : 'home.hero.resumes'), textAlign: TextAlign.center, style: muted),
      _ => const SizedBox(),
    };
    if (compact) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Center(
            child: _StateChip(title: title, color: color, lit: st.state == ConnState.connected),
          ),
          const SizedBox(height: 26),
          Center(
            child: ConnectButton(state: st.state, enabled: canConnect, onTap: state.toggleConnect, size: size),
          ),
          const SizedBox(height: 18),
          ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 20),
            child: Center(child: line),
          ),
        ],
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Center(
          child: ConnectButton(state: st.state, enabled: canConnect, onTap: state.toggleConnect, size: size),
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

/// The phone's state above the button: a lamp and the word, in a pill tinted
/// with the state's colour.
class _StateChip extends StatelessWidget {
  final String title;
  final Color color;
  final bool lit;
  const _StateChip({required this.title, required this.color, required this.lit});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 8),
      decoration: BoxDecoration(
        color: lit ? Color.alphaBlend(okColor.withValues(alpha: .12), p.surface) : p.surface,
        borderRadius: BorderRadius.circular(99),
        border: Border.all(color: lit ? okColor.withValues(alpha: .5) : p.border),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              color: lit ? color : Colors.transparent,
              border: Border.all(color: color, width: 1.5),
            ),
          ),
          const SizedBox(width: 8),
          Text(
            title,
            style: TextStyle(color: color, fontSize: 14, fontWeight: FontWeight.w600),
          ),
        ],
      ),
    );
  }
}

/// The phone's chosen server: one card under the button; a tap opens the
/// quick pick, as the server's station does on the desktop.
class _ServerChip extends StatelessWidget {
  final AppState state;
  const _ServerChip({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final sel = state.selection;
    final n = sel.node;
    final sub = state.subscriptionById(sel.subscription);
    final l = n == null ? null : state.latencyOf(sel.subscription, n.fingerprint);
    final detail = <InlineSpan>[
      if (n != null) TextSpan(text: protocolLabel(n.protocol)),
      if (l != null && l.ok) ...[
        const TextSpan(text: ' · '),
        TextSpan(
          text: tr('home.ms', {'ms': l.ms}),
          style: TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, color: p.ink(l.ms < 200 ? okColor : (l.ms < 500 ? warnColor : errColor))),
        ),
      ],
      if (sub != null) TextSpan(text: ' · ${sub.displayName}'),
    ];
    return Material(
      color: p.surface2,
      borderRadius: BorderRadius.circular(16),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () => showQuickPick(context, state, onAll: () => Nav.to(context, PageId.servers)),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(14, 12, 10, 12),
          child: Row(
            children: [
              if (n != null) ...[CountryBadge(countryOf(n.name, n.server), width: 40), const SizedBox(width: 14)],
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      n == null ? (sel.isEmpty ? tr('home.server.choose') : sel.name) : cleanNodeName(n.name),
                      style: display(17, weight: FontWeight.w700, color: n == null ? p.muted : null),
                      overflow: TextOverflow.ellipsis,
                    ),
                    if (detail.isNotEmpty) ...[
                      const SizedBox(height: 3),
                      Text.rich(
                        TextSpan(children: detail),
                        style: TextStyle(fontSize: 13, color: p.muted),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ],
                  ],
                ),
              ),
              Tooltip(
                message: tr('home.server.change'),
                child: Icon(Icons.chevron_right, size: 22, color: p.muted),
              ),
            ],
          ),
        ),
      ),
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
    final systemProxy = !platform.isAndroid && state.setting('proxy.system', false) == true;
    final selected = state.setting('routing.mode', 'all') == 'selected';
    final russia = state.setting('routing.russia_direct', false);
    final rules = !tun
        ? (systemProxy ? tr('home.device.rules.system_proxy') : tr('home.device.rules.proxy'))
        : selected
        ? tr('home.device.rules.selected')
        : russia
        ? tr('home.device.rules.russia')
        : tr('home.device.rules.all');
    return _Station(
      caption: tr('home.device.caption'),
      onTap: () => Nav.to(context, PageId.routing),
      trailing: Icon(Icons.chevron_right, size: 20, color: p.dim),
      child: Text.rich(
        TextSpan(
          children: [
            TextSpan(
              text: tun ? tr('home.device.all_apps') : (systemProxy ? tr('home.device.system_proxy') : tr('home.device.proxy_only')),
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
      caption: tr('home.server.caption'),
      onTap: () => showQuickPick(context, state, onAll: () => Nav.to(context, PageId.servers)),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (l != null && l.ok) ...[
            Text(tr('home.ms', {'ms': l.ms}), style: figures(14, color: p.ink(l.ms < 200 ? okColor : (l.ms < 500 ? warnColor : errColor)))),
            const SizedBox(width: 8),
          ],
          Tooltip(
            message: tr('home.server.change'),
            child: Icon(Icons.unfold_more, size: 20, color: p.dim),
          ),
        ],
      ),
      child: n == null
          ? Text(
              sel.isEmpty ? tr('home.server.choose') : sel.name,
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
/// down, blocked or the network is bad. Says which, as the engine found
/// looking past the tunnel, with the server's name, instead of "Подключено"
/// alone, and offers what can help from here: another server, unless the
/// network is at fault, and reconnecting.
class _UnresponsiveBanner extends StatelessWidget {
  final AppState state;
  const _UnresponsiveBanner({required this.state});

  @override
  Widget build(BuildContext context) {
    final problem = state.serverProblem;
    final (title, text) = AppState.serverProblemText(problem, cleanNodeName(state.status.node));
    final offline = problem == 'offline';
    return _Notice(
      color: errColor,
      icon: offline ? Icons.wifi_off_outlined : Icons.cloud_off_outlined,
      title: title,
      text: text,
      actions: [
        if (!offline)
          Btn(
            label: tr('home.btn.another_server'),
            small: true,
            kind: BtnKind.primary,
            onPressed: state.busy ? null : () => showQuickPick(context, state, onAll: () => Nav.to(context, PageId.servers)),
          ),
        Btn(label: tr('home.btn.reconnect'), small: true, onPressed: state.busy ? null : state.reconnect),
        Btn(label: tr('home.btn.check_all'), small: true, icon: Icons.fact_check_outlined, onPressed: () => showCheckup(context, state)),
      ],
    );
  }
}

/// The device has no network: CoreShift waits for it, and says so rather
/// than "Подключено" or an error. While it waits the user can cancel, or
/// turn a held VPN off to use the network without it once it is back.
class _NoNetworkBanner extends StatelessWidget {
  final AppState state;
  const _NoNetworkBanner({required this.state});

  @override
  Widget build(BuildContext context) {
    final waiting = state.status.waiting;
    return _Notice(
      color: warnColor,
      icon: Icons.wifi_off_outlined,
      title: tr('home.nonet.title'),
      text: waiting ? tr('home.nonet.waiting') : tr('home.nonet.lost'),
      actions: [Btn(label: waiting ? tr('home.nonet.cancel') : tr('home.nonet.disconnect'), small: true, onPressed: state.busy ? null : state.disconnect)],
    );
  }
}

/// Android will not let CoreShift run its VPN ([AppState.vpnBlocked]): it
/// refused, often without asking because another app is the "always-on" VPN,
/// or turned the VPN off for another app. Pressing «Подключить» again does
/// not help; turning that app's «Постоянная VPN» off in the settings does.
class _VpnBlockedBanner extends StatelessWidget {
  final AppState state;
  const _VpnBlockedBanner({required this.state});

  @override
  Widget build(BuildContext context) {
    final refusal = state.vpnRefusal;
    final refused = refusal == null ? null : vpnRefusedText(unasked: refusal == platform.VpnConsent.unasked);
    return _Notice(
      color: errColor,
      icon: Icons.vpn_lock_outlined,
      title: refused?.title ?? vpnRevokedText.title,
      text: refused?.text ?? vpnRevokedText.text,
      actions: [
        Btn(label: tr('home.blocked.settings'), small: true, kind: BtnKind.primary, onPressed: state.openVpnSettings),
        if (refusal != null) Btn(label: tr('home.btn.hide'), small: true, onPressed: state.dismissVpnRefusal),
      ],
    );
  }
}

/// A coloured line on the home page: a warning and what to do about it.
class _Notice extends StatelessWidget {
  final Color color;
  final IconData icon;
  final String? title;
  final String text;
  final Widget? action;

  /// Several ways out, in a row under the text rather than beside it.
  final List<Widget> actions;
  const _Notice({required this.color, required this.icon, this.title, required this.text, this.action, this.actions = const []});

  @override
  Widget build(BuildContext context) {
    final body = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (title != null) Text(title!, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
        Text(text, style: TextStyle(fontSize: 12, color: title == null ? null : context.pal.muted)),
        if (actions.isNotEmpty) ...[const SizedBox(height: 8), Wrap(spacing: 8, runSpacing: 6, children: actions)],
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
