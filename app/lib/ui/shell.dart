import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api/models.dart';
import '../platform/platform.dart' as platform;
import '../state/app_state.dart';
import 'pages/cores_page.dart';
import 'pages/home_page.dart';
import 'pages/logs_page.dart';
import 'pages/routing_page.dart';
import 'pages/servers_page.dart';
import 'pages/settings_page.dart';
import 'countries.dart';
import 'import_offer.dart';
import 'theme.dart';
import 'update_offer.dart';
import 'widgets.dart';

/// The pages. Five are stations on the main line: connecting (with the
/// speed test and the traffic), the servers, what goes through the VPN,
/// the journal and the settings (with the DNS leak test). The cores hang
/// off the settings.
enum PageId { home, servers, routing, logs, settings, cores }

/// The main page a page belongs to: the one lit in the navigation.
PageId stationOf(PageId p) => switch (p) {
  PageId.cores => PageId.settings,
  _ => p,
};

/// The main pages in the order the navigation shows them.
const mainPages = [
  (PageId.home, Icons.power_settings_new, 'Главная'),
  (PageId.servers, Icons.public, 'Серверы'),
  (PageId.routing, Icons.alt_route, 'Правила'),
  (PageId.logs, Icons.receipt_long_outlined, 'Журнал'),
  (PageId.settings, Icons.tune, 'Настройки'),
];

/// Lets pages switch to another page, e.g. the home page's node picker.
class Nav extends InheritedWidget {
  final ValueChanged<PageId> go;
  const Nav({super.key, required this.go, required super.child});

  static void to(BuildContext context, PageId id) => context.dependOnInheritedWidgetOfExactType<Nav>()!.go(id);

  @override
  bool updateShouldNotify(Nav old) => false;
}

class Shell extends StatefulWidget {
  final AppState state;
  final ThemeMode themeMode;
  final ValueChanged<ThemeMode> onThemeMode;

  const Shell({super.key, required this.state, required this.themeMode, required this.onThemeMode});

  @override
  State<Shell> createState() => _ShellState();
}

class _ShellState extends State<Shell> {
  PageId page = PageId.home;
  late final AppLifecycleListener _lifecycle;
  final _serverSearch = FocusNode();

  /// Ctrl+Enter: connect or disconnect, from any page.
  void _toggle() {
    final s = widget.state;
    if (s.online && (s.status.active || (s.selection.available && !s.busy))) s.toggleConnect();
  }

  /// Ctrl+F: the servers page, with its search focused.
  void _findServer() {
    if (!widget.state.loaded) return;
    setState(() => page = PageId.servers);
    WidgetsBinding.instance.addPostFrameCallback((_) => _serverSearch.requestFocus());
  }

  @override
  void initState() {
    super.initState();
    widget.state.addListener(_offerUpdate);
    widget.state.addListener(_offerImport);
    _offerUpdate();
    _offerImport();
    _lifecycle = AppLifecycleListener(
      onResume: () {
        widget.state.resumed();
        _lookAtClipboard();
      },
    );
  }

  @override
  void didUpdateWidget(Shell old) {
    super.didUpdateWidget(old);
    if (old.state == widget.state) return;
    old.state
      ..removeListener(_offerUpdate)
      ..removeListener(_offerImport);
    widget.state
      ..addListener(_offerUpdate)
      ..addListener(_offerImport);
    _wasLoaded = false;
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    _serverSearch.dispose();
    widget.state.removeListener(_offerUpdate);
    widget.state.removeListener(_offerImport);
    super.dispose();
  }

  bool _importShown = false;
  bool _wasLoaded = false;

  /// A subscription from a link, the clipboard or a QR code is offered in a
  /// window once the service answers; on the first load the clipboard is
  /// looked at too.
  void _offerImport() {
    final s = widget.state;
    if (s.loaded && s.online && !_wasLoaded) {
      _wasLoaded = true;
      _lookAtClipboard();
    }
    if (s.pendingImport == null || !s.loaded || !s.online || _importShown) return;
    _importShown = true;
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      if (mounted) await showImportOffer(context, s);
      // Closed some other way, such as Android's back: not added.
      if (s.pendingImport != null) s.dismissImport();
      _importShown = false;
    });
  }

  /// The clipboard, once the window is in front: Android lets an app read
  /// it only then, a moment after it comes back.
  void _lookAtClipboard() {
    Timer(const Duration(milliseconds: 400), () {
      if (!mounted || WidgetsBinding.instance.lifecycleState != AppLifecycleState.resumed) return;
      widget.state.checkClipboard();
    });
  }

  /// A downloaded update is offered in a window, wherever the user is.
  void _offerUpdate() {
    if (!widget.state.offerUpdate) return;
    widget.state.updateOffered();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) showUpdateOffer(context, widget.state);
    });
  }

  @override
  Widget build(BuildContext context) {
    final s = widget.state;
    return ListenableBuilder(
      listenable: s,
      builder: (context, _) {
        final body = !s.loaded
            ? _Offline(state: s)
            : switch (page) {
                PageId.home => HomePage(state: s),
                PageId.servers => ServersPage(state: s, searchFocus: _serverSearch),
                PageId.cores => CoresPage(state: s),
                PageId.routing => RoutingPage(state: s),
                PageId.logs => LogsPage(state: s),
                PageId.settings => SettingsPage(state: s, themeMode: widget.themeMode, onThemeMode: widget.onThemeMode),
              };
        final content = Column(
          children: [
            if (s.loaded && !s.online) _OfflineBanner(reason: s.offlineReason),
            // The home page says so itself. Elsewhere, e.g. right after
            // switching a rule, nothing would tell that it is not in effect yet.
            if (s.loaded && s.online && s.status.settingsPending && page != PageId.home) _PendingStrip(state: s),
            Expanded(
              child: KeyedSubtree(key: ValueKey(page), child: body),
            ),
          ],
        );
        if (isCompact(context)) {
          return Nav(
            go: (p) => setState(() => page = p),
            child: Scaffold(
              body: SafeArea(
                bottom: false,
                child: Stack(
                  children: [
                    content,
                    Positioned(left: 12, right: 12, bottom: 12, child: _Toasts(state: s)),
                  ],
                ),
              ),
              bottomNavigationBar: _BottomNav(state: s, page: page, onPage: (p) => setState(() => page = p)),
            ),
          );
        }
        return Nav(
          go: (p) => setState(() => page = p),
          child: CallbackShortcuts(
            bindings: {
              const SingleActivator(LogicalKeyboardKey.enter, control: true): _toggle,
              const SingleActivator(LogicalKeyboardKey.keyF, control: true): _findServer,
            },
            child: Focus(
              autofocus: true,
              child: Scaffold(
                body: Stack(
                  children: [
                    Row(
                      children: [
                        _Sidebar(state: s, page: page, onPage: (p) => setState(() => page = p)),
                        Expanded(child: content),
                      ],
                    ),
                    Positioned(right: 20, bottom: 20, child: _Toasts(state: s)),
                  ],
                ),
              ),
            ),
          ),
        );
      },
    );
  }
}

/// The phone's navigation: the five main pages. On the cores the settings
/// stay lit.
class _BottomNav extends StatelessWidget {
  final AppState state;
  final PageId page;
  final ValueChanged<PageId> onPage;
  const _BottomNav({required this.state, required this.page, required this.onPage});

  @override
  Widget build(BuildContext context) {
    final index = mainPages.indexWhere((m) => m.$1 == stationOf(page));
    return NavigationBar(
      height: 68,
      labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
      selectedIndex: index,
      onDestinationSelected: (i) => onPage(mainPages[i].$1),
      destinations: [for (final (_, icon, label) in mainPages) NavigationDestination(icon: Icon(icon), label: label)],
    );
  }
}

class _Sidebar extends StatelessWidget {
  final AppState state;
  final PageId page;
  final ValueChanged<PageId> onPage;
  const _Sidebar({required this.state, required this.page, required this.onPage});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      width: 228,
      decoration: BoxDecoration(
        color: p.bg2,
        border: Border(right: BorderSide(color: p.border)),
      ),
      padding: const EdgeInsets.fromLTRB(12, 18, 12, 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(6, 0, 6, 18),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                const CoreShiftMark(size: 26),
                const SizedBox(width: 10),
                Flexible(
                  child: Text('CoreShift', style: display(19, spacing: -.2), overflow: TextOverflow.ellipsis),
                ),
                const SizedBox(width: 6),
                Tooltip(
                  message: state.versionMismatch
                      ? state.versionMismatchAdvice
                      : 'Версия ${state.version.label}${state.version.commit.isEmpty ? '' : ', коммит ${state.version.commit}'}',
                  child: Text(
                    state.version.known ? state.version.version : state.info.version,
                    style: TextStyle(color: state.versionMismatch ? p.warnInk : p.dim, fontSize: 11, fontWeight: FontWeight.w500),
                  ),
                ),
              ],
            ),
          ),
          _StatusPill(state: state),
          const SizedBox(height: 18),
          // The pages as stations on one line.
          for (final (i, (id, icon, label)) in mainPages.indexed)
            _NavItem(icon: icon, label: label, active: stationOf(page) == id, first: i == 0, last: i == mainPages.length - 1, onTap: () => onPage(id)),
          const Spacer(),
        ],
      ),
    );
  }
}

class _StatusPill extends StatelessWidget {
  final AppState state;
  const _StatusPill({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status.state;
    final (color, text) = !state.online
        ? (errColor, 'Нет связи со службой')
        : switch (st) {
            ConnState.connected => (okColor, 'Подключено'),
            ConnState.connecting => (warnColor, 'Подключение…'),
            ConnState.disconnecting => (warnColor, 'Отключение…'),
            ConnState.failed => (errColor, 'Ошибка'),
            ConnState.idle => (p.dim, 'Отключено'),
          };
    final active = state.status.active;
    final canToggle = state.online && (active || (state.selection.available && !state.busy));
    final server = cleanNodeName(active ? state.status.node : state.selection.name);
    final on = st == ConnState.connected && state.online;
    return Container(
      padding: const EdgeInsets.fromLTRB(12, 8, 4, 8),
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: on ? okColor.withValues(alpha: .45) : p.border),
      ),
      child: Row(
        children: [
          Lamp(color: color, lit: on || st == ConnState.connecting, size: 9),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  text,
                  style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: on ? p.okInk : p.text),
                  overflow: TextOverflow.ellipsis,
                ),
                if (state.online && server.isNotEmpty)
                  Text(
                    server,
                    style: TextStyle(fontSize: 11.5, color: p.muted),
                    overflow: TextOverflow.ellipsis,
                  ),
              ],
            ),
          ),
          IconButton(
            tooltip: active ? 'Отключить (Ctrl+Enter)' : 'Подключить (Ctrl+Enter)',
            onPressed: canToggle ? state.toggleConnect : null,
            icon: Icon(Icons.power_settings_new, size: 18, color: active ? okColor : p.accentInk),
            visualDensity: VisualDensity.compact,
          ),
        ],
      ),
    );
  }
}

class _NavItem extends StatefulWidget {
  final IconData icon;
  final String label;
  final bool active;
  final bool first;
  final bool last;
  final VoidCallback onTap;
  const _NavItem({required this.icon, required this.label, required this.active, required this.onTap, this.first = false, this.last = false});

  @override
  State<_NavItem> createState() => _NavItemState();
}

class _NavItemState extends State<_NavItem> {
  bool hover = false;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final fg = widget.active || hover ? p.text : p.muted;
    return MouseRegion(
      cursor: SystemMouseCursors.click,
      onEnter: (_) => setState(() => hover = true),
      onExit: (_) => setState(() => hover = false),
      child: GestureDetector(
        onTap: widget.onTap,
        behavior: HitTestBehavior.opaque,
        child: CustomPaint(
          painter: _StationPainter(active: widget.active, first: widget.first, last: widget.last, line: p.border2, hollow: p.bg2, hover: hover),
          child: Padding(
            padding: const EdgeInsets.only(left: 30),
            child: Container(
              margin: const EdgeInsets.symmetric(vertical: 2),
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
              decoration: BoxDecoration(
                color: widget.active ? p.surface : (hover ? p.surface.withValues(alpha: .6) : Colors.transparent),
                borderRadius: BorderRadius.circular(8),
                border: Border.all(color: widget.active ? p.border : Colors.transparent),
              ),
              child: Row(
                children: [
                  Icon(widget.icon, size: 17, color: widget.active ? p.accentInk : fg),
                  const SizedBox(width: 11),
                  Expanded(
                    child: Text(
                      widget.label,
                      style: TextStyle(color: fg, fontWeight: widget.active ? FontWeight.w600 : FontWeight.w500),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// A page's station on the sidebar's line: a hollow ring, the current page
/// a lit amber one.
class _StationPainter extends CustomPainter {
  final bool active, first, last, hover;
  final Color line, hollow;
  _StationPainter({required this.active, required this.first, required this.last, required this.line, required this.hollow, required this.hover});

  @override
  void paint(Canvas canvas, Size size) {
    const x = 12.0;
    final y = size.height / 2;
    final track = Paint()
      ..color = line
      ..strokeWidth = 2;
    canvas.drawLine(Offset(x, first ? y : 0), Offset(x, last ? y : size.height), track);
    if (active) {
      canvas.drawCircle(Offset(x, y), 10, Paint()..color = accent.withValues(alpha: .18));
      canvas.drawCircle(Offset(x, y), 6, Paint()..color = accent);
      return;
    }
    canvas.drawCircle(Offset(x, y), 5, Paint()..color = hollow);
    canvas.drawCircle(
      Offset(x, y),
      5,
      Paint()
        ..color = hover ? accent : line
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2,
    );
  }

  @override
  bool shouldRepaint(_StationPainter old) => old.active != active || old.hover != hover || old.line != line || old.hollow != hollow;
}

class _Offline extends StatefulWidget {
  final AppState state;
  const _Offline({required this.state});

  @override
  State<_Offline> createState() => _OfflineState();
}

class _OfflineState extends State<_Offline> {
  bool starting = false;
  String? error;

  Future<void> _start() async {
    setState(() {
      starting = true;
      error = null;
    });
    final err = await platform.startService();
    if (!mounted) return;
    // On success the window connects by itself once the service is up.
    setState(() {
      starting = false;
      error = err;
    });
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final state = widget.state;
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 520),
        child: Panel(
          padding: const EdgeInsets.all(26),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: p.muted)),
                  const SizedBox(width: 12),
                  Flexible(
                    child: Text(
                      platform.isAndroid
                          ? 'Запуск CoreShift…'
                          : state.daemonStarting
                          ? 'Запуск службы…'
                          : 'Подключение к службе…',
                      style: dialogTitle,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              Text(
                state.offlineReason.isEmpty ? (platform.isAndroid ? 'Запускаем движок' : 'Ищем службу CoreShift') : state.offlineReason,
                style: TextStyle(color: p.muted),
              ),
              const SizedBox(height: 14),
              Text(
                platform.isAndroid
                    ? 'Движок VPN работает внутри приложения и обычно запускается за секунду. '
                          'Если этот экран не пропадает, закройте CoreShift в списке недавних приложений и откройте снова.'
                    : platform.isLinux
                    ? 'VPN работает через системную службу CoreShift. Она запускается вместе с компьютером и держит VPN, '
                          'пока его не выключат кнопкой, даже если окно закрыто.'
                    : state.daemonStartRefused
                    ? 'VPN работает через фоновую службу CoreShift. Windows не дал запустить её без прав администратора: '
                          'так бывает со службой, установленной версией до 0.4. Запустите её кнопкой ниже или переустановите CoreShift.'
                    : 'VPN работает через фоновую службу CoreShift. Она запускается вместе с приложением '
                          'и останавливается, когда его закрывают. Обычно это занимает пару секунд.',
                style: TextStyle(color: p.muted, fontSize: 13, height: 1.5),
              ),
              // Without access to the service, starting it would not help.
              if (platform.canStartService && !state.daemonStarting && !platform.daemonAccessDenied(state.offlineReason)) ...[
                const SizedBox(height: 16),
                Row(
                  children: [
                    Btn(label: 'Запустить службу', icon: Icons.play_arrow, kind: BtnKind.primary, loading: starting, onPressed: _start),
                    const SizedBox(width: 10),
                    Flexible(
                      child: Text(
                        platform.isLinux ? 'Система спросит пароль администратора' : 'Windows попросит права администратора',
                        style: TextStyle(color: p.dim, fontSize: 12),
                      ),
                    ),
                  ],
                ),
                if (error != null) ...[const SizedBox(height: 8), Text(error!, style: const TextStyle(color: errColor, fontSize: 12))],
              ],
              if (!platform.isAndroid) ...[
                const SizedBox(height: 16),
                Text(
                  'Адрес службы берётся из ${state.backend.description}',
                  style: TextStyle(color: p.dim, fontSize: 11, fontFamily: monoFont, fontFamilyFallback: monoFallback),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _OfflineBanner extends StatefulWidget {
  final String reason;
  const _OfflineBanner({required this.reason});

  @override
  State<_OfflineBanner> createState() => _OfflineBannerState();
}

class _OfflineBannerState extends State<_OfflineBanner> {
  bool starting = false;
  String? error;

  /// Linux: the service stopped while the window was open. Nothing starts
  /// it again by itself (on Windows the app does), so offer what the
  /// start screen offers.
  bool get _canStart => platform.isLinux && platform.canStartService && !platform.daemonAccessDenied(widget.reason);

  Future<void> _start() async {
    setState(() {
      starting = true;
      error = null;
    });
    final err = await platform.startService();
    if (!mounted) return;
    setState(() {
      starting = false;
      error = err;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 9),
      color: errColor.withValues(alpha: .12),
      child: Row(
        children: [
          const Icon(Icons.link_off, size: 16, color: errColor),
          const SizedBox(width: 10),
          Expanded(child: Text(error == null ? '${widget.reason}. Переподключаемся…' : '${widget.reason}. $error', style: const TextStyle(fontSize: 13))),
          if (_canStart) ...[
            const SizedBox(width: 10),
            Btn(label: 'Запустить службу', icon: Icons.play_arrow, small: true, loading: starting, onPressed: _start),
          ],
        ],
      ),
    );
  }
}

class _PendingStrip extends StatelessWidget {
  final AppState state;
  const _PendingStrip({required this.state});

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(20, 6, 12, 6),
      color: warnColor.withValues(alpha: .12),
      child: Row(
        children: [
          const Icon(Icons.info_outline, size: 16, color: warnColor),
          const SizedBox(width: 10),
          const Expanded(child: Text('Изменения применятся после переподключения', style: TextStyle(fontSize: 13))),
          Btn(label: 'Применить', small: true, onPressed: state.busy ? null : state.reconnect),
        ],
      ),
    );
  }
}

class _Toasts extends StatelessWidget {
  final AppState state;
  const _Toasts({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.end,
      mainAxisSize: MainAxisSize.min,
      children: [
        for (final t in state.toasts)
          Padding(
            key: ValueKey(t.id),
            padding: const EdgeInsets.only(top: 8),
            child: TweenAnimationBuilder<double>(
              tween: Tween(begin: 0, end: 1),
              duration: const Duration(milliseconds: 220),
              builder: (context, v, child) => Opacity(
                opacity: v,
                child: Transform.translate(offset: Offset(0, 8 * (1 - v)), child: child),
              ),
              child: Material(
                color: Colors.transparent,
                child: Container(
                  constraints: const BoxConstraints(maxWidth: 400),
                  padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
                  decoration: BoxDecoration(
                    color: p.surface2,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(
                      color: switch (t.kind) {
                        ToastKind.swap => swapColor.withValues(alpha: .5),
                        ToastKind.err => errColor.withValues(alpha: .5),
                        ToastKind.ok => okColor.withValues(alpha: .45),
                        ToastKind.info => p.border2,
                      },
                    ),
                    boxShadow: [BoxShadow(color: Colors.black.withValues(alpha: .4), blurRadius: 40, offset: const Offset(0, 14), spreadRadius: -12)],
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(
                        switch (t.kind) {
                          ToastKind.swap => Icons.swap_horiz,
                          ToastKind.err => Icons.error_outline,
                          ToastKind.ok => Icons.check_circle_outline,
                          ToastKind.info => Icons.info_outline,
                        },
                        size: 17,
                        color: switch (t.kind) {
                          ToastKind.swap => swapColor,
                          ToastKind.err => errColor,
                          ToastKind.ok => okColor,
                          ToastKind.info => p.muted,
                        },
                      ),
                      const SizedBox(width: 10),
                      Flexible(child: Text(t.message, style: const TextStyle(fontSize: 13))),
                      const SizedBox(width: 6),
                      InkWell(
                        onTap: () => state.dismissToast(t),
                        child: Icon(Icons.close, size: 14, color: p.dim),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
      ],
    );
  }
}
