part of '../servers_page.dart';

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

  /// Fills the form from a QR code: a subscription link, a panel's button
  /// link or server links.
  Future<void> _scan() async {
    String? text;
    try {
      text = await platform.scanQr();
    } catch (e) {
      setState(() => error = platform.scanQrError(e));
      return;
    }
    if (!mounted || text == null) return;
    final link = parseImportLink(text);
    if (link == null || link.error.isNotEmpty) {
      setState(() => error = link?.error ?? 'В QR-коде нет ссылки на подписку или сервер');
      return;
    }
    source.text = link.url.isNotEmpty ? link.url : link.content;
    if (link.name.isNotEmpty && name.text.isEmpty) name.text = link.name;
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
                platform.canScanQr
                    ? 'Вставьте ссылку на подписку от провайдера (обычно https://…), ссылки на серверы (vless://, hy2://…) или отсканируйте QR-код'
                    : 'Вставьте ссылку на подписку от провайдера (обычно https://…) или ссылки на серверы: vless://, trojan://, ss://, hy2://…',
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
                  if (platform.canScanQr) Btn(tooltip: 'Сканировать QR-код', icon: Icons.qr_code_scanner, kind: BtnKind.ghost, onPressed: busy ? null : _scan),
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

/// The subscription's link as a QR code, to add it on a phone: CoreShift's
/// scanner or any other app's reads it. The link is the access key, so the
/// window says so.
Future<void> showSubscriptionQr(BuildContext context, Subscription sub, String url) {
  final QrCode code;
  try {
    code = QrCode.text(url);
  } on ArgumentError {
    return Future.value();
  }
  return showDialog<void>(
    context: context,
    builder: (context) {
      final p = context.pal;
      final compact = isCompact(context);
      return Dialog(
        insetPadding: compact ? const EdgeInsets.symmetric(horizontal: 14, vertical: 24) : null,
        child: SizedBox(
          width: 380,
          child: Padding(
            padding: EdgeInsets.all(compact ? 18 : 22),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  sub.displayName,
                  style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 4),
                Text(
                  platform.isAndroid
                      ? 'Отсканируйте в CoreShift на другом телефоне'
                      : 'Отсканируйте в CoreShift на телефоне: «Добавить подписку» → «Сканировать QR»',
                  style: TextStyle(fontSize: 12, color: p.muted),
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 16),
                QrView(code: code, size: compact ? 260 : 280),
                const SizedBox(height: 14),
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Icon(Icons.lock_outline, size: 15, color: warnColor),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        'В коде ссылка с ключом доступа к подписке. Не показывайте его посторонним и не публикуйте скриншот.',
                        style: TextStyle(fontSize: 12, color: p.muted),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 18),
                Align(
                  alignment: Alignment.centerRight,
                  child: Btn(label: 'Готово', kind: BtnKind.primary, onPressed: () => Navigator.pop(context)),
                ),
              ],
            ),
          ),
        ),
      );
    },
  );
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
