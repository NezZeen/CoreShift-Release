import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api/models.dart';
import '../platform/api_access.dart' show windowsGroup;
import '../platform/platform.dart' as platform;
import '../state/app_state.dart';
import 'pages/cores_page.dart';
import 'pages/home_page.dart';
import 'pages/logs_page.dart';
import 'pages/routing_page.dart';
import 'pages/servers_page.dart';
import 'pages/settings_page.dart';
import 'countries.dart';
import 'disclaimer.dart';
import 'import_offer.dart';
import 'theme.dart';
import 'update_offer.dart';
import 'widgets.dart';

part 'shell/navigation.dart';
part 'shell/offline.dart';
part 'shell/notices.dart';

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
    // The disclaimer first of all, until it is accepted.
    if (widget.state.askDisclaimer) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) offerDisclaimer(context, widget.state);
      });
    }
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

  /// The disclaimer waits for an answer: no other window comes over it.
  /// Accepted, the pref it sets brings the waiting offers.
  bool get _awaitingDisclaimer => widget.state.askDisclaimer && widget.state.prefs[disclaimerPref] != true;

  /// A subscription from a link, the clipboard or a QR code is offered in a
  /// window once the service answers; on the first load the clipboard is
  /// looked at too.
  void _offerImport() {
    final s = widget.state;
    if (_awaitingDisclaimer) return;
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
    if (!widget.state.offerUpdate || _awaitingDisclaimer) return;
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
