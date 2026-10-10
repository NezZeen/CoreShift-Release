import 'dart:async';

import 'package:flutter/material.dart';

import '../platform/platform.dart' as platform;
import '../state/app_state.dart';
import 'pages/home_page.dart' show showQuickPick;
import 'shell.dart';
import 'theme.dart';
import 'widgets.dart';

/// «Проверить всё»: starts the checkup unless one runs, and shows it, a
/// sheet on a phone, a window on the desktop. The steps tick off as they
/// finish, then the verdict says what is wrong and offers what helps.
Future<void> showCheckup(BuildContext context, AppState state) {
  if (!state.checkup.running) unawaited(state.runCheckup());
  // The page's navigation, for the actions: the window's own context is
  // above it.
  final nav = context.getInheritedWidgetOfExactType<Nav>();
  void go(PageId id) => nav?.go(id);
  final p = context.pal;
  final view = CheckupView(state: state, opener: context, go: go);
  if (isCompact(context)) {
    return showModalBottomSheet<void>(
      context: context,
      backgroundColor: p.surface,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (c) => SafeArea(
        child: ConstrainedBox(
          constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(c).height * .85),
          child: view,
        ),
      ),
    );
  }
  return showDialog<void>(
    context: context,
    builder: (c) => Dialog(
      child: ConstrainedBox(constraints: const BoxConstraints(maxWidth: 560), child: view),
    ),
  );
}

/// The checkup's steps, verdict and actions.
class CheckupView extends StatefulWidget {
  final AppState state;

  /// The page that opened it, which outlives the window: another server is
  /// picked from there.
  final BuildContext opener;
  final ValueChanged<PageId> go;
  const CheckupView({super.key, required this.state, required this.opener, required this.go});

  @override
  State<CheckupView> createState() => _CheckupViewState();
}

class _CheckupViewState extends State<CheckupView> {
  /// Where «Отправить в поддержку» saved the report, once it did; '' when
  /// it went to the clipboard only.
  String? _saved;
  bool _sending = false;

  AppState get state => widget.state;

  Future<void> _send() async {
    setState(() => _sending = true);
    final path = await state.sendCheckup();
    if (!mounted) return;
    setState(() {
      _sending = false;
      _saved = path ?? '';
    });
  }

  /// Runs an action of the verdict: the window closes first, as the action
  /// leads elsewhere or changes the connection the window describes.
  void _act(String action) {
    Navigator.pop(context);
    switch (action) {
      case 'servers':
        if (widget.opener.mounted) {
          showQuickPick(widget.opener, state, onAll: () => widget.go(PageId.servers));
        } else {
          widget.go(PageId.servers);
        }
      case 'reconnect':
        state.reconnect();
      case 'connect':
        state.connect();
      case 'routing':
        if (state.directFixable) {
          state.fixDirect();
        } else {
          widget.go(PageId.routing);
        }
      case 'leak':
        widget.go(PageId.settings);
        state.runLeakTest();
    }
  }

  String _actionLabel(String a) => switch (a) {
    'servers' => 'Другой сервер',
    'reconnect' => 'Переподключить',
    'connect' => 'Подключить',
    'routing' => state.directFixable ? 'Всё через VPN' : 'Правила',
    'leak' => 'Проверка утечки DNS',
    _ => a,
  };

  @override
  Widget build(BuildContext context) => ListenableBuilder(listenable: state, builder: (context, _) => _build(context));

  Widget _build(BuildContext context) {
    final p = context.pal;
    final compact = isCompact(context);
    final c = state.checkup;
    final v = c.verdict;
    final note = c.running
        ? 'Проверяем сеть, сервер и VPN — до полуминуты'
        : c.at == null
        ? ''
        : 'Проверено ${formatAgo(c.at)}${c.server.isEmpty ? '' : ' · сервер «${c.server}»'}';
    final actions = v == null ? const <String>[] : v.actions.where((a) => !(a == 'connect' && state.status.active)).toList();
    return SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(compact ? 16 : 22, compact ? 0 : 20, compact ? 16 : 22, 18),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('Проверить всё', style: dialogTitle),
          if (note.isNotEmpty) ...[const SizedBox(height: 2), Text(note, style: TextStyle(fontSize: 12.5, color: p.muted))],
          const SizedBox(height: 14),
          if (v != null) ...[
            _VerdictBox(
              verdict: v,
              actions: [
                for (final (i, a) in actions.indexed)
                  Btn(label: _actionLabel(a), small: true, kind: i == 0 ? BtnKind.primary : BtnKind.normal, onPressed: state.busy ? null : () => _act(a)),
              ],
            ),
            const SizedBox(height: 12),
          ],
          if (c.error.isNotEmpty) ...[
            _VerdictBox(
              verdict: CheckVerdict(status: 'fail', title: 'Проверка не удалась', advice: c.error),
              actions: const [],
            ),
            const SizedBox(height: 12),
          ],
          for (final s in c.steps) _StepRow(step: s, pending: c.running),
          const SizedBox(height: 14),
          if (_saved != null)
            Padding(
              padding: const EdgeInsets.only(bottom: 10),
              child: Row(
                children: [
                  const Icon(Icons.check, size: 15, color: okColor),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      _saved!.isEmpty
                          ? 'Отчёт с журналом скопирован: вставьте его в чат поддержки'
                          : platform.isAndroid
                          ? 'Отчёт с журналом скопирован и сохранён в $_saved'
                          : 'Отчёт с журналом скопирован и сохранён в «Загрузки»',
                      style: TextStyle(fontSize: 12, color: p.muted),
                    ),
                  ),
                  if (_saved!.isNotEmpty && !platform.isAndroid) Btn(label: 'Показать', small: true, onPressed: () => platform.revealFile(_saved!)),
                ],
              ),
            ),
          Wrap(
            alignment: WrapAlignment.end,
            spacing: 8,
            runSpacing: 8,
            children: [
              Btn(
                label: 'Отправить в поддержку',
                icon: Icons.support_agent,
                loading: _sending,
                tooltip: 'Отчёт и журнал — в буфер обмена и в файл. Ссылок подписок и ключей в нём нет',
                onPressed: c.running ? null : _send,
              ),
              Btn(label: 'Ещё раз', icon: Icons.refresh, loading: c.running, onPressed: c.running || !state.online ? null : state.runCheckup),
              if (!compact) Btn(label: 'Закрыть', onPressed: () => Navigator.pop(context)),
            ],
          ),
        ],
      ),
    );
  }
}

/// A step: a spinner while it runs, then ✓, ! or ✗, its name and what it saw.
class _StepRow extends StatelessWidget {
  final CheckStep step;

  /// The checkup still runs: a step without a result is under way.
  final bool pending;
  const _StepRow({required this.step, required this.pending});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final Widget mark = switch (step.status) {
      'ok' => const Icon(Icons.check_circle, size: 17, color: okColor, semanticLabel: 'в порядке'),
      'warn' => const Icon(Icons.error, size: 17, color: warnColor, semanticLabel: 'внимание'),
      'fail' => const Icon(Icons.cancel, size: 17, color: errColor, semanticLabel: 'не работает'),
      'skipped' => Icon(Icons.remove_circle_outline, size: 17, color: p.dim, semanticLabel: 'пропущено'),
      _ when pending => const SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2)),
      _ => Icon(Icons.help_outline, size: 17, color: p.dim),
    };
    final detail = step.running ? (pending ? 'проверяем…' : 'не закончено') : step.detail;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(width: 20, height: 20, child: Center(child: mark)),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  step.title,
                  style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: step.status == 'skipped' ? p.muted : null),
                ),
                if (detail.isNotEmpty) Text(detail, style: TextStyle(fontSize: 12, color: step.status == 'skipped' ? p.dim : p.muted)),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// The verdict: what is most likely wrong, what to do, and the buttons.
class _VerdictBox extends StatelessWidget {
  final CheckVerdict verdict;
  final List<Widget> actions;
  const _VerdictBox({required this.verdict, required this.actions});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final (color, icon) = switch (verdict.status) {
      'ok' => (okColor, Icons.verified_outlined),
      'warn' => (warnColor, Icons.warning_amber_rounded),
      _ => (errColor, Icons.error_outline),
    };
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: color.withValues(alpha: .08),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: color.withValues(alpha: .4)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 20, color: color),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(verdict.title, style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600)),
                if (verdict.advice.isNotEmpty) ...[const SizedBox(height: 3), Text(verdict.advice, style: TextStyle(fontSize: 12.5, color: p.muted))],
                if (actions.isNotEmpty) ...[const SizedBox(height: 10), Wrap(spacing: 8, runSpacing: 6, children: actions)],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// The settings' line for «Проверить всё», beside the leak test.
class CheckupSettingRow extends StatelessWidget {
  final AppState state;
  const CheckupSettingRow({super.key, required this.state});

  @override
  Widget build(BuildContext context) => SettingRow(
    title: 'Проверить всё',
    description: 'Сеть, DNS, сервер, связь через VPN, утечку DNS и скорость — за полминуты, с советом, что делать. Итог можно отправить в поддержку',
    trailing: Btn(label: 'Проверить всё', icon: Icons.fact_check_outlined, loading: state.checkup.running, onPressed: () => showCheckup(context, state)),
  );
}
