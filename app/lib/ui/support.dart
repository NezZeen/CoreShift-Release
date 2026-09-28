import 'package:flutter/material.dart';

import '../state/app_state.dart';
import 'theme.dart';

/// Where a panel's support link leads: the messenger or social network,
/// told by the link's host, for its logo and colour.
enum SupportKind {
  telegram('Telegram', Color(0xFF229ED9)),
  vk('VK', Color(0xFF0077FF)),
  whatsapp('WhatsApp', Color(0xFF25D366)),
  discord('Discord', Color(0xFF5865F2)),
  email('почта', accent),
  other('', accent);

  final String name;
  final Color color;
  const SupportKind(this.name, this.color);

  static SupportKind of(String url) {
    final u = Uri.tryParse(url.trim());
    if (u == null) return other;
    if (u.scheme == 'tg') return telegram;
    if (u.scheme == 'mailto') return email;
    var host = u.host.toLowerCase();
    if (host.startsWith('www.')) host = host.substring(4);
    bool on(List<String> domains) => domains.any((d) => host == d || host.endsWith('.$d'));
    if (on(['t.me', 'telegram.me', 'telegram.dog', 'telegram.org'])) return telegram;
    if (on(['vk.com', 'vk.me', 'vk.ru'])) return vk;
    if (on(['wa.me', 'whatsapp.com'])) return whatsapp;
    if (on(['discord.gg', 'discord.com'])) return discord;
    return other;
  }
}

/// The logo of [kind] in its colour; those Material lacks drawn here: VK as
/// its letters on a blue square, WhatsApp as its bubble.
class SupportMark extends StatelessWidget {
  final SupportKind kind;
  final double size;
  const SupportMark(this.kind, {super.key, this.size = 20});

  @override
  Widget build(BuildContext context) => switch (kind) {
    SupportKind.telegram => Icon(Icons.telegram, size: size, color: kind.color),
    // Material has no WhatsApp logo: its green bubble with the handset.
    SupportKind.whatsapp => SizedBox(
      width: size,
      height: size,
      child: CustomPaint(
        painter: _BubblePainter(kind.color),
        child: Center(
          child: Icon(Icons.call, size: size * .5, color: Colors.white),
        ),
      ),
    ),
    SupportKind.discord => Icon(Icons.discord, size: size, color: kind.color),
    SupportKind.email => Icon(Icons.mail_outline, size: size, color: kind.color),
    SupportKind.other => Icon(Icons.support_agent, size: size, color: kind.color),
    SupportKind.vk => Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(color: kind.color, borderRadius: BorderRadius.circular(size * .28)),
      child: Text(
        'VK',
        style: TextStyle(color: Colors.white, fontSize: size * .46, fontWeight: FontWeight.w800, height: 1, letterSpacing: -.3),
      ),
    ),
  };
}

/// A round speech bubble with its tail at the bottom left.
class _BubblePainter extends CustomPainter {
  final Color color;
  const _BubblePainter(this.color);

  @override
  void paint(Canvas canvas, Size size) {
    final s = size.shortestSide;
    final paint = Paint()
      ..color = color
      ..isAntiAlias = true;
    canvas.drawCircle(Offset(s * .52, s * .48), s * .44, paint);
    canvas.drawPath(
      Path()
        ..moveTo(s * .2, s * .66)
        ..lineTo(s * .06, s * .96)
        ..lineTo(s * .4, s * .86)
        ..close(),
      paint,
    );
  }

  @override
  bool shouldRepaint(_BubblePainter old) => old.color != color;
}

/// A small chip to the panel's support chat: the messenger's logo and
/// "Поддержка", tinted in the messenger's colour.
class SupportButton extends StatelessWidget {
  final AppState state;
  final String url;

  /// Draws attention: the subscription expired or stopped updating.
  final bool urgent;
  const SupportButton({super.key, required this.state, required this.url, this.urgent = false});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final kind = SupportKind.of(url);
    final c = kind.color;
    final where = switch (kind) {
      SupportKind.email => 'Написать в поддержку на почту',
      SupportKind.other => 'Написать в поддержку',
      _ => 'Написать в поддержку в ${kind.name}',
    };
    return Tooltip(
      message: where,
      child: Material(
        color: c.withValues(alpha: urgent ? .18 : .1),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(99),
          side: BorderSide(color: c.withValues(alpha: urgent ? .7 : .35)),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: () => state.openLink(url),
          hoverColor: c.withValues(alpha: .1),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(5, 4, 10, 4),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                SupportMark(kind, size: 18),
                const SizedBox(width: 6),
                Text(
                  'Поддержка',
                  style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: p.text),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
