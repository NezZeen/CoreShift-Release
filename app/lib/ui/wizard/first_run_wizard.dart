import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../api/models.dart';
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../../state/import_link.dart' show ImportLink, linkFingerprint, parseImportLink;
import '../../state/redact.dart';
import '../countries.dart';
import '../pages/servers_page.dart' show showAddSubscription;
import '../theme.dart';
import '../widgets.dart';
import 'wizard_modes.dart';
import 'wizard_strings.dart';

/// The pref that records the wizard was finished or skipped: it is shown
/// once, like the disclaimer.
const wizardPref = 'wizard_done';

/// The wizard is due: the app asks for it ([AppState.askWizard]), the service
/// answers, the user has no subscription and has not seen the wizard.
bool wizardDue(AppState s) => s.askWizard && s.loaded && s.online && s.prefs[wizardPref] != true && s.subscriptions.isEmpty;

/// A user who already has subscriptions never sees the wizard, not even after
/// removing them all: the first time the app sees them, it is marked as done.
void wizardSettle(AppState s) {
  if (s.askWizard && s.loaded && s.subscriptions.isNotEmpty && s.prefs[wizardPref] != true) s.setPref(wizardPref, true);
}

/// Shows the wizard over [context]'s page and remembers that it was shown,
/// however it ends. «Добавить сервер вручную» ends it with the usual add
/// dialog. [android] tells which modes to offer; by default the system's.
Future<void> showFirstRunWizard(BuildContext context, AppState state, {bool? android}) async {
  var manual = false;
  try {
    manual =
        await Navigator.of(context).push<bool>(
          PageRouteBuilder<bool>(
            opaque: false,
            barrierColor: Colors.black54,
            barrierDismissible: false,
            transitionDuration: const Duration(milliseconds: 200),
            reverseTransitionDuration: const Duration(milliseconds: 150),
            pageBuilder: (_, _, _) => FirstRunWizard(state: state, android: android ?? platform.isAndroid),
            transitionsBuilder: (_, animation, _, child) => FadeTransition(opacity: animation, child: child),
          ),
        ) ??
        false;
  } finally {
    state.setPref(wizardPref, true);
  }
  if (manual && context.mounted) await showAddSubscription(context, state);
}

enum _Step { welcome, add, check, mode, done }

/// The first-run wizard: welcome, a subscription, a check of the servers,
/// the mode and the connection. A phone gets full-screen pages, a desktop a
/// card in the middle.
class FirstRunWizard extends StatefulWidget {
  final AppState state;
  final bool android;
  const FirstRunWizard({super.key, required this.state, required this.android});

  @override
  State<FirstRunWizard> createState() => _FirstRunWizardState();
}

class _FirstRunWizardState extends State<FirstRunWizard> with WidgetsBindingObserver {
  AppState get s => widget.state;
  _Step step = _Step.welcome;

  // The subscription step. The link is kept out of the field the moment it
  // is pasted, and only its masked form is shown: the path carries a token.
  final _field = TextEditingController();
  int _fieldLen = 0;
  ImportLink? _link;
  bool _prefilled = false;
  bool _busy = false;
  String? _error;

  // The servers step.
  String _subId = '';
  bool _testing = false;
  bool _tested = false;
  String? _choice; // 'auto', a fingerprint, or null for the best one
  ({String sub, String fingerprint, String name})? _picked;

  // The mode step.
  String? _modeId;

  late final List<WizardMode> _modes = wizardModes(android: widget.android);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _field.addListener(_onField);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _field.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // Back from the browser with a link copied.
    if (state == AppLifecycleState.resumed && step == _Step.add && _link == null && _field.text.isEmpty && !_busy) _prefill();
  }

  // ---------------------------------------------------------------- navigation

  void _go(_Step to) => setState(() {
    step = to;
    _error = null;
  });

  void _skip() => Navigator.of(context).pop(false);

  void _back() {
    if (_busy) return;
    switch (step) {
      case _Step.welcome:
        _skip();
      case _Step.add:
        _go(_Step.welcome);
      case _Step.check:
        // The subscription is added; its link is not asked for again.
        _go(_Step.add);
      case _Step.mode:
        _go(_Step.check);
      case _Step.done:
        _go(_Step.mode);
    }
  }

  VoidCallback? get _primary => switch (step) {
    _Step.welcome => () {
      _go(_Step.add);
      _prefill();
    },
    _Step.add => _hasInput && !_busy ? _add : null,
    _Step.check => _canContinueCheck ? _afterCheck : null,
    _Step.mode => _modeId != null && !_busy ? _afterMode : null,
    _Step.done => _connect,
  };

  // ---------------------------------------------------------------- subscription

  bool get _hasInput => _link != null || _field.text.trim().isNotEmpty;

  /// Typing changes the field by a character; a paste by many. A paste is
  /// taken out of the field at once, so the link is never on the screen.
  void _onField() {
    final grew = _field.text.length - _fieldLen;
    _fieldLen = _field.text.length;
    if (grew > 1) {
      _absorb(_field.text);
    } else {
      setState(() => _error = null);
    }
  }

  /// Reads [text] as a link or servers; whatever it is not stays in the field.
  void _absorb(String text) {
    final t = text.trim();
    final link = parseImportLink(t);
    setState(() {
      _error = null;
      if (link == null) {
        if (_field.text != text) _field.text = text;
        return;
      }
      _field.clear();
      _fieldLen = 0;
      if (link.error.isNotEmpty) {
        _link = null;
        _error = link.error;
      } else {
        _link = link;
      }
    });
  }

  /// The link of an "add to app" button that opened CoreShift, else a
  /// subscription link on the clipboard, as the import offer would take it.
  Future<void> _prefill() async {
    if (_prefilled && _link != null) return;
    final pending = s.pendingImport;
    if (pending != null && pending.error.isEmpty) {
      setState(() => _link = pending);
      return;
    }
    if (!s.clipboardImport) return;
    String? text;
    try {
      text = (await Clipboard.getData(Clipboard.kTextPlain))?.text;
    } catch (_) {
      return;
    }
    if (!mounted || text == null || _link != null || _field.text.isNotEmpty) return;
    final link = parseImportLink(text, strict: true);
    if (link == null || link.error.isNotEmpty) return;
    // Offered here, so the import offer does not repeat it afterwards.
    s.setPref('clipboard_offered', linkFingerprint(link.source));
    setState(() {
      _link = link;
      _prefilled = true;
    });
  }

  Future<void> _paste() async {
    String? text;
    try {
      text = (await Clipboard.getData(Clipboard.kTextPlain))?.text;
    } catch (_) {}
    if (!mounted) return;
    if (text == null || text.trim().isEmpty) {
      setState(() => _error = WizardStrings.addClipboardEmpty);
      return;
    }
    _absorb(text);
    if (_link == null && _error == null) _fieldLen = _field.text.length;
  }

  void _clearLink() => setState(() {
    _link = null;
    _prefilled = false;
    _error = null;
  });

  Future<void> _add() async {
    if (_busy) return;
    if (_link == null && _field.text.trim().isNotEmpty) _absorb(_field.text);
    final link = _link;
    final source = link?.source ?? _field.text.trim();
    if (source.isEmpty) {
      setState(() => _error = WizardStrings.addEmpty);
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    final before = {for (final sub in s.subscriptions) sub.id};
    final err = await s.addSubscription(source: source, name: link?.name ?? '');
    if (!mounted) return;
    if (err != null) {
      setState(() {
        _busy = false;
        _error = redactForSupport(err);
      });
      return;
    }
    if (s.pendingImport != null && s.pendingImport!.source == source) s.dismissImport();
    final added = s.subscriptions.where((x) => !before.contains(x.id)).lastOrNull ?? s.subscriptions.lastOrNull;
    setState(() {
      _busy = false;
      _subId = added?.id ?? '';
      _choice = null;
      _tested = false;
    });
    _go(_Step.check);
    _runTest();
  }

  // ---------------------------------------------------------------- servers

  Subscription? get _sub => s.subscriptionById(_subId);

  Future<void> _runTest() async {
    if (_testing || _sub == null) return;
    setState(() {
      _testing = true;
      _tested = false;
    });
    await s.testLatency(_subId);
    if (!mounted) return;
    setState(() {
      _testing = false;
      _tested = true;
    });
  }

  /// The servers some core can run, the quickest first: the ones that
  /// answered, then those that did not.
  List<(NodeView, Latency?)> _ranked(Subscription sub) {
    final rows = [
      for (final n in sub.nodes)
        if (n.cores.isNotEmpty) (n, s.latencyOf(sub.id, n.fingerprint)),
    ];
    final answered = rows.where((r) => r.$2?.ok == true).toList()..sort((a, b) => a.$2!.ms.compareTo(b.$2!.ms));
    return [...answered, ...rows.where((r) => r.$2?.ok != true)];
  }

  /// The subscription's own automatic selection, when it has one that can be
  /// run: the quickest of those servers is connected, and the engine moves
  /// to the next of them by itself.
  NodeView? _autoNode(Subscription sub) {
    final inAuto = _ranked(sub).where((r) => sub.auto.contains(r.$1.fingerprint));
    return inAuto.firstOrNull?.$1;
  }

  /// What is chosen: the user's pick, or Автовыбор when the subscription has
  /// it, else the quickest server.
  String? _effectiveChoice(Subscription sub) {
    final c = _choice;
    if (c != null) return c;
    if (_autoNode(sub) != null) return 'auto';
    return _ranked(sub).firstOrNull?.$1.fingerprint;
  }

  NodeView? _resolve(Subscription sub) {
    final c = _effectiveChoice(sub);
    if (c == null) return null;
    if (c == 'auto') return _autoNode(sub);
    return sub.nodes.where((n) => n.fingerprint == c).firstOrNull;
  }

  bool get _canContinueCheck {
    final sub = _sub;
    return sub != null && !_testing && _resolve(sub) != null;
  }

  Future<void> _afterCheck() async {
    final sub = _sub;
    final node = sub == null ? null : _resolve(sub);
    if (sub == null || node == null) return;
    _picked = (sub: sub.id, fingerprint: node.fingerprint, name: node.name);
    setState(() => _busy = true);
    await s.selectNode(sub.id, node.fingerprint, node.name);
    if (!mounted) return;
    setState(() => _busy = false);
    _modeId ??= wizardDefaultMode(_modes, s)?.id;
    _go(_Step.mode);
  }

  // ---------------------------------------------------------------- mode

  WizardMode? get _mode => _modes.where((m) => m.id == _modeId).firstOrNull;

  Future<void> _afterMode() async {
    final mode = _mode;
    if (mode == null) return;
    if (!mode.isActive(s)) {
      setState(() {
        _busy = true;
        _error = null;
      });
      final err = await s.updateSettings(mode.apply);
      if (!mounted) return;
      setState(() => _busy = false);
      if (err != null) {
        setState(() => _error = '${WizardStrings.modeFailed}: ${redactForSupport(err)}');
        return;
      }
    }
    _go(_Step.done);
  }

  // ---------------------------------------------------------------- connecting

  void _connect() {
    final picked = _picked;
    final nav = Navigator.of(context);
    nav.pop(false);
    if (picked != null) s.connect(subscription: picked.sub, fingerprint: picked.fingerprint, name: picked.name);
  }

  // ---------------------------------------------------------------- build

  static const _steps = _Step.values;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final compact = isCompact(context);
    final primary = _primary;
    final body = ListenableBuilder(
      listenable: s,
      builder: (context, _) => switch (step) {
        _Step.welcome => _welcome(p),
        _Step.add => _addStep(p),
        _Step.check => _check(p),
        _Step.mode => _modeStep(p),
        _Step.done => _done(p),
      },
    );
    final header = Row(
      children: [
        for (final st in _steps)
          AnimatedContainer(
            duration: const Duration(milliseconds: 180),
            width: st == step ? 22 : 8,
            height: 8,
            margin: const EdgeInsets.only(right: 5),
            decoration: BoxDecoration(color: st.index <= step.index ? accent : p.border2, borderRadius: BorderRadius.circular(99)),
          ),
        const SizedBox(width: 6),
        Expanded(
          child: Text(WizardStrings.stepOf(step.index + 1, _steps.length), style: TextStyle(fontSize: 12, color: p.muted)),
        ),
        if (step != _Step.done) Btn(label: WizardStrings.skip, kind: BtnKind.ghost, small: true, onPressed: _skip),
      ],
    );
    final footer = Row(
      children: [
        if (step != _Step.welcome)
          Btn(
            label: compact ? null : WizardStrings.back,
            tooltip: compact ? WizardStrings.back : null,
            icon: Icons.arrow_back,
            onPressed: _busy ? null : _back,
          ),
        const Spacer(),
        if (step == _Step.done) ...[Btn(label: WizardStrings.doneLater, onPressed: _skip), const SizedBox(width: 8)],
        Btn(
          label: switch (step) {
            _Step.welcome => WizardStrings.welcomeStart,
            _Step.add => WizardStrings.addSubmit,
            _Step.done => WizardStrings.doneConnect,
            _ => WizardStrings.next,
          },
          icon: step == _Step.done ? Icons.power_settings_new : null,
          kind: BtnKind.primary,
          loading: _busy,
          onPressed: primary,
        ),
      ],
    );
    final content = Column(
      mainAxisSize: compact ? MainAxisSize.max : MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        header,
        const SizedBox(height: 18),
        compact ? Expanded(child: SingleChildScrollView(child: body)) : Flexible(child: SingleChildScrollView(child: body)),
        const SizedBox(height: 18),
        footer,
      ],
    );
    final shell = compact
        ? Scaffold(
            backgroundColor: p.bg,
            body: SafeArea(
              child: Padding(padding: const EdgeInsets.fromLTRB(20, 16, 20, 16), child: content),
            ),
          )
        : Dialog(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 520, maxHeight: 640),
              child: Padding(padding: const EdgeInsets.all(24), child: content),
            ),
          );
    return PopScope(
      // Android's back goes a step back, from the first one it skips.
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _back();
      },
      child: CallbackShortcuts(
        bindings: {
          const SingleActivator(LogicalKeyboardKey.escape): _skip,
          const SingleActivator(LogicalKeyboardKey.enter): _enterKey,
          const SingleActivator(LogicalKeyboardKey.numpadEnter): _enterKey,
        },
        child: Focus(autofocus: true, child: shell),
      ),
    );
  }

  void _enterKey() => _primary?.call();

  // ---------------------------------------------------------------- steps

  Widget _heading(Palette p, Widget leading, String title, {String? text}) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Row(
        children: [
          leading,
          const SizedBox(width: 12),
          Expanded(child: Text(title, style: dialogTitle)),
        ],
      ),
      if (text != null) ...[const SizedBox(height: 10), Text(text, style: TextStyle(fontSize: 13, color: p.muted, height: 1.45))],
    ],
  );

  Widget _iconBox(Palette p, IconData icon) => Container(
    width: 38,
    height: 38,
    decoration: BoxDecoration(color: accent.withValues(alpha: .14), borderRadius: BorderRadius.circular(10)),
    child: Icon(icon, size: 20, color: p.accentInk),
  );

  Widget _errorLine(String text) => Padding(
    padding: const EdgeInsets.only(top: 10),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Icon(Icons.error_outline, size: 15, color: errColor),
        const SizedBox(width: 6),
        Expanded(
          child: Text(text, style: const TextStyle(fontSize: 12.5, color: errColor, height: 1.4)),
        ),
      ],
    ),
  );

  Widget _welcome(Palette p) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      const SizedBox(height: 8),
      const CoreShiftMark(size: 52),
      const SizedBox(height: 18),
      Text(WizardStrings.welcomeTitle, style: display(24)),
      const SizedBox(height: 14),
      Text(WizardStrings.welcomeLine1, style: const TextStyle(height: 1.5)),
      const SizedBox(height: 10),
      Text(WizardStrings.welcomeLine2, style: TextStyle(color: p.muted, height: 1.5)),
    ],
  );

  /// What the link is, without the part that is the access token.
  String _linkLabel(ImportLink link) {
    if (link.url.isNotEmpty) return maskedUrl(link.url);
    final n = link.content.split('\n').where((l) => l.trim().isNotEmpty).length;
    return WizardStrings.addServersInText(n);
  }

  Widget _addStep(Palette p) {
    final link = _link;
    final compact = isCompact(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _heading(p, _iconBox(p, Icons.add_link), WizardStrings.addTitle, text: WizardStrings.addHint),
        const SizedBox(height: 16),
        if (link != null)
          Container(
            padding: const EdgeInsets.fromLTRB(12, 10, 6, 10),
            decoration: BoxDecoration(
              color: p.surface2,
              borderRadius: BorderRadius.circular(10),
              border: Border.all(color: p.border),
            ),
            child: Row(
              children: [
                Icon(link.url.isNotEmpty ? Icons.link : Icons.dns_outlined, size: 18, color: p.muted),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        link.url.isNotEmpty ? WizardStrings.addSubscriptionLabel : WizardStrings.addServersLabel,
                        style: TextStyle(fontSize: 11.5, color: p.muted),
                      ),
                      Text(
                        _linkLabel(link),
                        key: const ValueKey('wizard-link'),
                        style: const TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 12.5),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ],
                  ),
                ),
                Btn(label: WizardStrings.addClear, kind: BtnKind.ghost, small: true, onPressed: _busy ? null : _clearLink),
              ],
            ),
          )
        else
          TextField(
            controller: _field,
            key: const ValueKey('wizard-field'),
            autofocus: !compact,
            enabled: !_busy,
            minLines: 1,
            maxLines: 3,
            // Enter adds; a list of servers is pasted, not typed.
            keyboardType: TextInputType.url,
            textInputAction: TextInputAction.done,
            onSubmitted: (_) => _primary?.call(),
            style: const TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 12.5),
            decoration: const InputDecoration(hintText: WizardStrings.addFieldHint),
          ),
        if (_prefilled && link != null) ...[const SizedBox(height: 8), Text(WizardStrings.addFromClipboard, style: TextStyle(fontSize: 12, color: p.muted))],
        if (_busy) ...[
          const SizedBox(height: 12),
          Row(
            children: [
              const SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2)),
              const SizedBox(width: 10),
              Expanded(
                child: Text(WizardStrings.addProgress, style: TextStyle(fontSize: 12.5, color: p.muted)),
              ),
            ],
          ),
        ],
        if (_error != null) _errorLine(_error!),
        const SizedBox(height: 14),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            Btn(label: WizardStrings.addPaste, icon: Icons.content_paste, onPressed: _busy ? null : _paste),
            Btn(
              label: WizardStrings.addManual,
              icon: Icons.edit_outlined,
              kind: BtnKind.ghost,
              onPressed: _busy ? null : () => Navigator.of(context).pop(true),
            ),
          ],
        ),
      ],
    );
  }

  Widget _check(Palette p) {
    final sub = _sub;
    final running = _testing || !_tested;
    final children = <Widget>[
      _heading(p, _iconBox(p, Icons.speed), WizardStrings.checkTitle, text: running ? WizardStrings.checkHintBusy : WizardStrings.checkHintDone),
      const SizedBox(height: 16),
    ];
    if (sub == null) {
      children.add(_errorLine(WizardStrings.checkNoServers));
    } else if (running) {
      final total = s.latencyTotal, done = s.latencyDone;
      children.addAll([
        LinearProgressIndicator(value: total > 0 ? (done / total).clamp(0.0, 1.0) : null, color: accent, backgroundColor: p.surface3, minHeight: 4),
        const SizedBox(height: 10),
        Text(WizardStrings.checkProgress(done, total), style: TextStyle(fontSize: 12.5, color: p.muted)),
      ]);
    } else {
      final ranked = _ranked(sub);
      final choice = _effectiveChoice(sub);
      final answered = ranked.where((r) => r.$2?.ok == true).toList();
      if (ranked.isEmpty) {
        children.add(_errorLine(WizardStrings.checkNoServers));
      } else {
        if (_autoNode(sub) != null) {
          children.add(
            _Choice(
              key: const ValueKey('wizard-choice-auto'),
              selected: choice == 'auto',
              onTap: () => setState(() => _choice = 'auto'),
              title: WizardStrings.checkAuto,
              text: WizardStrings.checkAutoText,
              leading: Icon(Icons.auto_awesome, size: 20, color: p.accentInk),
            ),
          );
        }
        for (final (i, (n, l)) in answered.take(4).indexed) {
          children.add(
            _Choice(
              key: ValueKey('wizard-choice-${n.fingerprint}'),
              selected: choice == n.fingerprint,
              onTap: () => setState(() => _choice = n.fingerprint),
              title: cleanNodeName(n.name),
              leading: CountryBadge(countryOf(n.name, n.server), width: 30),
              badge: i == 0 ? WizardStrings.checkBest : null,
              trailing: Text(WizardStrings.checkMs(l!.ms), style: figures(13, color: p.okInk)),
            ),
          );
        }
        if (answered.isEmpty) {
          children.add(_errorLine(WizardStrings.checkNoneAnswered));
        } else if (answered.length > 4) {
          children.add(
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(WizardStrings.checkMore(sub.nodes.length - 4), style: TextStyle(fontSize: 12, color: p.muted)),
            ),
          );
        }
      }
      children.addAll([
        const SizedBox(height: 10),
        Align(
          alignment: Alignment.centerLeft,
          child: Btn(label: WizardStrings.checkAgain, icon: Icons.refresh, kind: BtnKind.ghost, small: true, onPressed: _runTest),
        ),
      ]);
    }
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: children);
  }

  Widget _modeStep(Palette p) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      _heading(p, _iconBox(p, Icons.alt_route), WizardStrings.modeTitle, text: WizardStrings.modeHint),
      const SizedBox(height: 16),
      for (final m in _modes)
        _Choice(
          key: ValueKey('wizard-mode-${m.id}'),
          selected: _modeId == m.id,
          enabled: m.available(s),
          onTap: () => setState(() => _modeId = m.id),
          title: m.title,
          text: m.available(s) ? m.text : (m.unavailable(s).isEmpty ? WizardStrings.modeUnavailable : m.unavailable(s)),
          leading: Icon(m.icon, size: 20, color: p.accentInk),
          badge: m.recommended ? WizardStrings.modeRecommended : null,
        ),
      if (_error != null) _errorLine(_error!),
    ],
  );

  Widget _done(Palette p) {
    final picked = _picked;
    final node = picked == null ? null : s.subscriptionById(picked.sub)?.nodes.where((n) => n.fingerprint == picked.fingerprint).firstOrNull;
    Widget row(String label, Widget value) => Padding(
      padding: const EdgeInsets.symmetric(vertical: 7),
      child: Row(
        children: [
          SizedBox(
            width: 80,
            child: Text(label, style: TextStyle(fontSize: 12.5, color: p.muted)),
          ),
          Expanded(child: value),
        ],
      ),
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _heading(p, _iconBox(p, Icons.check), WizardStrings.doneTitle, text: WizardStrings.doneHint),
        const SizedBox(height: 14),
        Panel(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
          color: p.surface2,
          child: Column(
            children: [
              row(
                WizardStrings.doneServer,
                Row(
                  children: [
                    if (node != null) ...[CountryBadge(countryOf(node.name, node.server), width: 26), const SizedBox(width: 8)],
                    Expanded(
                      child: Text(
                        node == null ? (picked?.name ?? '') : cleanNodeName(node.name),
                        style: const TextStyle(fontWeight: FontWeight.w600),
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                    if (_choice == 'auto' || (_choice == null && _sub != null && _effectiveChoice(_sub!) == 'auto'))
                      const Pill(WizardStrings.checkAuto, color: accent),
                  ],
                ),
              ),
              Divider(height: 1, color: p.border),
              row(WizardStrings.doneMode, Text(_mode?.title ?? '', style: const TextStyle(fontWeight: FontWeight.w600))),
            ],
          ),
        ),
      ],
    );
  }
}

/// A choice in a list: a card with a radio mark that lights when chosen.
class _Choice extends StatelessWidget {
  final bool selected;
  final bool enabled;
  final VoidCallback onTap;
  final String title;
  final String? text;
  final Widget? leading;
  final Widget? trailing;
  final String? badge;

  const _Choice({
    super.key,
    required this.selected,
    required this.onTap,
    required this.title,
    this.text,
    this.leading,
    this.trailing,
    this.badge,
    this.enabled = true,
  });

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Opacity(
        opacity: enabled ? 1 : .5,
        child: Semantics(
          button: true,
          selected: selected,
          enabled: enabled,
          child: Panel(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 11),
            borderColor: selected ? accent : null,
            color: selected ? accent.withValues(alpha: .06) : null,
            onTap: enabled ? onTap : null,
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.only(top: 1),
                  child: Icon(selected ? Icons.radio_button_checked : Icons.radio_button_unchecked, size: 18, color: selected ? p.accentInk : p.dim),
                ),
                const SizedBox(width: 10),
                if (leading != null) ...[leading!, const SizedBox(width: 10)],
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Wrap(
                        crossAxisAlignment: WrapCrossAlignment.center,
                        spacing: 8,
                        runSpacing: 4,
                        children: [
                          Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
                          if (badge != null) Pill(badge!, color: okColor),
                        ],
                      ),
                      if (text != null) ...[const SizedBox(height: 3), Text(text!, style: TextStyle(fontSize: 12.5, color: p.muted, height: 1.4))],
                    ],
                  ),
                ),
                if (trailing != null) ...[const SizedBox(width: 10), trailing!],
              ],
            ),
          ),
        ),
      ),
    );
  }
}
