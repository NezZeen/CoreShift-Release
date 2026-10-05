import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../state/app_state.dart';
import '../theme.dart';
import '../widgets.dart';

enum _Filter { all, errors }

class LogsPage extends StatefulWidget {
  final AppState state;
  const LogsPage({super.key, required this.state});

  @override
  State<LogsPage> createState() => _LogsPageState();
}

class _LogsPageState extends State<LogsPage> {
  _Filter filter = _Filter.all;
  String query = '';
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

  /// Of the cores' own output the page shows the errors and warnings: the
  /// service groups their repeats into one line and a count. The rest
  /// drowns the events; a search finds it, and the copy for support has
  /// what was printed around a failure (AppState.journalForSupport).
  bool _keep(LogLine l) {
    if (query.isNotEmpty && !'${l.source} ${l.message}'.toLowerCase().contains(query)) return false;
    final isCore = allCores.contains(l.source);
    if (query.isEmpty && isCore && l.level == LogLevel.info && !_isEvent(l)) return false;
    return switch (filter) {
      _Filter.all => true,
      _Filter.errors => l.level == LogLevel.err || l.level == LogLevel.warn,
    };
  }

  // Core output lines are noisy; lifecycle messages are kept regardless.
  static bool _isEvent(LogLine l) => const {'запуск', 'работает', 'проверка связи'}.contains(l.message);

  // The lines shown, filtered again only when the journal, the filter or
  // the search changed: the page is rebuilt every second while connected
  // (traffic), with up to AppState.logsKeep lines.
  List<LogLine> _lines = const [];
  (int, _Filter, String)? _linesFor;

  List<LogLine> _shown() {
    final key = (widget.state.logsRevision, filter, query);
    if (key != _linesFor) {
      _lines = widget.state.logs.where(_keep).toList();
      _linesFor = key;
    }
    return _lines;
  }

  /// Scrolls to the newest line. The list only estimates the height of the
  /// lines it has not laid out, so the end moves as it gets there: a few
  /// frames follow it.
  void _toEnd([int frames = 4]) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !_scroll.hasClients) return;
      final end = _scroll.position.maxScrollExtent;
      if (_scroll.position.pixels == end) return;
      _scroll.jumpTo(end);
      if (frames > 1) _toEnd(frames - 1);
    });
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final before = _linesFor;
    final lines = _shown();
    if (_follow && before != _linesFor) _toEnd();
    final compact = isCompact(context);
    final search = TextField(
      onChanged: (v) => setState(() => query = v.trim().toLowerCase()),
      style: const TextStyle(fontSize: 13),
      decoration: InputDecoration(
        isDense: true,
        hintText: 'Поиск',
        prefixIcon: Icon(Icons.search, size: 16, color: p.dim),
        prefixIconConstraints: const BoxConstraints(minWidth: 32),
      ),
    );
    final seg = Seg<_Filter>(value: filter, options: const [(_Filter.all, 'Все'), (_Filter.errors, 'Ошибки')], onChanged: (v) => setState(() => filter = v));
    // The whole journal with the cores' warnings and errors: what support needs.
    final copy = Btn(
      label: 'Копировать',
      icon: Icons.copy,
      small: true,
      tooltip: 'Скопировать весь журнал с версиями и режимом, для поддержки',
      onPressed: widget.state.logs.isEmpty
          ? null
          : () {
              Clipboard.setData(ClipboardData(text: widget.state.journalForSupport().join('\n')));
              widget.state.toast('Журнал скопирован');
            },
    );
    return Padding(
      padding: compact ? const EdgeInsets.fromLTRB(16, 16, 16, 12) : const EdgeInsets.fromLTRB(32, 28, 32, 24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          // On a phone the search and the filter take a row of their own
          // under the title: beside it they wrapped above it.
          PageHeader(
            'Журнал',
            subtitle: 'Подключения, смены ядер и ошибки. Скопируйте журнал, чтобы отправить его в поддержку.',
            actions: [
              if (!compact) ...[SizedBox(width: 220, child: search), seg],
              copy,
            ],
          ),
          if (compact)
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Row(
                children: [
                  Expanded(child: search),
                  const SizedBox(width: 8),
                  seg,
                ],
              ),
            ),
          Expanded(
            child: Container(
              decoration: BoxDecoration(
                color: p.bg2,
                borderRadius: BorderRadius.circular(12),
                border: Border.all(color: p.border),
              ),
              child: lines.isEmpty
                  ? Center(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Icon(Icons.receipt_long_outlined, size: 32, color: p.dim),
                          const SizedBox(height: 10),
                          Text(
                            'Пусто',
                            style: TextStyle(color: p.muted, fontWeight: FontWeight.w600),
                          ),
                          const SizedBox(height: 2),
                          Text(
                            'Здесь появятся подключения, смены ядер и ошибки',
                            style: TextStyle(color: p.dim, fontSize: 12),
                            textAlign: TextAlign.center,
                          ),
                        ],
                      ),
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
    final srcColor = isCore ? p.ink(coreStyle(l.source).color) : (l.level == LogLevel.swap ? p.ink(swapColor) : p.muted);
    final msgColor = switch (l.level) {
      LogLevel.err => p.errInk,
      LogLevel.warn => p.warnInk,
      LogLevel.ok => p.okInk,
      LogLevel.swap => p.ink(swapColor),
      LogLevel.info => p.text,
    };
    const mono = TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 12.5, height: 1.45);
    final source = isCore ? coreStyle(l.source).name : l.source;
    final message = mono.copyWith(color: msgColor, fontWeight: l.level == LogLevel.swap ? FontWeight.w500 : null);
    if (isCompact(context)) {
      // A phone has no room for columns: one paragraph that wraps.
      return Container(
        color: l.level == LogLevel.swap ? swapColor.withValues(alpha: .08) : null,
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 2),
        child: Text.rich(
          TextSpan(
            children: [
              TextSpan(
                text: '${_time(l.time)} ',
                style: mono.copyWith(color: p.dim, fontSize: 11.5),
              ),
              TextSpan(
                text: '$source ',
                style: mono.copyWith(color: srcColor, fontWeight: FontWeight.w500),
              ),
              TextSpan(text: l.message, style: message),
            ],
          ),
        ),
      );
    }
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
              source,
              style: mono.copyWith(color: srcColor, fontWeight: FontWeight.w500),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          Expanded(child: Text(l.message, style: message)),
        ],
      ),
    );
  }
}
