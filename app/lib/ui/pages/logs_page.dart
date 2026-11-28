import 'package:flutter/material.dart';

import '../../l10n/strings.dart';
import '../../l10n/ui_en.dart';
import '../../l10n/ui_ru.dart';
import '../../platform/platform.dart' as platform;
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
  // In both languages: a line keeps the words it was written in.
  static bool _isEvent(LogLine l) => _eventWords.contains(l.message);

  static final _eventWords = {
    for (final k in ['app.coreState.starting', 'app.coreState.connected', 'app.coreState.connecting']) ...[uiRu[k], uiEn[k]],
  };

  // The lines shown, filtered again only when the journal, the filter or
  // the search changed: the page is rebuilt every second while connected
  // (traffic), with up to AppState.logsKeep lines.
  List<LogLine> _lines = const [];
  List<_Entry> _entries = const [];
  int _errorCount = 0;
  (int, _Filter, String)? _linesFor;
  final Set<LogLine> _open = {};

  List<LogLine> _shown() {
    final key = (widget.state.logsRevision, filter, query);
    if (key != _linesFor) {
      _lines = widget.state.logs.where(_keep).toList();
      _entries = _group(_lines);
      _errorCount = widget.state.logs.where((l) => l.level == LogLevel.err).length;
      _linesFor = key;
    }
    return _lines;
  }

  /// Consecutive error lines of one source with one message become one
  /// entry; purely presentational, the journal itself is untouched.
  static List<_Entry> _group(List<LogLine> lines) {
    final out = <_Entry>[];
    for (final l in lines) {
      if (l.level == LogLevel.err && out.isNotEmpty) {
        final last = out.last;
        if (last.isError && last.first.source == l.source && last.first.message == l.message) {
          last.lines.add(l);
          continue;
        }
      }
      out.add(_Entry(l));
    }
    return out;
  }

  void _toggle(LogLine first) => setState(() {
    if (!_open.remove(first)) _open.add(first);
  });

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
  Widget build(BuildContext context) => ListenableBuilder(listenable: widget.state.logTick, builder: (context, _) => _page(context));

  Widget _page(BuildContext context) {
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
        hintText: tr('logs.search'),
        prefixIcon: Icon(Icons.search, size: 16, color: p.dim),
        prefixIconConstraints: const BoxConstraints(minWidth: 32),
      ),
    );
    final errLabel = tr('logs.filter.errorsN', {'n': '$_errorCount'});
    final seg = compact
        ? Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              _Pill(label: tr('logs.filter.all'), on: filter == _Filter.all, onTap: () => setState(() => filter = _Filter.all)),
              const SizedBox(width: 8),
              _Pill(label: errLabel, on: filter == _Filter.errors, red: true, onTap: () => setState(() => filter = _Filter.errors)),
            ],
          )
        : null;
    // The whole journal with the cores' warnings and errors: what support needs.
    final copy = Btn(
      label: compact ? null : tr('logs.copy'),
      icon: Icons.copy,
      small: true,
      tooltip: platform.isAndroid ? tr('logs.copy.tipAndroid') : tr('logs.copy.tipDesktop'),
      onPressed: widget.state.logs.isEmpty
          ? null
          : () async {
              final s = widget.state;
              final path = await s.copyJournal();
              if (path == null) {
                s.toast(tr('logs.copy.done'));
              } else if (platform.isAndroid) {
                s.toast(tr('logs.copy.savedTo', {'path': path}), ToastKind.ok);
              } else {
                s.toast(tr('logs.copy.savedDownloads'), ToastKind.ok, (tr('logs.copy.show'), () => platform.revealFile(path)));
              }
            },
    );
    // «Подробно»: the cores' and the TUN layer's harmless lines too, and the
    // cores tell more from the next connection. An older service has no
    // such setting.
    final s = widget.state;
    final verbose = s.setting('log.verbose', false) == true;
    Future<void> toggleVerbose() async {
      final err = await s.updateSettings((x) {
        x['log'] = {...?(x['log'] as Map?)?.cast<String, dynamic>(), 'verbose': !verbose};
      });
      if (err == null) {
        s.toast(verbose ? tr('logs.verbose.toastOff') : (s.status.active ? tr('logs.verbose.toastOnActive') : tr('logs.verbose.toastOn')));
      }
    }

    final canVerbose = s.hasSetting('log.verbose');
    final VoidCallback? verboseTap = s.online ? toggleVerbose : null;
    final Widget? detail = !canVerbose
        ? null
        : compact
        ? Btn(
            icon: verbose ? Icons.toggle_on : Icons.toggle_off_outlined,
            small: true,
            kind: verbose ? BtnKind.primary : BtnKind.normal,
            tooltip: verbose ? tr('logs.verbose.tipOn') : tr('logs.verbose.tipOff'),
            onPressed: verboseTap,
          )
        : Tooltip(
            message: verbose ? tr('logs.verbose.tipOn') : tr('logs.verbose.tipOff'),
            child: _Pill(label: '● ${tr('logs.verbose')}', on: verbose, filled: true, onTap: verboseTap),
          );
    final groups = _entries.where((e) => e.isError).length;
    final footer = Padding(
      padding: const EdgeInsets.only(top: 8),
      child: Text(tr('logs.footer', {'groups': '$groups', 'events': '${lines.length}'}), style: TextStyle(color: p.muted, fontSize: 11.5)),
    );
    return Padding(
      padding: compact ? const EdgeInsets.fromLTRB(16, 16, 16, 12) : const EdgeInsets.fromLTRB(32, 28, 32, 24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PageHeader(
            tr('logs.title'),
            subtitle: compact ? null : tr('logs.subtitle'),
            actions: [
              if (!compact) ...[
                SizedBox(width: 220, child: search),
                _Pill(label: tr('logs.filter.all'), on: filter == _Filter.all, onTap: () => setState(() => filter = _Filter.all)),
                _Pill(label: errLabel, on: filter == _Filter.errors, red: true, onTap: () => setState(() => filter = _Filter.errors)),
              ],
              ?detail,
              copy,
            ],
          ),
          if (compact)
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [search, const SizedBox(height: 10), ?seg]),
            ),
          Expanded(
            child: Container(
              decoration: compact
                  ? null
                  : BoxDecoration(
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
                            tr('logs.empty.title'),
                            style: TextStyle(color: p.muted, fontWeight: FontWeight.w600),
                          ),
                          const SizedBox(height: 2),
                          Text(
                            tr('logs.empty.text'),
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
                          padding: EdgeInsets.symmetric(vertical: compact ? 0 : 12),
                          itemCount: _entries.length,
                          itemBuilder: (context, i) {
                            final e = _entries[i];
                            return e.isError ? _ErrorEntry(e, open: _open.contains(e.first), onTap: () => _toggle(e.first)) : _Line(e.first);
                          },
                        ),
                      ),
                    ),
            ),
          ),
          footer,
        ],
      ),
    );
  }
}

String _time(DateTime t) => '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}:${t.second.toString().padLeft(2, '0')}';

/// One entry of the page: an error line with its repeats, or a plain line.
class _Entry {
  final List<LogLine> lines;
  _Entry(LogLine first) : lines = [first];
  LogLine get first => lines.first;
  bool get isError => first.level == LogLevel.err;
  String get range => _time(first.time) == _time(lines.last.time) ? _time(first.time) : '${_time(first.time)} – ${_time(lines.last.time)}';
}

const _mono = TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 12.5, height: 1.45);

/// A user's action or a settings change: shown in amber.
final _actionSources = {
  for (final k in ['app.src.action', 'app.src.settings']) ...[uiRu[k], uiEn[k]],
};

bool _isAction(LogLine l) => _actionSources.contains(l.source);

String _sourceName(LogLine l) => allCores.contains(l.source) ? coreStyle(l.source).name : l.source;

Color _sourceColor(BuildContext context, LogLine l) {
  final p = context.pal;
  if (allCores.contains(l.source)) return p.ink(coreStyle(l.source).color);
  if (l.level == LogLevel.swap) return p.ink(swapColor);
  if (_isAction(l)) return p.accentInk;
  return p.muted;
}

class _Pill extends StatelessWidget {
  final String label;
  final bool on;
  final bool red;
  final bool filled;
  final VoidCallback? onTap;
  const _Pill({required this.label, required this.on, this.red = false, this.filled = false, this.onTap});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final Color bg, fg, border;
    if (red) {
      bg = on ? errColor.withValues(alpha: .16) : Colors.transparent;
      fg = p.errInk;
      border = errColor.withValues(alpha: on ? .6 : .4);
    } else if (filled && on) {
      bg = accent;
      fg = onAccent;
      border = accent;
    } else {
      bg = on ? p.surface2 : Colors.transparent;
      fg = on ? p.text : p.muted;
      border = on ? p.border2 : p.border;
    }
    return Opacity(
      opacity: onTap == null ? .5 : 1,
      child: Material(
        color: bg,
        shape: StadiumBorder(side: BorderSide(color: border)),
        child: InkWell(
          customBorder: const StadiumBorder(),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 7),
            child: Text(
              label,
              style: TextStyle(color: fg, fontSize: 12.5, fontWeight: FontWeight.w600),
            ),
          ),
        ),
      ),
    );
  }
}

class _ErrorEntry extends StatelessWidget {
  final _Entry e;
  final bool open;
  final VoidCallback onTap;
  const _ErrorEntry(this.e, {required this.open, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final l = e.first;
    final n = e.lines.length;
    final compact = isCompact(context);
    final badge = n > 1
        ? Container(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
            decoration: BoxDecoration(color: errColor.withValues(alpha: .22), borderRadius: BorderRadius.circular(99)),
            child: Text(
              '×$n',
              style: _mono.copyWith(color: p.errInk, fontSize: 12, fontWeight: FontWeight.w700),
            ),
          )
        : null;
    final msg = _mono.copyWith(color: p.errInk);
    final Widget head;
    if (compact) {
      head = Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 8,
                height: 8,
                decoration: const BoxDecoration(color: errColor, shape: BoxShape.circle),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  '${_sourceName(l)} · ${tr('logs.err')}',
                  style: TextStyle(color: p.text, fontWeight: FontWeight.w700, fontSize: 13.5),
                ),
              ),
              ?badge,
            ],
          ),
          const SizedBox(height: 4),
          Text(l.message, style: msg),
          const SizedBox(height: 4),
          Text(e.range, style: _mono.copyWith(color: p.muted, fontSize: 11.5)),
        ],
      );
    } else {
      head = Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 76,
            child: Text(_time(l.time), style: _mono.copyWith(color: p.muted)),
          ),
          SizedBox(
            width: 96,
            child: Text(
              _sourceName(l),
              style: _mono.copyWith(color: _sourceColor(context, l), fontWeight: FontWeight.w500),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          Expanded(
            child: Text.rich(
              TextSpan(
                children: [
                  TextSpan(text: l.message, style: msg),
                  if (n > 1)
                    TextSpan(
                      text: '   ${e.range}',
                      style: _mono.copyWith(color: p.muted, fontSize: 11.5),
                    ),
                ],
              ),
            ),
          ),
          if (badge != null) ...[const SizedBox(width: 8), badge],
        ],
      );
    }
    final radius = BorderRadius.circular(compact ? 14 : 8);
    return Padding(
      padding: compact ? const EdgeInsets.only(bottom: 8) : const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
      child: Material(
        color: errColor.withValues(alpha: .14),
        shape: RoundedRectangleBorder(
          borderRadius: radius,
          side: BorderSide(color: errColor.withValues(alpha: .35)),
        ),
        child: InkWell(
          customBorder: RoundedRectangleBorder(borderRadius: radius),
          onTap: n > 1 ? onTap : null,
          child: Padding(
            padding: compact ? const EdgeInsets.all(12) : const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                head,
                if (open && n > 1)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        for (final x in e.lines)
                          Padding(
                            padding: const EdgeInsets.symmetric(vertical: 1),
                            child: Text.rich(
                              TextSpan(
                                children: [
                                  TextSpan(
                                    text: '${_time(x.time)}  ',
                                    style: _mono.copyWith(color: p.muted, fontSize: 11.5),
                                  ),
                                  TextSpan(text: x.message, style: msg),
                                ],
                              ),
                            ),
                          ),
                      ],
                    ),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _Line extends StatelessWidget {
  final LogLine l;
  const _Line(this.l);

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final action = _isAction(l);
    final srcColor = _sourceColor(context, l);
    final msgColor = action
        ? p.accentInk
        : switch (l.level) {
            LogLevel.err => p.errInk,
            LogLevel.warn => p.warnInk,
            LogLevel.ok => p.okInk,
            LogLevel.swap => p.ink(swapColor),
            LogLevel.info => p.text,
          };
    final source = _sourceName(l);
    final message = _mono.copyWith(color: msgColor, fontWeight: l.level == LogLevel.swap ? FontWeight.w500 : null);
    final tint = l.level == LogLevel.swap ? swapColor.withValues(alpha: .08) : null;
    if (isCompact(context)) {
      // A phone has no room for columns: compact lines that wrap.
      return Container(
        color: tint,
        padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 3),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.baseline,
          textBaseline: TextBaseline.alphabetic,
          children: [
            Text(_time(l.time), style: _mono.copyWith(color: p.muted, fontSize: 11.5)),
            const SizedBox(width: 8),
            Expanded(
              child: Text.rich(
                TextSpan(
                  children: [
                    if (action)
                      TextSpan(
                        text: '● ',
                        style: _mono.copyWith(color: p.accentInk, fontSize: 9),
                      ),
                    TextSpan(
                      text: '$source ',
                      style: _mono.copyWith(color: srcColor, fontWeight: FontWeight.w500),
                    ),
                    TextSpan(text: l.message, style: message),
                  ],
                ),
              ),
            ),
          ],
        ),
      );
    }
    return Container(
      color: tint,
      padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 1),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 76,
            child: Text(_time(l.time), style: _mono.copyWith(color: p.muted)),
          ),
          SizedBox(
            width: 96,
            child: Text.rich(
              TextSpan(
                children: [
                  if (action)
                    TextSpan(
                      text: '● ',
                      style: _mono.copyWith(color: p.accentInk, fontSize: 9),
                    ),
                  TextSpan(text: source),
                ],
              ),
              style: _mono.copyWith(color: srcColor, fontWeight: FontWeight.w500),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          Expanded(child: Text(l.message, style: message)),
        ],
      ),
    );
  }
}
