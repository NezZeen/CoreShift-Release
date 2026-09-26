import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../state/app_state.dart';
import '../theme.dart';
import '../widgets.dart';

enum _Filter { all, swap, cores, errors }

class LogsPage extends StatefulWidget {
  final AppState state;
  const LogsPage({super.key, required this.state});

  @override
  State<LogsPage> createState() => _LogsPageState();
}

class _LogsPageState extends State<LogsPage> {
  _Filter filter = _Filter.all;
  bool coreOutput = false;
  final _scroll = ScrollController();
  bool _follow = true;

  @override
  void initState() {
    super.initState();
    _scroll.addListener(() {
      if (!_scroll.hasClients) return;
      _follow = _scroll.position.pixels >= _scroll.position.maxScrollExtent - 24;
    });
  }

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  bool _keep(LogLine l) {
    final isCore = allCores.contains(l.source);
    if (!coreOutput && isCore && l.level == LogLevel.info && !_isEvent(l)) return false;
    return switch (filter) {
      _Filter.all => true,
      _Filter.swap => l.level == LogLevel.swap,
      _Filter.cores => isCore,
      _Filter.errors => l.level == LogLevel.err || l.level == LogLevel.warn,
    };
  }

  // Core output lines are noisy; lifecycle messages are kept regardless.
  static bool _isEvent(LogLine l) => const {'запуск', 'работает', 'проверка связи'}.contains(l.message);

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final lines = widget.state.logs.where(_keep).toList();
    if (_follow) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (_scroll.hasClients) _scroll.jumpTo(_scroll.position.maxScrollExtent);
      });
    }
    return Padding(
      padding: const EdgeInsets.fromLTRB(28, 24, 28, 24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PageHeader(
            'Журнал',
            subtitle: 'События службы, ядер и автосвапа.',
            actions: [
              Seg<_Filter>(
                value: filter,
                options: const [(_Filter.all, 'Все'), (_Filter.swap, 'Автосвап'), (_Filter.cores, 'Ядра'), (_Filter.errors, 'Ошибки')],
                onChanged: (v) => setState(() => filter = v),
              ),
              Tooltip(
                message: 'Показывать построчный вывод ядер',
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Transform.scale(
                      scale: .8,
                      child: Switch(value: coreOutput, onChanged: (v) => setState(() => coreOutput = v)),
                    ),
                    Text('вывод ядер', style: TextStyle(fontSize: 12, color: p.muted)),
                  ],
                ),
              ),
              Btn(
                icon: Icons.copy,
                small: true,
                tooltip: 'Скопировать',
                onPressed: lines.isEmpty
                    ? null
                    : () {
                        final text = [...widget.state.diagnosticsHeader(), for (final l in lines) '${_time(l.time)}  ${l.source}  ${l.message}'];
                        Clipboard.setData(ClipboardData(text: text.join('\n')));
                        widget.state.toast('Журнал скопирован');
                      },
              ),
              Btn(label: 'Очистить', small: true, onPressed: widget.state.clearLogs),
            ],
          ),
          Expanded(
            child: Container(
              decoration: BoxDecoration(
                color: p.bg2,
                borderRadius: BorderRadius.circular(14),
                border: Border.all(color: p.border),
              ),
              child: lines.isEmpty
                  ? Center(
                      child: Text('Пусто', style: TextStyle(color: p.dim)),
                    )
                  : Scrollbar(
                      controller: _scroll,
                      child: SelectionArea(
                        child: ListView.builder(
                          controller: _scroll,
                          padding: const EdgeInsets.symmetric(vertical: 12),
                          itemCount: lines.length,
                          itemBuilder: (context, i) => _Line(lines[i]),
                        ),
                      ),
                    ),
            ),
          ),
        ],
      ),
    );
  }
}

String _time(DateTime t) => '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}:${t.second.toString().padLeft(2, '0')}';

class _Line extends StatelessWidget {
  final LogLine l;
  const _Line(this.l);

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final isCore = allCores.contains(l.source);
    final srcColor = isCore ? coreStyle(l.source).color : (l.level == LogLevel.swap ? swapColor : p.muted);
    final msgColor = switch (l.level) {
      LogLevel.err => errColor,
      LogLevel.warn => warnColor,
      LogLevel.ok => okColor,
      LogLevel.swap => swapColor,
      LogLevel.info => p.text,
    };
    const mono = TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 12.5, height: 1.45);
    return Container(
      color: l.level == LogLevel.swap ? swapColor.withValues(alpha: .08) : null,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 1),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 78,
            child: Text(_time(l.time), style: mono.copyWith(color: p.dim)),
          ),
          SizedBox(
            width: 100,
            child: Text(
              isCore ? coreStyle(l.source).name : l.source,
              style: mono.copyWith(color: srcColor, fontWeight: FontWeight.w500),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          Expanded(
            child: Text(
              l.message,
              style: mono.copyWith(color: msgColor, fontWeight: l.level == LogLevel.swap ? FontWeight.w500 : null),
            ),
          ),
        ],
      ),
    );
  }
}
