import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../api/models.dart';
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../support.dart';
import '../theme.dart';
import '../widgets.dart';

class ServersPage extends StatefulWidget {
  final AppState state;
  const ServersPage({super.key, required this.state});

  @override
  State<ServersPage> createState() => _ServersPageState();
}

class _ServersPageState extends State<ServersPage> {
  String? subFilter; // null = all subscriptions
  String protoFilter = '';
  String query = '';

  AppState get s => widget.state;

  @override
  void initState() {
    super.initState();
    if (!s.latencyAutoTested && s.online && !s.testingLatency && s.latency.isEmpty && s.nodeCount > 0) {
      s.latencyAutoTested = true;
      // Not while the page is being built: the test notifies listeners.
      WidgetsBinding.instance.addPostFrameCallback((_) => s.testLatency());
    }
  }

  /// The fastest working node among [rows], if any was tested.
  (Subscription, NodeView)? _fastest(List<(Subscription, NodeView)> rows) {
    (Subscription, NodeView)? best;
    var bestMs = 0;
    for (final (sub, n) in rows) {
      final l = s.latencyOf(sub.id, n.fingerprint);
      if (l != null && l.ok && n.cores.isNotEmpty && (best == null || l.ms < bestMs)) {
        best = (sub, n);
        bestMs = l.ms;
      }
    }
    return best;
  }

  @override
  Widget build(BuildContext context) {
    final subs = s.subscriptions;
    if (subFilter != null && s.subscriptionById(subFilter!) == null) subFilter = null;
    final rows = <(Subscription, NodeView)>[
      for (final sub in subs)
        if (subFilter == null || sub.id == subFilter)
          for (final n in sub.nodes)
            if ((protoFilter.isEmpty || n.protocol == protoFilter) && _matches(n, sub)) (sub, n),
    ];
    final protocols = {
      for (final sub in subs)
        for (final n in sub.nodes) n.protocol,
    }.toList()..sort();
    final fastest = _fastest(rows);
    final compact = isCompact(context);

    final search = TextField(
      onChanged: (v) => setState(() => query = v.trim().toLowerCase()),
      style: const TextStyle(fontSize: 13),
      decoration: InputDecoration(
        hintText: compact ? 'Поиск' : 'Поиск по названию, стране, протоколу',
        prefixIcon: Icon(Icons.search, size: 17, color: context.pal.dim),
        prefixIconConstraints: const BoxConstraints(minWidth: 34),
      ),
    );
    // The protocols in a menu rather than a row of chips that wraps.
    final protocol = PopupMenuButton<String>(
      tooltip: 'Показать только один протокол',
      onSelected: (v) => setState(() => protoFilter = v),
      itemBuilder: (_) => [
        CheckedPopupMenuItem(value: '', checked: protoFilter.isEmpty, child: const Text('Все протоколы')),
        for (final pr in protocols) CheckedPopupMenuItem(value: pr, checked: protoFilter == pr, child: Text(protocolLabel(pr))),
      ],
      child: IgnorePointer(
        child: Btn(label: protoFilter.isEmpty ? 'Все протоколы' : protocolLabel(protoFilter), icon: Icons.filter_list, small: true, onPressed: () {}),
      ),
    );
    final chips = [
      protocol,
      if (subFilter != null) _Chip(label: '× ${s.subscriptionById(subFilter!)?.displayName ?? ''}', on: true, onTap: () => setState(() => subFilter = null)),
    ];
    final pingMode = s.setting('cores.latency_test', 'ping');
    // How to ping, next to the button that pings.
    final pingHow = s.hasSetting('cores.latency_test')
        ? PopupMenuButton<String>(
            tooltip: 'Как проверять пинг',
            onSelected: (v) => s.updateSettings((x) => x['cores']['latency_test'] = v),
            itemBuilder: (_) => [
              for (final (v, title, text) in const [
                ('ping', 'Пинг до сервера', 'ICMP-пинг, где он закрыт — время TCP-подключения'),
                ('proxy', 'Запрос через ядро', 'Реальная задержка с шифрованием, заодно видно, работает ли сервер'),
              ])
                CheckedPopupMenuItem(
                  value: v,
                  checked: pingMode == v,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(title),
                      Text(text, style: TextStyle(fontSize: 11, color: context.pal.muted)),
                    ],
                  ),
                ),
            ],
            child: IgnorePointer(
              child: Btn(icon: Icons.tune, small: true, onPressed: () {}),
            ),
          )
        : null;
    final ping = Btn(
      label: 'Проверить пинг',
      icon: Icons.speed,
      small: true,
      loading: s.testingLatency,
      tooltip: subFilter == null ? 'Проверить все серверы' : 'Проверить серверы этой подписки',
      onPressed: () => s.testLatency(subFilter),
    );
    final best = Btn(
      label: 'Самый быстрый',
      icon: Icons.bolt,
      small: true,
      tooltip: fastest == null ? 'Сначала запустите тест задержки' : '${fastest.$2.name}: ${s.latencyOf(fastest.$1.id, fastest.$2.fingerprint)!.ms} мс',
      onPressed: fastest == null ? null : () => s.selectNode(fastest.$1.id, fastest.$2.fingerprint, fastest.$2.name),
    );

    return PageFrame(
      children: [
        PageHeader(
          'Серверы',
          subtitle: 'Выберите сервер и нажмите «Подключить». Нажмите на подписку, чтобы показать только её серверы.',
          actions: [
            if (subs.any((x) => !x.isLocal))
              Btn(
                label: compact ? null : 'Обновить все',
                icon: Icons.refresh,
                tooltip: compact ? 'Обновить подписки' : null,
                loading: s.refreshing.isNotEmpty,
                onPressed: s.refreshAll,
              ),
            Btn(label: compact ? 'Добавить' : 'Добавить подписку', icon: Icons.add, kind: BtnKind.primary, onPressed: () => showAddSubscription(context, s)),
          ],
        ),
        if (subs.isEmpty)
          _EmptySubs(onAdd: () => showAddSubscription(context, s))
        else ...[
          LayoutBuilder(
            builder: (context, c) {
              final cols = (c.maxWidth / 272).floor().clamp(1, 4);
              final w = (c.maxWidth - 12 * (cols - 1)) / cols;
              return Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  for (final sub in subs)
                    SizedBox(
                      width: w,
                      child: _SubCard(
                        state: s,
                        sub: sub,
                        selected: subFilter == sub.id,
                        onTap: () => setState(() => subFilter = subFilter == sub.id ? null : sub.id),
                      ),
                    ),
                ],
              );
            },
          ),
          SizedBox(height: compact ? 14 : 18),
          if (compact) ...[
            // A phone: the search with the buttons as icons, then one
            // sideways row of filters.
            Row(
              children: [
                Expanded(child: search),
                const SizedBox(width: 8),
                Btn(icon: ping.icon, tooltip: ping.tooltip, loading: ping.loading, onPressed: ping.onPressed),
                const SizedBox(width: 6),
                Btn(icon: best.icon, tooltip: best.tooltip, onPressed: best.onPressed),
              ],
            ),
            const SizedBox(height: 10),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  for (final (i, c) in chips.indexed) ...[if (i > 0) const SizedBox(width: 6), c],
                  if (pingHow != null) ...[const SizedBox(width: 6), pingHow],
                ],
              ),
            ),
          ] else
            Wrap(
              spacing: 12,
              runSpacing: 10,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                SizedBox(width: 320, child: search),
                ...chips,
                Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    ping,
                    if (pingHow != null) ...[const SizedBox(width: 4), pingHow],
                  ],
                ),
                best,
                Text('Пинг: зелёный — до 200 мс, жёлтый — до 500 мс', style: TextStyle(fontSize: 11, color: context.pal.dim)),
              ],
            ),
          const SizedBox(height: 12),
          Panel(
            padding: const EdgeInsets.all(6),
            child: _NodeTable(state: s, rows: rows, showSub: subs.length > 1 && subFilter == null),
          ),
        ],
      ],
    );
  }

  bool _matches(NodeView n, Subscription sub) {
    if (query.isEmpty) return true;
    return n.name.toLowerCase().contains(query) ||
        n.server.toLowerCase().contains(query) ||
        protocolLabel(n.protocol).toLowerCase().contains(query) ||
        n.transport.contains(query) ||
        sub.displayName.toLowerCase().contains(query);
  }
}

class _Chip extends StatelessWidget {
  final String label;
  final bool on;
  final VoidCallback onTap;
  const _Chip({required this.label, required this.on, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return MouseRegion(
      cursor: SystemMouseCursors.click,
      child: GestureDetector(
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 5),
          decoration: BoxDecoration(
            color: on ? p.surface3 : Colors.transparent,
            borderRadius: BorderRadius.circular(99),
            border: Border.all(color: on ? p.border2 : p.border),
          ),
          child: Text(
            label,
            style: TextStyle(fontSize: 12, fontWeight: FontWeight.w500, color: on ? p.text : p.muted),
          ),
        ),
      ),
    );
  }
}

class _EmptySubs extends StatelessWidget {
  final VoidCallback onAdd;
  const _EmptySubs({required this.onAdd});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Panel(
      padding: const EdgeInsets.symmetric(vertical: 48, horizontal: 24),
      child: Column(
        children: [
          Icon(Icons.cloud_download_outlined, size: 40, color: p.dim),
          const SizedBox(height: 14),
          const Text('Подписок пока нет', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
          const SizedBox(height: 6),
          Text(
            'Добавьте ссылку на подписку от вашего провайдера или вставьте ссылки на серверы (vless://, trojan://, hy2://…)',
            textAlign: TextAlign.center,
            style: TextStyle(color: p.muted),
          ),
          const SizedBox(height: 18),
          Btn(label: 'Добавить подписку', icon: Icons.add, kind: BtnKind.primary, onPressed: onAdd),
        ],
      ),
    );
  }
}

class _SubCard extends StatelessWidget {
  final AppState state;
  final Subscription sub;
  final bool selected;
  final VoidCallback onTap;
  const _SubCard({required this.state, required this.sub, required this.selected, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final i = sub.info;
    final refreshing = state.refreshing.contains(sub.id);
    final daysLeft = i.expire?.difference(DateTime.now()).inDays;
    final expired = i.expire != null && i.expire!.isBefore(DateTime.now());
    final expiresSoon = daysLeft != null && daysLeft < 7;
    final compact = isCompact(context);
    return Panel(
      padding: compact ? const EdgeInsets.fromLTRB(14, 4, 4, 12) : const EdgeInsets.fromLTRB(16, 12, 8, 14),
      borderColor: selected ? accent : null,
      onTap: onTap,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  sub.displayName,
                  style: const TextStyle(fontWeight: FontWeight.w600),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              // The panel's support-url header: its support chat.
              if (i.supportUrl.isNotEmpty) ...[
                const SizedBox(width: 8),
                SupportButton(state: state, url: i.supportUrl, urgent: expired || sub.lastError.isNotEmpty),
                const SizedBox(width: 4),
              ],
              if (refreshing) const SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2)),
              if (sub.lastError.isNotEmpty && !refreshing)
                Tooltip(
                  message: 'Последнее обновление не удалось:\n${humanError(sub.lastError)}',
                  child: const Icon(Icons.error_outline, size: 16, color: warnColor),
                ),
              _SubMenu(state: state, sub: sub),
            ],
          ),
          if (!compact)
            Padding(
              padding: const EdgeInsets.only(right: 8),
              child: Text(
                sub.isLocal ? 'вставленный список' : sub.host,
                style: TextStyle(fontSize: 12, color: p.dim, fontFamily: monoFont, fontFamilyFallback: monoFallback),
                overflow: TextOverflow.ellipsis,
              ),
            ),
          Padding(
            padding: EdgeInsets.only(right: compact ? 10 : 8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SizedBox(height: compact ? 4 : 12),
                // What the panel says about the subscription: the traffic
                // on the left, how long it lasts on the right.
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      flex: 3,
                      child: _Fact(
                        label: i.total > 0 ? 'Трафик' : (i.used > 0 ? 'Израсходовано' : 'Серверы'),
                        value: i.total > 0 || i.used > 0 ? formatBytes(i.used) : '${sub.nodes.length}',
                        note: i.total > 0 ? 'из ${formatBytes(i.total)}' : (i.used > 0 ? 'без лимита' : ''),
                        color: i.total > 0 && i.used / i.total > .9 ? errColor : null,
                        progress: i.total > 0 ? (i.used / i.total).clamp(0, 1).toDouble() : null,
                      ),
                    ),
                    const SizedBox(width: 14),
                    Expanded(
                      flex: 2,
                      child: i.expire != null
                          ? _Fact(
                              label: expired ? 'Истекла' : 'Осталось',
                              value: expired ? formatDate(i.expire!) : (daysLeft! < 1 ? 'меньше дня' : '$daysLeft дн.'),
                              note: expired ? 'продлите подписку' : 'до ${formatDate(i.expire!)}',
                              color: expired ? errColor : (expiresSoon ? warnColor : null),
                            )
                          : _Fact(label: 'Срок', value: 'бессрочно', note: sub.isLocal ? '' : 'обновлено ${formatAgo(sub.updatedAt)}'),
                    ),
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

/// One fact about a subscription: a caption, the value, a note, and for
/// the traffic how much of it is used.
class _Fact extends StatelessWidget {
  final String label;
  final String value;
  final String note;
  final Color? color;
  final double? progress;
  const _Fact({required this.label, required this.value, this.note = '', this.color, this.progress});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label, style: TextStyle(fontSize: 11, color: p.dim)),
        const SizedBox(height: 2),
        Text.rich(
          TextSpan(
            children: [
              TextSpan(
                text: value,
                style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: color ?? p.text),
              ),
              if (note.isNotEmpty && progress != null)
                TextSpan(
                  text: ' $note',
                  style: TextStyle(fontSize: 12, color: p.muted),
                ),
            ],
          ),
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
        ),
        if (progress != null) ...[
          const SizedBox(height: 6),
          ClipRRect(
            borderRadius: BorderRadius.circular(9),
            child: LinearProgressIndicator(value: progress, minHeight: 5, backgroundColor: p.surface3, color: color ?? accent),
          ),
        ],
        if (note.isNotEmpty && progress == null)
          Text(
            note,
            style: TextStyle(fontSize: 11.5, color: p.muted),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
      ],
    );
  }
}

class _SubMenu extends StatelessWidget {
  final AppState state;
  final Subscription sub;
  const _SubMenu({required this.state, required this.sub});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return PopupMenuButton<String>(
      tooltip: 'Действия',
      icon: Icon(Icons.more_vert, size: 18, color: p.muted),
      padding: EdgeInsets.zero,
      constraints: const BoxConstraints(minWidth: 180),
      onSelected: (v) async {
        switch (v) {
          case 'refresh':
            state.refreshSubscription(sub.id);
          case 'rename':
            final name = await _askText(context, 'Переименовать', sub.name.isEmpty ? sub.displayName : sub.name);
            if (name != null) state.renameSubscription(sub.id, name);
          case 'page':
            state.openLink(sub.info.webPageUrl);
          case 'site':
            await Clipboard.setData(ClipboardData(text: sub.info.webPageUrl));
            state.toast('Адрес страницы скопирован');
          case 'delete':
            if (await _confirm(context, 'Удалить «${sub.displayName}»?', 'Серверы этой подписки пропадут из списка. Текущее подключение не прервётся.')) {
              state.removeSubscription(sub.id);
            }
        }
      },
      itemBuilder: (context) => [
        if (!sub.isLocal) _item('refresh', Icons.refresh, 'Обновить'),
        _item('rename', Icons.edit_outlined, 'Переименовать'),
        if (sub.info.webPageUrl.isNotEmpty) ...[
          _item('page', Icons.open_in_new, 'Открыть страницу подписки'),
          _item('site', Icons.link, 'Копировать адрес страницы'),
        ],
        _item('delete', Icons.delete_outline, 'Удалить', color: errColor),
      ],
    );
  }

  PopupMenuItem<String> _item(String v, IconData icon, String label, {Color? color}) => PopupMenuItem(
    value: v,
    height: 38,
    child: Row(
      children: [
        Icon(icon, size: 16, color: color),
        const SizedBox(width: 10),
        Text(label, style: TextStyle(color: color)),
      ],
    ),
  );
}

class _NodeTable extends StatelessWidget {
  final AppState state;
  final List<(Subscription, NodeView)> rows;
  final bool showSub;
  const _NodeTable({required this.state, required this.rows, required this.showSub});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    if (rows.isEmpty) {
      return Padding(
        padding: const EdgeInsets.all(30),
        child: Center(
          child: Text('Ничего не найдено', style: TextStyle(color: p.dim)),
        ),
      );
    }
    final headStyle = TextStyle(fontSize: 11, color: p.dim, letterSpacing: .6, fontWeight: FontWeight.w600);
    // The columns need about 640 points; narrower, the rows become a list.
    return LayoutBuilder(
      builder: (context, c) {
        final narrow = isCompact(context) || c.maxWidth < 640;
        return Column(
          children: [
            _row(
              narrow: narrow,
              header: true,
              cells: [
                const SizedBox(),
                Text('СЕРВЕР', style: headStyle),
                Text('ПРОТОКОЛ', style: headStyle),
                Text('ТРАНСПОРТ', style: headStyle),
                Text('ПИНГ', style: headStyle),
                const SizedBox(),
              ],
            ),
            for (final (sub, n) in rows) _NodeRow(state: state, sub: sub, node: n, showSub: showSub, narrow: narrow),
          ],
        );
      },
    );
  }

  static Widget _row({required bool narrow, required List<Widget> cells, bool header = false}) {
    if (narrow) {
      // A phone: the name over the protocol and transport, then the ping
      // and the action; the column headings and the cores are left out.
      if (header) return const SizedBox.shrink();
      return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 10),
        child: Row(
          children: [
            SizedBox(width: 28, child: cells[0]),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  cells[1],
                  const SizedBox(height: 4),
                  Row(
                    children: [
                      cells[2],
                      const SizedBox(width: 8),
                      Flexible(child: cells[3]),
                    ],
                  ),
                ],
              ),
            ),
            SizedBox(width: 64, child: cells[4]),
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 104),
              child: FittedBox(fit: BoxFit.scaleDown, child: cells[5]),
            ),
          ],
        ),
      );
    }
    return Padding(
      padding: EdgeInsets.symmetric(horizontal: 12, vertical: header ? 8 : 10),
      child: Row(
        children: [
          SizedBox(width: 28, child: cells[0]),
          Expanded(flex: 14, child: cells[1]),
          SizedBox(width: 118, child: cells[2]),
          Expanded(flex: 10, child: cells[3]),
          SizedBox(width: 84, child: cells[4]),
          SizedBox(
            width: 118,
            child: Align(
              alignment: Alignment.centerRight,
              child: FittedBox(fit: BoxFit.scaleDown, child: cells[5]),
            ),
          ),
        ],
      ),
    );
  }
}

class _NodeRow extends StatefulWidget {
  final AppState state;
  final Subscription sub;
  final NodeView node;
  final bool showSub;
  final bool narrow;
  const _NodeRow({required this.state, required this.sub, required this.node, required this.showSub, required this.narrow});

  @override
  State<_NodeRow> createState() => _NodeRowState();
}

class _NodeRowState extends State<_NodeRow> {
  bool hover = false;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final s = widget.state;
    final n = widget.node;
    final sel = s.isSelected(widget.sub, n);
    final active = sel && s.status.active;
    final connected = sel && s.status.state == ConnState.connected;
    final unusable = n.cores.isEmpty;

    final row = _NodeTable._row(
      narrow: widget.narrow,
      cells: [
        _Radio(on: sel),
        Row(
          children: [
            Flexible(
              child: Tooltip(
                message:
                    '${n.server}:${n.port}\n'
                    '${n.cores.isEmpty ? 'Ни одно ядро не поддерживает' : 'Ядра: ${n.cores.map((k) => coreStyle(k).name).join(', ')}'}',
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      n.name,
                      style: const TextStyle(fontWeight: FontWeight.w500),
                      overflow: TextOverflow.ellipsis,
                    ),
                    if (widget.showSub)
                      Text(
                        widget.sub.displayName,
                        style: TextStyle(fontSize: 11, color: p.dim),
                        overflow: TextOverflow.ellipsis,
                      ),
                  ],
                ),
              ),
            ),
          ],
        ),
        Align(alignment: Alignment.centerLeft, child: ProtoBadge(n.protocol)),
        Text(
          '${n.transport} · ${n.security}',
          style: TextStyle(fontSize: 12, color: p.muted),
          overflow: TextOverflow.ellipsis,
        ),
        _LatencyCell(latency: s.latencyOf(widget.sub.id, n.fingerprint), testing: s.testingLatency),
        if (connected)
          const Pill('ПОДКЛЮЧЁН', color: okColor)
        else if (active)
          const Pill('ПОДКЛЮЧЕНИЕ…', color: accent)
        else if (!unusable && (hover || sel))
          Btn(
            label: 'Подключить',
            icon: Icons.power_settings_new,
            small: true,
            kind: sel ? BtnKind.primary : BtnKind.normal,
            onPressed: s.busy || !s.online ? null : () => s.connect(subscription: widget.sub.id, fingerprint: n.fingerprint, name: n.name),
          )
        else
          const SizedBox(),
      ],
    );

    return Tooltip(
      message: unusable ? 'Ни одно установленное ядро не поддерживает этот сервер' : '',
      child: MouseRegion(
        cursor: unusable ? SystemMouseCursors.basic : SystemMouseCursors.click,
        onEnter: (_) => setState(() => hover = true),
        onExit: (_) => setState(() => hover = false),
        child: GestureDetector(
          onTap: unusable || sel
              ? null
              : isCompact(context) && s.status.active && s.online && !s.busy
              ? () => s.connect(subscription: widget.sub.id, fingerprint: n.fingerprint, name: n.name)
              : () => s.selectNode(widget.sub.id, n.fingerprint, n.name),
          child: Opacity(
            opacity: unusable ? .45 : 1,
            child: Container(
              decoration: BoxDecoration(
                color: sel ? accent.withValues(alpha: .10) : (hover && !unusable ? p.surface2 : Colors.transparent),
                borderRadius: BorderRadius.circular(10),
              ),
              child: row,
            ),
          ),
        ),
      ),
    );
  }
}

class _LatencyCell extends StatelessWidget {
  final Latency? latency;
  final bool testing;
  const _LatencyCell({required this.latency, required this.testing});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final l = latency;
    const mono = TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 12, fontWeight: FontWeight.w500);
    if (l == null) {
      return testing
          ? Align(
              alignment: Alignment.centerLeft,
              child: SizedBox(width: 12, height: 12, child: CircularProgressIndicator(strokeWidth: 1.5, color: p.dim)),
            )
          : Text('—', style: mono.copyWith(color: p.dim));
    }
    if (!l.ok) {
      return Tooltip(
        message: l.error,
        child: Text('нет ответа', style: mono.copyWith(color: errColor, fontSize: 11)),
      );
    }
    final color = l.ms < 200 ? okColor : (l.ms < 500 ? warnColor : errColor);
    final text = Text('${l.ms} мс', style: mono.copyWith(color: color));
    final how = switch (l.method) {
      'icmp' => 'ICMP-пинг до сервера',
      'tcp' => 'Время TCP-подключения к серверу: ICMP-пинг он не пропускает',
      _ when l.core.isNotEmpty => 'Запрос через ${coreStyle(l.core).name}',
      _ => '',
    };
    return how.isEmpty ? text : Tooltip(message: how, child: text);
  }
}

class _Radio extends StatelessWidget {
  final bool on;
  const _Radio({required this.on});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      width: 16,
      height: 16,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        border: Border.all(color: on ? accent : p.border2, width: 2),
      ),
      padding: const EdgeInsets.all(2),
      child: on
          ? Container(
              decoration: const BoxDecoration(color: accent, shape: BoxShape.circle),
            )
          : null,
    );
  }
}

// ------------------------------------------------------------------ dialogs

Future<void> showAddSubscription(BuildContext context, AppState state) => showDialog(
  context: context,
  builder: (_) => _AddDialog(state: state),
);

class _AddDialog extends StatefulWidget {
  final AppState state;
  const _AddDialog({required this.state});

  @override
  State<_AddDialog> createState() => _AddDialogState();
}

class _AddDialogState extends State<_AddDialog> {
  final source = TextEditingController();
  final name = TextEditingController();
  bool busy = false;
  String? error;

  @override
  void initState() {
    super.initState();
    source.addListener(() => setState(() => error = null));
    _pasteIfLink();
  }

  /// Most users come with the link just copied: take it from the clipboard
  /// when it looks like one.
  Future<void> _pasteIfLink() async {
    final d = await Clipboard.getData(Clipboard.kTextPlain);
    final t = d?.text?.trim() ?? '';
    if (!mounted || source.text.isNotEmpty) return;
    if (RegExp(r'^(https?|vless|vmess|trojan|ss|hy2|hysteria2|tuic|anytls|wireguard)://\S+', caseSensitive: false).hasMatch(t)) {
      source.text = t;
      source.selection = TextSelection(baseOffset: 0, extentOffset: t.length);
    }
  }

  @override
  void dispose() {
    source.dispose();
    name.dispose();
    super.dispose();
  }

  String get _detected {
    final t = source.text.trim();
    if (t.isEmpty) return '';
    if (RegExp(r'^https?://\S+$').hasMatch(t)) return 'Ссылка на подписку — серверы скачаются и будут обновляться сами';
    final links = RegExp(r'^[a-z0-9]+://', multiLine: true).allMatches(t).length;
    if (links > 0) return 'Серверов в тексте: $links — сохранятся как есть, без обновления';
    return 'Похоже на содержимое подписки (base64, Clash или sing-box)';
  }

  Future<void> _submit() async {
    if (source.text.trim().isEmpty) return;
    setState(() => busy = true);
    final err = await widget.state.addSubscription(source: source.text, name: name.text);
    if (!mounted) return;
    if (err == null) {
      Navigator.pop(context);
    } else {
      setState(() {
        busy = false;
        error = err;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final label = TextStyle(fontSize: 12, color: p.muted, fontWeight: FontWeight.w500);
    final compact = isCompact(context);
    return Dialog(
      // A phone: the dialog takes the width, the paste button its icon.
      insetPadding: compact ? const EdgeInsets.symmetric(horizontal: 14, vertical: 24) : null,
      child: SizedBox(
        width: 520,
        child: Padding(
          padding: EdgeInsets.all(compact ? 18 : 22),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('Добавить подписку', style: TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
              const SizedBox(height: 4),
              Text(
                'Вставьте ссылку на подписку от провайдера (обычно https://…) или ссылки на серверы: vless://, trojan://, ss://, hy2://…',
                style: TextStyle(fontSize: 12, color: p.muted),
              ),
              const SizedBox(height: 14),
              Text('Ссылка', style: label),
              const SizedBox(height: 6),
              TextField(
                controller: source,
                autofocus: true,
                minLines: 3,
                maxLines: 6,
                style: const TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 12.5),
                decoration: const InputDecoration(hintText: 'https://… или vless://…'),
              ),
              const SizedBox(height: 8),
              ConstrainedBox(
                constraints: const BoxConstraints(minHeight: 18),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (error != null) ...[const Icon(Icons.error_outline, size: 15, color: errColor), const SizedBox(width: 6)],
                    Expanded(
                      child: Text(error ?? _detected, style: TextStyle(fontSize: 12, color: error != null ? errColor : p.muted)),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 8),
              Text('Название', style: label),
              const SizedBox(height: 6),
              TextField(
                controller: name,
                style: const TextStyle(fontSize: 13),
                decoration: const InputDecoration(hintText: 'Необязательно — возьмём у провайдера'),
                onSubmitted: (_) => _submit(),
              ),
              const SizedBox(height: 20),
              Row(
                children: [
                  Btn(
                    label: compact ? null : 'Из буфера',
                    tooltip: compact ? 'Вставить из буфера' : null,
                    icon: Icons.content_paste,
                    kind: BtnKind.ghost,
                    onPressed: () async {
                      final d = await Clipboard.getData(Clipboard.kTextPlain);
                      if (d?.text != null) source.text = d!.text!.trim();
                    },
                  ),
                  const Spacer(),
                  Btn(label: 'Отмена', onPressed: busy ? null : () => Navigator.pop(context)),
                  const SizedBox(width: 8),
                  Btn(label: 'Добавить', kind: BtnKind.primary, loading: busy, onPressed: source.text.trim().isEmpty ? null : _submit),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

Future<String?> _askText(BuildContext context, String title, String initial) {
  final c = TextEditingController(text: initial);
  return showDialog<String>(
    context: context,
    builder: (context) => Dialog(
      child: SizedBox(
        width: 420,
        child: Padding(
          padding: const EdgeInsets.all(22),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
              const SizedBox(height: 14),
              TextField(controller: c, autofocus: true, onSubmitted: (v) => Navigator.pop(context, v.trim())),
              const SizedBox(height: 8),
              Text('Пустое название — взять у провайдера', style: TextStyle(fontSize: 12, color: context.pal.dim)),
              const SizedBox(height: 18),
              Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  Btn(label: 'Отмена', onPressed: () => Navigator.pop(context)),
                  const SizedBox(width: 8),
                  Btn(label: 'Сохранить', kind: BtnKind.primary, onPressed: () => Navigator.pop(context, c.text.trim())),
                ],
              ),
            ],
          ),
        ),
      ),
    ),
  );
}

Future<bool> _confirm(BuildContext context, String title, String text) async {
  final r = await showDialog<bool>(
    context: context,
    builder: (context) => Dialog(
      child: SizedBox(
        width: 420,
        child: Padding(
          padding: const EdgeInsets.all(22),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
              const SizedBox(height: 10),
              Text(text, style: TextStyle(color: context.pal.muted)),
              const SizedBox(height: 20),
              Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  Btn(label: 'Отмена', onPressed: () => Navigator.pop(context, false)),
                  const SizedBox(width: 8),
                  Btn(label: 'Удалить', kind: BtnKind.danger, onPressed: () => Navigator.pop(context, true)),
                ],
              ),
            ],
          ),
        ),
      ),
    ),
  );
  return r == true;
}

/// "5 серверов", with the Russian plural.
String serversCount(int n) {
  final m10 = n % 10, m100 = n % 100;
  final word = m10 == 1 && m100 != 11 ? 'сервер' : (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14) ? 'сервера' : 'серверов');
  return '$n $word';
}
