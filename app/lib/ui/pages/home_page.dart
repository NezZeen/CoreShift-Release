import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';

import '../../api/models.dart';
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../shell.dart';
import 'servers_page.dart' show showAddSubscription;
import '../theme.dart';
import '../widgets.dart';

class HomePage extends StatelessWidget {
  final AppState state;
  const HomePage({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    if (state.subscriptions.isEmpty) return PageFrame(children: [_Welcome(state: state)]);
    if (isCompact(context)) {
      // Room above for the page to sit mid-screen when connected, the
      // tallest it gets, so the button stays put as the numbers come in.
      return LayoutBuilder(
        builder: (context, c) => PageFrame(
          children: [
            SizedBox(height: max(0, (c.maxHeight - 560) / 2)),
            _CompactHome(state: state),
          ],
        ),
      );
    }
    return PageFrame(
      children: [
        LayoutBuilder(
          builder: (context, c) {
            final hero = Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                _Hero(state: state),
                const SizedBox(height: 18),
                _SpeedCard(state: state),
              ],
            );
            final right = Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                _CoreCard(state: state),
                const SizedBox(height: 18),
                _Stats(state: state, columns: c.maxWidth >= 1000 ? 4 : 2),
                const SizedBox(height: 18),
                _LatencyCard(state: state),
              ],
            );
            if (c.maxWidth < 1000) {
              return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [hero, const SizedBox(height: 18), right]);
            }
            return Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SizedBox(width: max(340, c.maxWidth * .4), child: hero),
                const SizedBox(width: 18),
                Expanded(child: right),
              ],
            );
          },
        ),
      ],
    );
  }
}

class _Hero extends StatelessWidget {
  final AppState state;
  const _Hero({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status;
    final sel = state.selection;
    final (title, color) = switch (st.state) {
      ConnState.connected => ('Подключено', okColor),
      ConnState.connecting => ('Подключение…', accent),
      ConnState.disconnecting => ('Отключение…', p.muted),
      ConnState.failed => ('Ошибка подключения', errColor),
      ConnState.idle => ('Отключено', p.text),
    };
    final canConnect = state.online && (st.active || (sel.available && !state.busy));
    final tunOk = state.info.tunAvailable;
    final tun = state.setting('tun', false);

    return Panel(
      padding: const EdgeInsets.fromLTRB(22, 30, 22, 22),
      child: Column(
        children: [
          _ConnectButton(state: st.state, enabled: canConnect, onTap: state.toggleConnect),
          const SizedBox(height: 18),
          Text(
            title,
            style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600, color: st.state == ConnState.idle ? p.text : color),
          ),
          const SizedBox(height: 4),
          SizedBox(
            height: 38,
            child: switch (st.state) {
              ConnState.connected when st.since != null => _Elapsed(since: st.since!, tun: st.tun),
              ConnState.failed => Tooltip(
                message: st.error,
                child: Text(
                  humanError(st.error),
                  textAlign: TextAlign.center,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: p.muted, fontSize: 12),
                ),
              ),
              ConnState.idle when sel.isEmpty => Text('Сначала выберите сервер', style: TextStyle(color: p.muted, fontSize: 13)),
              ConnState.idle when !sel.available => Text('Сервер «${sel.name}» пропал из подписки', style: const TextStyle(color: warnColor, fontSize: 13)),
              ConnState.idle => Text('Нажмите, чтобы подключиться', style: TextStyle(color: p.muted, fontSize: 13)),
              _ => const SizedBox(),
            },
          ),
          const SizedBox(height: 8),
          _NodePick(state: state),
          if (st.settingsPending) ...[const SizedBox(height: 12), _PendingBanner(state: state)],
          const SizedBox(height: 16),
          _OptRow(
            label: 'Через VPN',
            child: Seg<bool>(
              value: tun,
              options: const [(true, 'Все приложения'), (false, 'Только прокси')],
              disabled: tunOk ? const {} : const {true},
              tooltips: {
                true: tunOk ? 'Весь трафик компьютера идёт через VPN (режим TUN), DNS защищён' : state.info.tunUnavailable,
                false: 'Через VPN идут только программы, где вручную указан прокси SOCKS5 127.0.0.1:17890',
              },
              onChanged: (v) => state.updateSettings((s) => s['tun'] = v),
            ),
          ),
        ],
      ),
    );
  }
}

/// The home page on a phone: the button, the server, and while connected
/// the few numbers worth a glance. The charts, the core queue and the mode
/// switch stay on the desktop; the mode is in the settings.
class _CompactHome extends StatelessWidget {
  final AppState state;
  const _CompactHome({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status;
    final sel = state.selection;
    final (title, color) = switch (st.state) {
      ConnState.connected => ('Подключено', okColor),
      ConnState.connecting => ('Подключение…', accent),
      ConnState.disconnecting => ('Отключение…', p.muted),
      ConnState.failed => ('Ошибка подключения', errColor),
      ConnState.idle => ('Отключено', p.text),
    };
    final canConnect = state.online && (st.active || (sel.available && !state.busy));
    final muted = TextStyle(color: p.muted, fontSize: 13);
    final Widget line = switch (st.state) {
      ConnState.connected when st.since != null => _Elapsed(since: st.since!, tun: st.tun, short: true),
      ConnState.failed => Text(humanError(st.error), textAlign: TextAlign.center, maxLines: 3, overflow: TextOverflow.ellipsis, style: muted),
      ConnState.idle when sel.isEmpty => Text('Сначала выберите сервер', style: muted),
      ConnState.idle when !sel.available => Text(
        'Сервер «${sel.name}» пропал из подписки',
        textAlign: TextAlign.center,
        style: const TextStyle(color: warnColor, fontSize: 13),
      ),
      ConnState.idle => Text('Нажмите, чтобы подключиться', style: muted),
      _ => const SizedBox(),
    };
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const SizedBox(height: 16),
        Center(
          child: _ConnectButton(state: st.state, enabled: canConnect, onTap: state.toggleConnect, size: 150),
        ),
        const SizedBox(height: 26),
        Text(
          title,
          textAlign: TextAlign.center,
          style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600, color: st.state == ConnState.idle ? p.text : color),
        ),
        const SizedBox(height: 4),
        ConstrainedBox(
          constraints: const BoxConstraints(minHeight: 20),
          child: Center(child: line),
        ),
        const SizedBox(height: 22),
        if (st.settingsPending) ...[_PendingBanner(state: state), const SizedBox(height: 10)],
        _NodePick(state: state),
        if (st.state == ConnState.connected) ...[const SizedBox(height: 10), _CompactStats(state: state)],
        _SubscriptionLine(state: state),
      ],
    );
  }
}

/// While connected: speed and ping in one row, the running core under them.
class _CompactStats extends StatelessWidget {
  final AppState state;
  const _CompactStats({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status;
    final (up, down) = state.speed.isEmpty ? (0, 0) : state.speed.last;
    final pings = _latencyData(state).$1;
    final manual = state.setting('cores.mode', 'auto') == 'manual';
    final chain = st.chain;
    final core = st.core;
    final onBackup = !manual && chain.length > 1 && core.isNotEmpty && core != chain.first;
    final failures = st.failed;

    Widget metric(IconData icon, Color color, String label, String value) => Expanded(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 13, color: color),
              const SizedBox(width: 4),
              Text(label, style: TextStyle(fontSize: 11, color: p.muted)),
            ],
          ),
          const SizedBox(height: 3),
          Text(
            value,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 15, fontWeight: FontWeight.w500),
          ),
        ],
      ),
    );

    return Panel(
      padding: const EdgeInsets.fromLTRB(14, 12, 10, 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              metric(Icons.south, okColor, 'Загрузка', formatRate(down)),
              metric(Icons.north, accent, 'Отдача', formatRate(up)),
              metric(Icons.monitor_heart_outlined, p.muted, 'Пинг', pings.isEmpty ? '—' : '${pings.last} мс'),
            ],
          ),
          Divider(height: 20, color: p.border),
          InkWell(
            borderRadius: BorderRadius.circular(8),
            onTap: () => Nav.to(context, PageId.cores),
            child: Row(
              children: [
                if (core.isNotEmpty) CoreLogo(core, size: 22) else CoreLogo('?', size: 22, off: true),
                const SizedBox(width: 10),
                Expanded(
                  child: Text.rich(
                    TextSpan(
                      children: [
                        TextSpan(
                          text: core.isEmpty ? 'Ядро запускается…' : coreStyle(core).name,
                          style: const TextStyle(fontWeight: FontWeight.w500),
                        ),
                        if (failures.isNotEmpty)
                          TextSpan(
                            text: '  ${failures.keys.map((k) => coreStyle(k).name).join(', ')}: сбой',
                            style: const TextStyle(fontSize: 12, color: warnColor),
                          )
                        else if (onBackup)
                          TextSpan(
                            text: '  резерв',
                            style: TextStyle(fontSize: 12, color: p.muted),
                          ),
                      ],
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (onBackup)
                  Btn(
                    label: 'Вернуть ${coreStyle(chain.first).name}',
                    small: true,
                    loading: state.returning,
                    onPressed: state.busy ? null : state.returnToPrimary,
                  )
                else ...[
                  Pill(manual ? 'ВРУЧНУЮ' : 'АВТОСВАП', color: manual ? p.muted : swapColor),
                  Icon(Icons.chevron_right, size: 18, color: p.dim),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// The selected subscription's days and traffic left, in one quiet line
/// that turns yellow or red when they run out.
class _SubscriptionLine extends StatelessWidget {
  final AppState state;
  const _SubscriptionLine({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final info = state.subscriptionById(state.selection.subscription)?.info;
    if (info == null) return const SizedBox();
    final days = info.expire?.difference(DateTime.now()).inDays;
    final parts = <String>[
      if (days != null) days < 0 ? 'подписка истекла' : 'подписка ещё $days дн.',
      if (info.total > 0) '${formatBytes(info.used)} из ${formatBytes(info.total)}',
    ];
    if (parts.isEmpty) return const SizedBox();
    final bad = (days != null && days < 0) || (info.total > 0 && info.used / info.total > .9);
    final soon = days != null && days < 7;
    return Padding(
      padding: const EdgeInsets.only(top: 14),
      child: Text(
        parts.join(' · '),
        textAlign: TextAlign.center,
        style: TextStyle(fontSize: 12, color: bad ? errColor : (soon ? warnColor : p.dim)),
      ),
    );
  }
}

class _PendingBanner extends StatelessWidget {
  final AppState state;
  const _PendingBanner({required this.state});

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
    decoration: BoxDecoration(
      color: warnColor.withValues(alpha: .08),
      borderRadius: BorderRadius.circular(10),
      border: Border.all(color: warnColor.withValues(alpha: .35)),
    ),
    child: Row(
      children: [
        const Icon(Icons.info_outline, size: 16, color: warnColor),
        const SizedBox(width: 9),
        const Expanded(child: Text('Настройки изменены и применятся после переподключения', style: TextStyle(fontSize: 12))),
        Btn(label: 'Применить', small: true, onPressed: state.busy ? null : state.reconnect),
      ],
    ),
  );
}

/// The first screen before any subscription is added: what to do, in order.
class _Welcome extends StatelessWidget {
  final AppState state;
  const _Welcome({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    Widget step(int n, String title, String text) => Padding(
      padding: const EdgeInsets.only(bottom: 14),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 26,
            height: 26,
            alignment: Alignment.center,
            decoration: BoxDecoration(color: accent.withValues(alpha: .15), shape: BoxShape.circle),
            child: Text(
              '$n',
              style: const TextStyle(color: accent, fontWeight: FontWeight.w700, fontSize: 13),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
                const SizedBox(height: 2),
                Text(text, style: TextStyle(color: p.muted, fontSize: 13)),
              ],
            ),
          ),
        ],
      ),
    );
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560),
        child: Panel(
          padding: const EdgeInsets.fromLTRB(28, 30, 28, 26),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('Добро пожаловать в CoreShift', style: TextStyle(fontSize: 22, fontWeight: FontWeight.w600)),
              const SizedBox(height: 6),
              Text('Три шага до подключения:', style: TextStyle(color: p.muted)),
              const SizedBox(height: 22),
              step(1, 'Добавьте подписку', 'Скопируйте ссылку на подписку из бота, панели или письма провайдера и вставьте её сюда.'),
              step(2, 'Выберите сервер', 'CoreShift сам замерит пинг и подскажет самый быстрый.'),
              step(3, 'Нажмите кнопку подключения', 'Через VPN пойдут все приложения. Если одно ядро перестанет работать, CoreShift переключится на другое.'),
              const SizedBox(height: 8),
              Btn(label: 'Добавить подписку', icon: Icons.add, kind: BtnKind.primary, onPressed: () => showAddSubscription(context, state)),
            ],
          ),
        ),
      ),
    );
  }
}

class _OptRow extends StatelessWidget {
  final String label;
  final Widget child;
  const _OptRow({required this.label, required this.child});

  @override
  Widget build(BuildContext context) => Row(
    children: [
      Text(label, style: TextStyle(color: context.pal.muted, fontSize: 13)),
      const SizedBox(width: 12),
      Expanded(
        child: Align(
          alignment: Alignment.centerRight,
          child: FittedBox(fit: BoxFit.scaleDown, child: child),
        ),
      ),
    ],
  );
}

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
        onTap: () => Nav.to(context, PageId.servers),
        hoverColor: p.surface3,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
          child: Row(
            children: [
              Container(
                width: 34,
                height: 26,
                alignment: Alignment.center,
                decoration: BoxDecoration(color: p.surface3, borderRadius: BorderRadius.circular(7)),
                child: Icon(Icons.dns_outlined, size: 15, color: p.muted),
              ),
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
                            n.name,
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
              Icon(Icons.chevron_right, color: p.dim),
            ],
          ),
        ),
      ),
    );
  }
}

class _CoreCard extends StatelessWidget {
  final AppState state;
  const _CoreCard({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status;
    final manual = state.setting('cores.mode', 'auto') == 'manual';
    final chain = st.active ? st.chain : (state.selection.node?.cores ?? const <String>[]);
    final current = st.active && st.core.isNotEmpty ? st.core : (chain.isNotEmpty ? chain.first : '');
    final prio = state.setting<List>('cores.priority', const []).cast<String>();
    final unsupported = [
      for (final k in prio)
        if (!chain.contains(k) && state.info.installed(k)) k,
    ];

    String subtitle;
    if (current.isEmpty) {
      subtitle = state.selection.node == null ? 'Сервер не выбран' : 'Ни одно установленное ядро не поддерживает этот сервер';
    } else if (st.state == ConnState.connected) {
      final pos = chain.indexOf(current);
      subtitle = pos <= 0 ? 'основное ядро' : 'резерв №$pos';
    } else if (st.state == ConnState.connecting) {
      subtitle = 'запускается…';
    } else {
      subtitle = 'будет запущено первым';
    }

    final failures = st.active ? st.failed : const <String, String>{};
    // Running on a backup: offer to move back without waiting for the timer.
    final onBackup = st.state == ConnState.connected && !manual && chain.length > 1 && current != chain.first;
    final returnAfter = state.setting('cores.return_after_min', 0);
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PanelTitle(
            st.state == ConnState.connected ? 'Активное ядро' : 'Будет запущено',
            trailing: Pill(manual ? 'ВРУЧНУЮ' : 'АВТОСВАП', color: manual ? p.muted : swapColor),
          ),
          Row(
            children: [
              if (current.isNotEmpty) CoreLogo(current, size: 46) else CoreLogo('?', size: 46, off: true),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(current.isEmpty ? '—' : coreStyle(current).name, style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
                    Text(subtitle, style: TextStyle(fontSize: 12, color: p.muted)),
                  ],
                ),
              ),
              if (onBackup)
                Tooltip(
                  message:
                      'Сначала ${coreStyle(chain.first).name} проверяется в фоне; переключимся, только если оно работает.'
                      '${returnAfter > 0 ? ' Само это произойдёт через $returnAfter мин после сбоя.' : ''}',
                  child: Btn(
                    label: 'Вернуть ${coreStyle(chain.first).name}',
                    icon: Icons.undo,
                    small: true,
                    loading: state.returning,
                    onPressed: state.busy ? null : state.returnToPrimary,
                  ),
                ),
            ],
          ),
          const SizedBox(height: 14),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
            decoration: BoxDecoration(
              color: p.surface2,
              borderRadius: BorderRadius.circular(10),
              border: Border.all(color: p.border),
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(failures.isEmpty ? Icons.swap_horiz : Icons.warning_amber_rounded, size: 17, color: failures.isEmpty ? swapColor : warnColor),
                const SizedBox(width: 9),
                Expanded(
                  child: Tooltip(
                    message: failures.entries.map((e) => '${coreStyle(e.key).name}: ${e.value}').join('\n'),
                    child: Text(
                      failures.isNotEmpty
                          ? '${failures.keys.map((k) => coreStyle(k).name).join(', ')} '
                                '${failures.length == 1 ? 'перестало' : 'перестали'} работать, CoreShift переключился на резервное ядро. '
                                'Наведите, чтобы увидеть причину.'
                          : manual
                          ? 'Ручной режим: работает только выбранное ядро, без переключения при сбоях.'
                          : chain.length > 1
                          ? 'Если ядро перестанет работать, CoreShift сам переключится на следующее — соединение не прервётся.'
                          : 'Этот сервер поддерживает только одно ядро — переключаться некуда.',
                      style: TextStyle(fontSize: 13, color: p.muted),
                    ),
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 16),
          const SectionLabel('Очередь ядер'),
          Wrap(
            spacing: 6,
            runSpacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              for (final (i, k) in chain.indexed) ...[
                if (i > 0) Icon(Icons.arrow_forward, size: 15, color: p.dim),
                _ChainChip(
                  kind: k,
                  status: failures.containsKey(k) ? _ChipStatus.failed : (st.active && k == st.core ? _ChipStatus.active : _ChipStatus.standby),
                ),
              ],
              for (final k in unsupported) _ChainChip(kind: k, status: _ChipStatus.skipped),
            ],
          ),
          const SizedBox(height: 16),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              if (st.active) Btn(label: 'Переподключить', icon: Icons.refresh, small: true, onPressed: state.busy ? null : state.reconnect),
              Btn(label: 'Настроить автосвап', icon: Icons.tune, small: true, kind: BtnKind.ghost, onPressed: () => Nav.to(context, PageId.cores)),
            ],
          ),
        ],
      ),
    );
  }
}

enum _ChipStatus { active, standby, failed, skipped }

class _ChainChip extends StatelessWidget {
  final String kind;
  final _ChipStatus status;
  const _ChainChip({required this.kind, required this.status});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final s = coreStyle(kind);
    final (label, labelColor) = switch (status) {
      _ChipStatus.active => ('активно', okColor),
      _ChipStatus.standby => ('резерв', p.dim),
      _ChipStatus.failed => ('сбой', errColor),
      _ChipStatus.skipped => ('не поддерживает', p.dim),
    };
    return Opacity(
      opacity: switch (status) {
        _ChipStatus.skipped => .45,
        _ChipStatus.failed => .8,
        _ => 1,
      },
      child: Container(
        padding: const EdgeInsets.fromLTRB(6, 6, 12, 6),
        decoration: BoxDecoration(
          color: p.surface2,
          borderRadius: BorderRadius.circular(99),
          border: Border.all(
            color: switch (status) {
              _ChipStatus.active => s.color,
              _ChipStatus.failed => errColor.withValues(alpha: .5),
              _ => p.border2,
            },
          ),
          boxShadow: status == _ChipStatus.active ? [BoxShadow(color: s.color.withValues(alpha: .18), spreadRadius: 3)] : null,
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            CoreLogo(kind, size: 22),
            const SizedBox(width: 8),
            Text(s.name, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w500)),
            const SizedBox(width: 8),
            Text(
              label,
              style: TextStyle(fontSize: 11, color: labelColor, fontWeight: FontWeight.w500),
            ),
          ],
        ),
      ),
    );
  }
}

class _Stats extends StatelessWidget {
  final AppState state;
  final int columns;
  const _Stats({required this.state, required this.columns});

  @override
  Widget build(BuildContext context) {
    final st = state.status;
    final data = _latencyData(state).$1;
    final lat = data.isEmpty ? null : data.last;
    final info = state.subscriptionById(state.selection.subscription)?.info;
    final expire = info?.expire;
    final daysLeft = expire?.difference(DateTime.now()).inDays;
    final tiles = <(IconData, String, String, String, Color?)>[
      (Icons.monitor_heart_outlined, 'Пинг', lat == null ? '—' : '$lat', lat == null ? '' : 'мс', null),
      (
        Icons.data_usage,
        'Трафик подписки',
        info == null || (info.used == 0 && info.total == 0) ? '—' : formatBytes(info.used),
        info != null && info.total > 0 ? '/ ${formatBytes(info.total)}' : '',
        info != null && info.total > 0 && info.used / info.total > .9 ? errColor : null,
      ),
      (
        Icons.event_outlined,
        'Подписка',
        daysLeft == null ? '—' : (daysLeft < 0 ? 'истекла' : '$daysLeft'),
        daysLeft == null || daysLeft < 0 ? '' : 'дн. осталось',
        daysLeft == null ? null : (daysLeft < 0 ? errColor : (daysLeft < 7 ? warnColor : null)),
      ),
      (Icons.swap_horiz, 'Смен ядра', st.active ? '${state.swaps}' : '—', '', null),
    ];
    return LayoutBuilder(
      builder: (context, c) {
        final w = (c.maxWidth - 12 * (columns - 1)) / columns;
        return Wrap(
          spacing: 12,
          runSpacing: 12,
          children: [
            for (final (icon, k, v, unit, color) in tiles)
              SizedBox(
                width: w,
                child: _StatTile(icon: icon, label: k, value: v, unit: unit, color: color),
              ),
          ],
        );
      },
    );
  }
}

class _StatTile extends StatelessWidget {
  final IconData icon;
  final String label;
  final String value;
  final String unit;
  final Color? color;
  const _StatTile({required this.icon, required this.label, required this.value, required this.unit, this.color});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 13),
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: p.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 14, color: p.muted),
              const SizedBox(width: 6),
              Flexible(
                child: Text(
                  label,
                  style: TextStyle(fontSize: 12, color: p.muted),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          const SizedBox(height: 5),
          Text.rich(
            TextSpan(
              children: [
                TextSpan(
                  text: value,
                  style: TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 19, fontWeight: FontWeight.w500, color: color),
                ),
                if (unit.isNotEmpty)
                  TextSpan(
                    text: ' $unit',
                    style: TextStyle(fontSize: 12, color: p.muted),
                  ),
              ],
            ),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
        ],
      ),
    );
  }
}

class _SpeedCard extends StatelessWidget {
  final AppState state;
  const _SpeedCard({required this.state});

  @override
  Widget build(BuildContext context) {
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
            Text(
              active ? formatRate(rate) : '—',
              style: TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 17, fontWeight: FontWeight.w500),
            ),
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
            sub: 'через VPN, последние 2 минуты',
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
          Wrap(spacing: 28, runSpacing: 8, children: [metric(Icons.south, okColor, 'Загрузка', down), metric(Icons.north, accent, 'Отдача', up)]),
          const SizedBox(height: 12),
          SizedBox(
            height: 110,
            child: data.length < 2
                ? Center(
                    child: Text(active ? 'Собираем данные…' : 'Появится после подключения', style: TextStyle(color: p.dim, fontSize: 12)),
                  )
                : CustomPaint(
                    painter: _SpeedPainter(data, grid: p.border, label: p.dim),
                    size: Size.infinite,
                  ),
          ),
        ],
      ),
    );
  }
}

class _SpeedPainter extends CustomPainter {
  final List<(int, int)> data;
  final Color grid;
  final Color label;
  _SpeedPainter(this.data, {required this.grid, required this.label});

  @override
  void paint(Canvas canvas, Size size) {
    final peak = data.fold<int>(0, (m, e) => max(m, max(e.$1, e.$2)));
    final maxV = max(peak * 1.2, 125000.0); // at least 1 Mbit/s tall
    const left = 64.0;
    final w = size.width - left;
    final h = size.height - 4;
    final gridPaint = Paint()
      ..color = grid
      ..strokeWidth = 1;
    for (var i = 0; i <= 2; i++) {
      final y = h - h * i / 2;
      canvas.drawLine(Offset(left, y), Offset(size.width, y), gridPaint);
      final tp = TextPainter(
        text: TextSpan(
          text: i == 0 ? '0' : formatRate((maxV * i / 2).round()),
          style: TextStyle(color: label, fontSize: 10, fontFamily: monoFont),
        ),
        textDirection: TextDirection.ltr,
      )..layout();
      tp.paint(canvas, Offset(0, (y - tp.height / 2).clamp(0, size.height - tp.height)));
    }
    const slots = AppState.speedKeep;
    final step = w / (slots - 1);
    final start = slots - data.length;
    Path line(int Function((int, int)) pick) {
      final path = Path();
      for (var i = 0; i < data.length; i++) {
        final pt = Offset(left + (start + i) * step, h - h * pick(data[i]) / maxV);
        i == 0 ? path.moveTo(pt.dx, pt.dy) : path.lineTo(pt.dx, pt.dy);
      }
      return path;
    }

    final down = line((e) => e.$2);
    final fill = Path.from(down)
      ..lineTo(left + (slots - 1) * step, h)
      ..lineTo(left + start * step, h)
      ..close();
    canvas.drawPath(
      fill,
      Paint()
        ..shader = LinearGradient(
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
          colors: [okColor.withValues(alpha: .25), okColor.withValues(alpha: 0)],
        ).createShader(Rect.fromLTWH(0, 0, size.width, h)),
    );
    final stroke = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2
      ..strokeJoin = StrokeJoin.round;
    canvas.drawPath(down, stroke..color = okColor);
    canvas.drawPath(line((e) => e.$1), stroke..color = accent.withValues(alpha: .9));
  }

  @override
  bool shouldRepaint(_SpeedPainter old) => true;
}

class _LatencyCard extends StatelessWidget {
  final AppState state;
  const _LatencyCard({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final (data, sub) = _latencyData(state);
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PanelTitle('Пинг', sub: sub),
          SizedBox(
            height: 130,
            child: data.length < 2
                ? Center(
                    child: Text(state.status.active ? 'Собираем данные…' : 'Появится после подключения', style: TextStyle(color: p.dim, fontSize: 12)),
                  )
                : CustomPaint(
                    painter: _LatencyPainter(data, grid: p.border, label: p.dim),
                    size: Size.infinite,
                  ),
          ),
        ],
      ),
    );
  }
}

/// The connection's latency history and what it measures. Pings of the
/// server match the node list; when the server answers none, the health
/// checks through the core stand in, which time a whole HTTP request.
(List<int>, String) _latencyData(AppState state) {
  if (!state.status.active) return (const [], 'пинг до сервера');
  if (state.pings.isNotEmpty) {
    return (state.pings, state.pingMethod == 'tcp' ? 'время TCP-подключения к серверу' : 'ICMP-пинг до сервера');
  }
  if (state.pingError.isNotEmpty) return (state.latencies, 'сервер не отвечает на пинг — по проверкам связи через ядро');
  return (const [], 'пинг до сервера');
}

class _LatencyPainter extends CustomPainter {
  final List<int> data;
  final Color grid;
  final Color label;
  _LatencyPainter(this.data, {required this.grid, required this.label});

  @override
  void paint(Canvas canvas, Size size) {
    final maxV = (data.reduce(max) * 1.25).clamp(100, 1e9).toDouble();
    const left = 36.0;
    final w = size.width - left;
    final h = size.height - 4;
    final gridPaint = Paint()
      ..color = grid
      ..strokeWidth = 1;
    for (var i = 0; i <= 2; i++) {
      final y = h - h * i / 2;
      canvas.drawLine(Offset(left, y), Offset(size.width, y), gridPaint);
      final tp = TextPainter(
        text: TextSpan(
          text: '${(maxV * i / 2).round()}',
          style: TextStyle(color: label, fontSize: 10, fontFamily: monoFont),
        ),
        textDirection: TextDirection.ltr,
      )..layout();
      tp.paint(canvas, Offset(0, (y - tp.height / 2).clamp(0, size.height - tp.height)));
    }
    const slots = 60;
    final step = w / (slots - 1);
    final start = slots - data.length;
    final path = Path();
    for (var i = 0; i < data.length; i++) {
      final pt = Offset(left + (start + i) * step, h - h * data[i] / maxV);
      i == 0 ? path.moveTo(pt.dx, pt.dy) : path.lineTo(pt.dx, pt.dy);
    }
    final fill = Path.from(path)
      ..lineTo(left + (slots - 1) * step, h)
      ..lineTo(left + start * step, h)
      ..close();
    canvas.drawPath(
      fill,
      Paint()
        ..shader = LinearGradient(
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
          colors: [okColor.withValues(alpha: .25), okColor.withValues(alpha: 0)],
        ).createShader(Rect.fromLTWH(0, 0, size.width, h)),
    );
    canvas.drawPath(
      path,
      Paint()
        ..color = okColor
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2
        ..strokeJoin = StrokeJoin.round,
    );
  }

  @override
  bool shouldRepaint(_LatencyPainter old) => true;
}
