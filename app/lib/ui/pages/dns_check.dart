import 'package:flutter/material.dart';

import '../../state/app_state.dart';
import '../theme.dart';

/// What to know before typing a DNS of one's own: where it is safe from
/// the provider.
const dnsHintText =
    'Свой DNS с блокировкой рекламы (например, AdGuard) лучше указывать в поле «через VPN»: туда провайдер не дотянется. '
    'В поле «напрямую» его может заблокировать провайдер или белый список.';

/// The hint above the DNS fields of the settings.
class DnsHint extends StatelessWidget {
  const DnsHint({super.key});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      key: const ValueKey('dns-hint'),
      margin: const EdgeInsets.only(bottom: 4),
      padding: const EdgeInsets.fromLTRB(10, 9, 10, 9),
      decoration: BoxDecoration(color: p.surface2, borderRadius: BorderRadius.circular(10)),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.only(top: 1),
            child: Icon(Icons.info_outline, size: 16, color: p.muted),
          ),
          const SizedBox(width: 9),
          Expanded(
            child: Text(dnsHintText, style: TextStyle(fontSize: 12, color: p.muted)),
          ),
        ],
      ),
    );
  }
}

/// Under a DNS field: whether the server answers in this network, as the
/// engine found when the field was saved; a link to try again, and, where
/// nothing has been tried yet, to try.
class DnsCheckLine extends StatelessWidget {
  final AppState state;

  /// The field: "direct" or "remote".
  final String which;
  const DnsCheckLine({super.key, required this.state, required this.which});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final typed = state.setting('dns.$which', '').trim();
    if (typed.isEmpty || state.dnsCheckUnsupported) return const SizedBox.shrink();
    final c = state.dnsChecks[which];
    // A result of another value than the field has now is not its own.
    final shown = c != null && c.server == typed ? c : null;
    final (IconData icon, Color color) = switch (shown) {
      null => (Icons.help_outline, p.muted),
      DnsCheck(running: true) => (Icons.sync, p.muted),
      DnsCheck(ok: true) => (Icons.check_circle_outline, p.okInk),
      DnsCheck(failed: true) => (Icons.error_outline, p.warnInk),
      _ => (Icons.schedule, p.muted),
    };
    final text = shown == null
        ? 'ещё не проверен'
        : shown.running
        ? 'проверяю…'
        : shown.text;
    return Padding(
      key: ValueKey('dns-check-$which'),
      padding: const EdgeInsets.only(top: 2),
      child: Wrap(
        crossAxisAlignment: WrapCrossAlignment.center,
        spacing: 8,
        runSpacing: 2,
        children: [
          Row(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Padding(
                padding: const EdgeInsets.only(top: 1),
                child: Icon(icon, size: 14, color: color),
              ),
              const SizedBox(width: 5),
              Flexible(
                child: Text(text, style: TextStyle(fontSize: 12, color: color)),
              ),
            ],
          ),
          if (shown == null || !shown.running)
            InkWell(
              onTap: state.online ? () => state.checkDns(which) : null,
              child: Text(
                shown == null ? 'Проверить' : 'Проверить ещё раз',
                style: TextStyle(fontSize: 12, color: p.accentInk, fontWeight: FontWeight.w600),
              ),
            ),
        ],
      ),
    );
  }
}

/// Brings its child into view once it is built, when [request] is set: the
/// settings page opened by «Настройки DNS» on the home page.
class ScrollIntoViewOnce extends StatefulWidget {
  final bool request;
  final Widget child;
  const ScrollIntoViewOnce({super.key, required this.request, required this.child});

  @override
  State<ScrollIntoViewOnce> createState() => _ScrollIntoViewOnceState();
}

class _ScrollIntoViewOnceState extends State<ScrollIntoViewOnce> {
  @override
  void initState() {
    super.initState();
    if (widget.request) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) Scrollable.ensureVisible(context, duration: const Duration(milliseconds: 250), alignment: .1);
      });
    }
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
