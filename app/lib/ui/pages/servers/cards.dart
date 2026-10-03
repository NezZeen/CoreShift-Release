part of '../servers_page.dart';

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
          if (sub.auto.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 4, right: 8),
              child: Row(
                children: [
                  Icon(Icons.swap_horiz, size: 14, color: p.dim),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      'Автовыбор: ${serversCount(sub.auto.length)}, при отказе подключится следующий',
                      style: TextStyle(fontSize: 12, color: p.dim),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
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
            // A longer note, such as "обновлено только что", wraps rather
            // than being cut off.
            maxLines: 2,
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
          case 'qr':
            final url = await state.subscriptionUrl(sub.id);
            if (url != null && context.mounted) await showSubscriptionQr(context, sub, url);
          case 'delete':
            if (await _confirm(context, 'Удалить «${sub.displayName}»?', 'Серверы этой подписки пропадут из списка. Текущее подключение не прервётся.')) {
              state.removeSubscription(sub.id);
            }
        }
      },
      itemBuilder: (context) => [
        if (!sub.isLocal) _item('refresh', Icons.refresh, 'Обновить'),
        _item('rename', Icons.edit_outlined, 'Переименовать'),
        if (!sub.isLocal) _item('qr', Icons.qr_code_2, platform.isAndroid ? 'Показать QR-код' : 'QR-код для телефона'),
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

/// Tells a phone's user about the swipes, until they use one or close it.
class _SwipeHint extends StatelessWidget {
  final VoidCallback onClose;
  const _SwipeHint({required this.onClose});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      margin: const EdgeInsets.only(top: 12),
      padding: const EdgeInsets.fromLTRB(12, 4, 2, 4),
      decoration: BoxDecoration(
        color: accent.withValues(alpha: .08),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: accent.withValues(alpha: .3)),
      ),
      child: Row(
        children: [
          const Icon(Icons.swipe, size: 17, color: accent),
          const SizedBox(width: 10),
          Expanded(
            child: Text('Смахните сервер вправо, чтобы подключиться', style: TextStyle(fontSize: 12, color: p.muted)),
          ),
          IconButton(
            onPressed: onClose,
            visualDensity: VisualDensity.compact,
            icon: Icon(Icons.close, size: 16, color: p.dim),
            tooltip: 'Понятно',
          ),
        ],
      ),
    );
  }
}
