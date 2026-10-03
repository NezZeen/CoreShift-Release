import 'package:flutter/material.dart';

import '../state/app_state.dart';
import 'theme.dart';
import 'widgets.dart';

/// Asks before adding a subscription that came from outside the add dialog:
/// a panel's button, the clipboard or a QR code. It shows the panel's host,
/// never the link, whose path carries the access token.
Future<void> showImportOffer(BuildContext context, AppState state) => showDialog<void>(
  context: context,
  barrierDismissible: false,
  builder: (_) => _ImportOffer(state: state),
);

class _ImportOffer extends StatefulWidget {
  final AppState state;
  const _ImportOffer({required this.state});

  @override
  State<_ImportOffer> createState() => _ImportOfferState();
}

class _ImportOfferState extends State<_ImportOffer> {
  bool busy = false;
  String? error;

  Future<void> _add() async {
    setState(() {
      busy = true;
      error = null;
    });
    final err = await widget.state.acceptImport();
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
    final s = widget.state;
    final link = s.pendingImport;
    if (link == null) return const SizedBox();
    final servers = link.content.isEmpty ? 0 : link.content.split('\n').length;
    final what = link.url.isNotEmpty ? Uri.tryParse(link.url)?.host ?? '' : (servers == 1 ? 'Один сервер' : 'Серверов: $servers');
    final from = switch (s.importFrom) {
      ImportFrom.clipboard => 'В буфере обмена ссылка на подписку.',
      ImportFrom.qr => 'В QR-коде ссылка на подписку.',
      ImportFrom.link => 'Панель провайдера передала подписку в CoreShift.',
    };
    final compact = isCompact(context);
    return Dialog(
      insetPadding: compact ? const EdgeInsets.symmetric(horizontal: 14, vertical: 24) : null,
      child: SizedBox(
        width: 440,
        child: Padding(
          padding: EdgeInsets.all(compact ? 18 : 22),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(s.importFrom == ImportFrom.clipboard ? Icons.content_paste : Icons.add_link, color: context.pal.accentInk),
                  const SizedBox(width: 10),
                  Expanded(child: Text(link.url.isNotEmpty ? 'Добавить подписку?' : 'Добавить серверы?', style: dialogTitle)),
                ],
              ),
              const SizedBox(height: 10),
              Text(from, style: TextStyle(color: p.muted, fontSize: 13)),
              const SizedBox(height: 12),
              Container(
                width: double.infinity,
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                decoration: BoxDecoration(
                  color: p.surface2,
                  borderRadius: BorderRadius.circular(10),
                  border: Border.all(color: p.border),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if (link.name.isNotEmpty) Text(link.name, style: const TextStyle(fontWeight: FontWeight.w600)),
                    Text(
                      what,
                      style: TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 12.5, color: link.name.isEmpty ? p.text : p.muted),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ],
                ),
              ),
              if (error != null) ...[
                const SizedBox(height: 10),
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Icon(Icons.error_outline, size: 15, color: errColor),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text(error!, style: const TextStyle(fontSize: 12, color: errColor)),
                    ),
                  ],
                ),
              ],
              const SizedBox(height: 20),
              // A narrow phone puts the buttons on two lines rather than
              // cutting them off.
              Wrap(
                alignment: WrapAlignment.end,
                spacing: 8,
                runSpacing: 8,
                children: [
                  Btn(
                    label: 'Не добавлять',
                    onPressed: busy
                        ? null
                        : () {
                            s.dismissImport();
                            Navigator.pop(context);
                          },
                  ),
                  Btn(label: 'Добавить', icon: compact ? null : Icons.add, kind: BtnKind.primary, loading: busy, onPressed: _add),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}
