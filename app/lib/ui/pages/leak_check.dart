import 'package:flutter/material.dart';

import '../../api/models.dart';
import '../../state/app_state.dart';
import '../../state/leak.dart';
import '../theme.dart';
import '../widgets.dart';

/// The DNS leak test: a button and what it found.
class LeakCheck extends StatelessWidget {
  final AppState state;
  final bool first;
  const LeakCheck({super.key, required this.state, this.first = false});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status;
    final ready = st.state == ConnState.connected && st.tun;
    final report = state.leakReport;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SettingRow(
          first: first,
          title: 'Проверка утечки DNS',
          description: ready
              ? 'Узнать, какие DNS-серверы видят ваши запросы: через VPN или провайдера. Использует сервис bash.ws'
              : 'Подключитесь в режиме «Все приложения», чтобы проверить',
          trailing: Btn(label: 'Проверить', icon: Icons.policy_outlined, loading: state.leakTesting, onPressed: ready ? state.runLeakTest : null),
        ),
        if (state.leakError.isNotEmpty)
          _Box(
            color: errColor,
            icon: Icons.error_outline,
            title: 'Проверка не удалась',
            children: [Text(state.leakError, style: TextStyle(fontSize: 12, color: p.muted))],
          )
        else if (report != null)
          _Result(report: report),
      ],
    );
  }
}

class _Result extends StatelessWidget {
  final LeakReport report;
  const _Result({required this.report});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final suspicious = report.suspicious;
    final small = TextStyle(fontSize: 12, color: p.muted);
    Widget server(LeakServer s, {bool bad = false}) => Padding(
      padding: const EdgeInsets.only(top: 3),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(bad ? Icons.warning_amber_rounded : Icons.dns_outlined, size: 14, color: bad ? warnColor : p.dim),
          const SizedBox(width: 6),
          Expanded(
            child: Text.rich(
              TextSpan(
                children: [
                  TextSpan(
                    text: s.ip,
                    style: TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, color: p.text),
                  ),
                  if (s.place.isNotEmpty) TextSpan(text: '  ${s.place}'),
                ],
              ),
              style: small,
            ),
          ),
        ],
      ),
    );

    return _Box(
      color: report.leaks ? warnColor : okColor,
      icon: report.leaks ? Icons.warning_amber_rounded : Icons.verified_user_outlined,
      title: report.leaks ? 'Возможна утечка DNS' : 'Утечки нет',
      children: [
        Text(
          report.leaks
              ? 'Часть запросов обрабатывают DNS-серверы не из страны VPN-сервера — возможно, это DNS вашего провайдера. '
                    'Включите «Строгий DNS в Windows» и блокировку DNS-over-HTTPS браузеров в разделе DNS, переподключитесь и проверьте снова.'
              : report.dns.isEmpty
              ? 'Ни один DNS-сервер не увидел проверочных запросов в обход VPN.'
              : 'Запросы обрабатывают DNS-серверы на стороне VPN, провайдер их не видит.',
          style: small,
        ),
        if (report.exit != null) ...[
          const SizedBox(height: 8),
          Text('Сайты видят вас из', style: small.copyWith(fontWeight: FontWeight.w600)),
          server(report.exit!),
        ],
        if (report.dns.isNotEmpty) ...[
          const SizedBox(height: 8),
          Text('DNS-серверы', style: small.copyWith(fontWeight: FontWeight.w600)),
          for (final d in report.dns) server(d, bad: suspicious.contains(d)),
        ],
      ],
    );
  }
}

class _Box extends StatelessWidget {
  final Color color;
  final IconData icon;
  final String title;
  final List<Widget> children;
  const _Box({required this.color, required this.icon, required this.title, required this.children});

  @override
  Widget build(BuildContext context) => Container(
    margin: const EdgeInsets.only(top: 4, bottom: 8),
    padding: const EdgeInsets.all(12),
    decoration: BoxDecoration(
      color: color.withValues(alpha: .07),
      borderRadius: BorderRadius.circular(10),
      border: Border.all(color: color.withValues(alpha: .35)),
    ),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 18, color: color),
        const SizedBox(width: 10),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
              const SizedBox(height: 4),
              ...children,
            ],
          ),
        ),
      ],
    ),
  );
}
