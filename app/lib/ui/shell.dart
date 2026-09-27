import 'package:flutter/material.dart';

import '../api/models.dart';
import '../platform/platform.dart' as platform;
import '../state/app_state.dart';
import 'pages/cores_page.dart';
import 'pages/home_page.dart';
import 'pages/logs_page.dart';
import 'pages/routing_page.dart';
import 'pages/servers_page.dart';
import 'pages/settings_page.dart';
import 'theme.dart';
import 'update_offer.dart';
import 'widgets.dart';

enum PageId { home, servers, cores, routing, logs, settings }

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

  @override
  void initState() {
    super.initState();
    widget.state.addListener(_offerUpdate);
    _offerUpdate();
  }

  @override
  void dispose() {
    widget.state.removeListener(_offerUpdate);
    super.dispose();
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
                PageId.servers => ServersPage(state: s),
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
        );
      },
    );
  }
}

/// The phone's navigation: the four main pages, and the pages for advanced
/// users behind "Ещё".
class _BottomNav extends StatelessWidget {
  final AppState state;
  final PageId page;
  final ValueChanged<PageId> onPage;
  const _BottomNav({required this.state, required this.page, required this.onPage});

  static const _main = [PageId.home, PageId.servers, PageId.routing, PageId.settings];

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final index = _main.indexOf(page);
    return NavigationBar(
      height: 64,
      backgroundColor: p.bg2,
      indicatorColor: accent.withValues(alpha: .18),
      labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
      selectedIndex: index < 0 ? _main.length : index,
      onDestinationSelected: (i) async {
        if (i < _main.length) return onPage(_main[i]);
        final more = await showModalBottomSheet<PageId>(
          context: context,
          backgroundColor: p.bg2,
          builder: (context) => SafeArea(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Padding(padding: EdgeInsets.fromLTRB(20, 16, 20, 4), child: SectionLabel('Для опытных')),
                ListTile(leading: const Icon(Icons.memory), title: const Text('Ядра'), onTap: () => Navigator.pop(context, PageId.cores)),
                ListTile(leading: const Icon(Icons.notes), title: const Text('Журнал'), onTap: () => Navigator.pop(context, PageId.logs)),
                const SizedBox(height: 8),
              ],
            ),
          ),
        );
        if (more != null) onPage(more);
      },
      destinations: [
        const NavigationDestination(icon: Icon(Icons.home_outlined), label: 'Главная'),
        NavigationDestination(
          icon: Badge(isLabelVisible: state.nodeCount > 0, label: Text('${state.nodeCount}'), child: const Icon(Icons.public)),
          label: 'Серверы',
        ),
        const NavigationDestination(icon: Icon(Icons.alt_route), label: 'Правила'),
        const NavigationDestination(icon: Icon(Icons.settings_outlined), label: 'Настройки'),
        const NavigationDestination(icon: Icon(Icons.more_horiz), label: 'Ещё'),
      ],
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
    final nodes = state.nodeCount;
    final main = [
      (PageId.home, Icons.home_outlined, 'Главная', null),
      (PageId.servers, Icons.public, 'Серверы', nodes > 0 ? '$nodes' : null),
      (PageId.routing, Icons.alt_route, 'Правила', null),
      (PageId.settings, Icons.settings_outlined, 'Настройки', null),
    ];
    final advanced = [(PageId.cores, Icons.memory, 'Ядра', null), (PageId.logs, Icons.notes, 'Журнал', null)];
    return Container(
      width: 216,
      decoration: BoxDecoration(
        color: p.bg2,
        border: Border(right: BorderSide(color: p.border)),
      ),
      padding: const EdgeInsets.fromLTRB(10, 16, 10, 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 0, 8, 16),
            child: Row(
              children: [
                const FlutterLogo(size: 22),
                const SizedBox(width: 9),
                const Flexible(
                  child: Text(
                    'CoreShift',
                    style: TextStyle(fontWeight: FontWeight.w700, fontSize: 15, letterSpacing: .2),
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                const SizedBox(width: 5),
                Tooltip(
                  message: state.versionMismatch
                      ? 'Приложение ${state.version.label}, служба ${state.info.buildVersion.label}: версии различаются, переустановите CoreShift'
                      : 'Версия ${state.version.label}${state.version.commit.isEmpty ? '' : ', коммит ${state.version.commit}'}',
                  child: Text(
                    state.version.known ? state.version.version : state.info.version,
                    style: TextStyle(color: state.versionMismatch ? warnColor : p.dim, fontSize: 11, fontWeight: FontWeight.w500),
                  ),
                ),
              ],
            ),
          ),
          _StatusPill(state: state),
          const SizedBox(height: 14),
          for (final (id, icon, label, count) in main) _NavItem(icon: icon, label: label, count: count, active: page == id, onTap: () => onPage(id)),
          const Padding(padding: EdgeInsets.fromLTRB(12, 16, 12, 0), child: SectionLabel('Для опытных')),
          for (final (id, icon, label, count) in advanced) _NavItem(icon: icon, label: label, count: count, active: page == id, onTap: () => onPage(id)),
          const Spacer(),
          _CoreFooter(state: state),
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
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 2),
      padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 6),
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(99),
        border: Border.all(color: p.border),
      ),
      child: Row(
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(
              color: color,
              shape: BoxShape.circle,
              boxShadow: st == ConnState.connected && state.online ? [BoxShadow(color: okColor.withValues(alpha: .3), spreadRadius: 3)] : null,
            ),
          ),
          const SizedBox(width: 8),
          Flexible(
            child: Text(
              text,
              style: TextStyle(fontSize: 12, color: p.muted),
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
    );
  }
}

class _NavItem extends StatefulWidget {
  final IconData icon;
  final String label;
  final String? count;
  final bool active;
  final VoidCallback onTap;
  const _NavItem({required this.icon, required this.label, this.count, required this.active, required this.onTap});

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
        child: Stack(
          clipBehavior: Clip.none,
          children: [
            Container(
              margin: const EdgeInsets.only(bottom: 2),
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
              decoration: BoxDecoration(color: widget.active ? p.surface2 : (hover ? p.surface : Colors.transparent), borderRadius: BorderRadius.circular(10)),
              child: Row(
                children: [
                  Icon(widget.icon, size: 18, color: fg),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      widget.label,
                      style: TextStyle(color: fg, fontWeight: FontWeight.w500),
                    ),
                  ),
                  if (widget.count != null)
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 1),
                      decoration: BoxDecoration(color: p.surface, borderRadius: BorderRadius.circular(99)),
                      child: Text(widget.count!, style: TextStyle(fontSize: 11, color: p.dim)),
                    ),
                ],
              ),
            ),
            if (widget.active)
              Positioned(
                left: -10,
                top: 9,
                bottom: 11,
                child: Container(
                  width: 3,
                  decoration: const BoxDecoration(
                    color: accent,
                    borderRadius: BorderRadius.horizontal(right: Radius.circular(3)),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _CoreFooter extends StatelessWidget {
  final AppState state;
  const _CoreFooter({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status;
    final core = st.active ? st.core : '';
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: p.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: p.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const SectionLabel('Ядро'),
          Row(
            children: [
              if (core.isNotEmpty) ...[CoreLogo(core, size: 22), const SizedBox(width: 8)],
              Expanded(
                child: Text(
                  core.isNotEmpty ? coreStyle(core).name : (st.active ? 'запускается…' : 'не запущено'),
                  style: TextStyle(fontSize: 12, color: core.isNotEmpty ? p.text : p.muted, fontWeight: FontWeight.w500),
                ),
              ),
              if (core.isNotEmpty && state.setting('cores.mode', 'auto') == 'auto') const Pill('АВТОСВАП', color: swapColor),
            ],
          ),
        ],
      ),
    );
  }
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
                      platform.isAndroid ? 'Запуск CoreShift…' : 'Подключение к службе…',
                      style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600),
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
                    : 'VPN работает через фоновую службу CoreShift. Обычно она запускается вместе с Windows. '
                          'Если служба остановлена, запустите её — окно подключится само.',
                style: TextStyle(color: p.muted, fontSize: 13, height: 1.5),
              ),
              if (platform.canStartService) ...[
                const SizedBox(height: 16),
                Row(
                  children: [
                    Btn(label: 'Запустить службу', icon: Icons.play_arrow, kind: BtnKind.primary, loading: starting, onPressed: _start),
                    const SizedBox(width: 10),
                    Flexible(
                      child: Text('Windows попросит права администратора', style: TextStyle(color: p.dim, fontSize: 12)),
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

class _OfflineBanner extends StatelessWidget {
  final String reason;
  const _OfflineBanner({required this.reason});

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
          Expanded(child: Text('$reason. Переподключаемся…', style: const TextStyle(fontSize: 13))),
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
                    borderRadius: BorderRadius.circular(12),
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
