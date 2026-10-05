import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:tray_manager/tray_manager.dart' as tray;
import 'package:window_manager/window_manager.dart';

import '../api/models.dart';
import '../state/app_state.dart';
import '../ui/countries.dart' show cleanNodeName;
import '../ui/theme.dart';
import '../ui/widgets.dart' show formatRate;
import 'linux_desktop.dart' as linux;
import 'tray_balloon.dart';

bool _enabled = false;

/// The session shows no tray icons (Linux): closing the window minimizes it.
bool _noTray = false;

/// Whether the system can show CoreShift's notifications.
bool get canNotify => Platform.isWindows || Platform.isLinux;

/// Takes over the window: CoreShift draws its own title bar, and closing the
/// window hides it to the tray. [hidden] starts in the tray, as after a
/// self-update. Returns false where there is no such window, and then
/// [DesktopFrame] adds nothing.
Future<bool> initWindow({bool hidden = false}) async {
  if (!Platform.isWindows && !Platform.isLinux) return false;
  // A Linux session may have no tray (plain GNOME, WSLg): then the window
  // never hides, or nothing could bring it back.
  if (Platform.isLinux) _noTray = !await linux.trayAvailable();
  if (_noTray) hidden = false;
  await windowManager.ensureInitialized();
  await windowManager.waitUntilReadyToShow(const WindowOptions(title: 'CoreShift', titleBarStyle: TitleBarStyle.hidden, minimumSize: Size(960, 640)), () async {
    if (hidden) return;
    await windowManager.show();
    await windowManager.focus();
  });
  await windowManager.setPreventClose(true);
  return _enabled = true;
}

/// The title bar above the app and the tray icon beside the clock.
class DesktopFrame extends StatefulWidget {
  final AppState state;
  final Widget child;
  const DesktopFrame({super.key, required this.state, required this.child});

  @override
  State<DesktopFrame> createState() => _DesktopFrameState();
}

class _DesktopFrameState extends State<DesktopFrame> with WindowListener {
  _Tray? _tray;
  bool _maximized = false;
  StreamSubscription<Alert>? _alerts;

  @override
  void initState() {
    super.initState();
    if (!_enabled) return;
    windowManager.addListener(this);
    windowManager.isMaximized().then((v) => mounted ? setState(() => _maximized = v) : null);
    _tray = _Tray.create(widget.state, onOpen: _show, onExit: _exit);
    _alerts = widget.state.alerts.listen(_notify);
  }

  @override
  void dispose() {
    if (_enabled) windowManager.removeListener(this);
    _alerts?.cancel();
    _tray?.dispose();
    super.dispose();
  }

  /// A system notification, when the user would not see the in-app toast.
  Future<void> _notify(Alert a) async {
    if (!widget.state.systemNotifications) return;
    try {
      if (await windowManager.isVisible() && !await windowManager.isMinimized() && await windowManager.isFocused()) return;
    } catch (_) {
      // Unsure: notify rather than miss it.
    }
    if (Platform.isWindows) {
      final id = _tray?.icon.getId();
      if (id != null) showTrayBalloon(id, a.title, a.body);
    } else if (Platform.isLinux) {
      Process.run('notify-send', ['-a', 'CoreShift', a.title, a.body]).ignore();
    }
  }

  Future<void> _show() async {
    if (await windowManager.isMinimized()) await windowManager.restore();
    await windowManager.show();
    await windowManager.focus();
  }

  /// Quits the app, and with it the VPN: the service stops once the app is
  /// gone, however it ends; here it disconnects at once rather than after
  /// its grace period.
  Future<void> _exit() async {
    await windowManager.hide();
    if (widget.state.status.active) {
      try {
        await widget.state.disconnect().timeout(const Duration(seconds: 5));
      } catch (_) {
        // The service disconnects by itself once the app is gone.
      }
    }
    try {
      _tray?.dispose();
    } catch (_) {
      // The icon goes with the process anyway.
    }
    _tray = null;
    // windowManager.destroy() only posts WM_QUIT, which a menu or other modal
    // loop can swallow, leaving a live process behind; exit for certain.
    exit(0);
  }

  @override
  void onWindowClose() {
    if (_noTray) {
      // No tray to come back from: the window stays on the taskbar, and
      // the VPN runs on regardless.
      windowManager.minimize();
      return;
    }
    windowManager.hide();
    // Once: a window that just vanishes reads as CoreShift having quit,
    // while it runs on in the tray, the VPN with it.
    final state = widget.state;
    if (state.prefs['tray_hint_shown'] == true) return;
    state.setPref('tray_hint_shown', true);
    if (Platform.isLinux) {
      Process.run('notify-send', [
        '-a',
        'CoreShift',
        'CoreShift работает в трее',
        'Окно свёрнуто, VPN работает как прежде. Чтобы выйти совсем, откройте меню значка CoreShift в трее → «Выход».',
      ]).ignore();
    } else if (Platform.isWindows) {
      final id = _tray?.icon.getId();
      if (id != null) {
        showTrayBalloon(
          id,
          'CoreShift работает в трее',
          'Окно свёрнуто, VPN работает как прежде. Чтобы выйти совсем, нажмите на значок правой кнопкой → «Выход».',
        );
      }
    }
  }

  @override
  void onWindowMaximize() => setState(() => _maximized = true);

  @override
  void onWindowUnmaximize() => setState(() => _maximized = false);

  @override
  Widget build(BuildContext context) {
    if (!_enabled) return widget.child;
    return Column(
      children: [
        _TitleBar(maximized: _maximized),
        Expanded(child: widget.child),
      ],
    );
  }
}

class _TitleBar extends StatelessWidget {
  final bool maximized;
  const _TitleBar({required this.maximized});

  static const height = 34.0;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Material(
      color: p.bg2,
      child: Container(
        height: height,
        decoration: BoxDecoration(
          border: Border(bottom: BorderSide(color: p.border)),
        ),
        child: Row(
          children: [
            Expanded(
              child: DragToMoveArea(
                child: Container(
                  alignment: Alignment.centerLeft,
                  padding: const EdgeInsets.only(left: 14),
                  child: Text(
                    'CoreShift',
                    style: TextStyle(fontSize: 12, color: p.dim, fontWeight: FontWeight.w500),
                  ),
                ),
              ),
            ),
            _CaptionButton(glyph: _Glyph.minimize, onTap: windowManager.minimize),
            _CaptionButton(glyph: maximized ? _Glyph.restore : _Glyph.maximize, onTap: () => maximized ? windowManager.unmaximize() : windowManager.maximize()),
            _CaptionButton(glyph: _Glyph.close, onTap: windowManager.close, danger: true),
          ],
        ),
      ),
    );
  }
}

enum _Glyph { minimize, maximize, restore, close }

class _CaptionButton extends StatefulWidget {
  final _Glyph glyph;
  final VoidCallback onTap;
  final bool danger;
  const _CaptionButton({required this.glyph, required this.onTap, this.danger = false});

  @override
  State<_CaptionButton> createState() => _CaptionButtonState();
}

class _CaptionButtonState extends State<_CaptionButton> {
  bool hover = false;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final bg = !hover ? Colors.transparent : (widget.danger ? const Color(0xFFE81123) : p.surface2);
    final fg = hover && widget.danger ? Colors.white : p.muted;
    return MouseRegion(
      onEnter: (_) => setState(() => hover = true),
      onExit: (_) => setState(() => hover = false),
      child: GestureDetector(
        onTap: () {
          // The window may hide under the pointer (close to the tray), and
          // then no exit comes: it would show again with the button lit.
          setState(() => hover = false);
          widget.onTap();
        },
        child: Container(
          width: 46,
          height: _TitleBar.height,
          color: bg,
          child: CustomPaint(painter: _GlyphPainter(widget.glyph, fg)),
        ),
      ),
    );
  }
}

/// Draws the caption glyphs the way Windows does: thin 10 px outlines.
class _GlyphPainter extends CustomPainter {
  final _Glyph glyph;
  final Color color;
  _GlyphPainter(this.glyph, this.color);

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..strokeWidth = 1
      ..style = PaintingStyle.stroke;
    final c = size.center(Offset.zero);
    const h = 5.0;
    switch (glyph) {
      case _Glyph.minimize:
        canvas.drawLine(c + const Offset(-h, 0), c + const Offset(h, 0), paint);
      case _Glyph.maximize:
        canvas.drawRect(Rect.fromCenter(center: c, width: 2 * h, height: 2 * h), paint);
      case _Glyph.restore:
        canvas.drawRect(Rect.fromLTWH(c.dx - h, c.dy - h + 2, 2 * h - 2, 2 * h - 2), paint);
        canvas.drawPath(
          Path()
            ..moveTo(c.dx - h + 2, c.dy - h + 2)
            ..lineTo(c.dx - h + 2, c.dy - h)
            ..lineTo(c.dx + h, c.dy - h)
            ..lineTo(c.dx + h, c.dy + h - 2)
            ..lineTo(c.dx + h - 2, c.dy + h - 2),
          paint,
        );
      case _Glyph.close:
        canvas.drawLine(c + const Offset(-h, -h), c + const Offset(h, h), paint);
        canvas.drawLine(c + const Offset(-h, h), c + const Offset(h, -h), paint);
    }
  }

  @override
  bool shouldRepaint(_GlyphPainter old) => old.glyph != glyph || old.color != color;
}

/// The tray icon: its picture and tooltip follow the connection, a click
/// opens the window, and its menu connects, disconnects, switches servers
/// and quits.
/// The servers the tray's menu offers: the first [max] usable ones of the
/// selected subscription (of all, with none selected), and a key that
/// changes whenever the menu should.
@visibleForTesting
(List<(Subscription, NodeView)>, String) trayServers(AppState state, {int max = 10}) {
  final sel = state.selection;
  final list = [
    for (final sub in state.subscriptions)
      if (sub.id == sel.subscription || sel.isEmpty)
        for (final n in sub.nodes)
          if (n.cores.isNotEmpty) (sub, n),
  ].take(max).toList();
  String ms(Subscription sub, NodeView n) {
    final l = state.latencyOf(sub.id, n.fingerprint);
    return l == null || !l.ok ? '-' : '${l.ms}';
  }

  final key = [state.online && !state.busy, for (final (sub, n) in list) '${sub.id}/${n.fingerprint}/${state.isSelected(sub, n)}/${ms(sub, n)}'].join('|');
  return (list, key);
}

class _Tray {
  final AppState state;
  final tray.TrayIcon icon;
  final tray.MenuItem toggle;
  // The native menu stays alive only while something refers to it.
  final tray.Menu menu;
  final tray.Menu servers;
  final List<tray.MenuItem> _serverItems = [];
  String _shown = '';
  String _serversShown = '';

  final VoidCallback onOpen;

  _Tray._(this.state, this.icon, this.menu, this.toggle, this.servers, this.onOpen);

  /// How many servers the menu offers: the first of the selected
  /// subscription, in its order.
  static const _maxServers = 10;

  static _Tray? create(AppState state, {required VoidCallback onOpen, required VoidCallback onExit}) {
    final icon = tray.TrayIcon.create();
    final menu = tray.Menu.create();
    if (icon == null || menu == null) return null;
    tray.MenuItem? item(String label, VoidCallback onClick) {
      final i = tray.MenuItem.createWithLabelAndType(label, tray.MenuItemType.normal);
      i?.addListener((e) {
        // Called from inside the native menu: act once it has returned.
        if (e is tray.MenuItemClickedEvent) Timer.run(onClick);
      });
      menu.addItem(i);
      return i;
    }

    item('Открыть CoreShift', onOpen);
    menu.addSeparator();
    late final _Tray t;
    final toggle = item('Подключить', () => t._toggle());
    final servers = tray.Menu.create();
    final serverItem = tray.MenuItem.createWithLabelAndType('Сервер', tray.MenuItemType.submenu);
    if (servers != null && serverItem != null) {
      serverItem.submenu = servers;
      menu.addItem(serverItem);
    }
    menu.addSeparator();
    item('Выход', onExit);
    if (toggle == null || servers == null) return null;
    t = _Tray._(state, icon, menu, toggle, servers, onOpen);

    icon.setContextMenu(menu);
    if (Platform.isLinux) {
      // A StatusNotifierItem: the panel draws the menu itself, and nativeapi
      // exports it only for the "clicked" trigger; without this neither
      // click did anything. A right click now opens the menu, whose first
      // item opens the window. A left click still does nothing in Plasma
      // 5.27: it calls Activate, which nativeapi accepts and ignores.
      icon.setContextMenuTrigger(tray.ContextMenuTrigger.clicked);
      // The name in the panel's list of tray entries.
      icon.setTitle('CoreShift');
    } else {
      icon.setContextMenuTrigger(tray.ContextMenuTrigger.rightClicked);
    }
    icon.addListener((e) {
      if (e is tray.TrayIconClickedEvent || e is tray.TrayIconDoubleClickedEvent) Timer.run(onOpen);
    });
    t._update();
    icon.setVisible(true);
    state.addListener(t._update);
    return t;
  }

  void _toggle() {
    if (state.busy || !state.online) return;
    if (state.status.active) {
      state.disconnect();
    } else if (!state.selection.available) {
      // Nothing to connect to yet: show the window, where a server is picked.
      onOpen();
    } else {
      state.connect();
    }
  }

  void _update() {
    final st = state.status;
    final (image, tip) = switch (st.state) {
      _ when !state.online => ('idle', 'CoreShift — служба не запущена'),
      ConnState.connected => ('connected', 'CoreShift — подключено${st.node.isEmpty ? '' : ': ${st.node}'}${_speed()}'),
      ConnState.connecting => ('busy', 'CoreShift — подключение…'),
      ConnState.disconnecting => ('busy', 'CoreShift — отключение…'),
      ConnState.failed => ('idle', 'CoreShift — ошибка подключения'),
      ConnState.idle => ('idle', 'CoreShift — отключено'),
    };
    final key = '$image|$tip|${st.active}|${state.busy}|${state.online}';
    var menuChanged = false;
    if (key != _shown) {
      _shown = key;
      icon.icon = tray.ImageAsset.fromAsset('assets/tray/$image.png');
      icon.setTooltip(tip);
      final label = st.active ? 'Отключить' : 'Подключить';
      final enabled = state.online && !state.busy;
      menuChanged = toggle.label != label || toggle.isEnabled != enabled;
      toggle.label = label;
      toggle.isEnabled = enabled;
    }
    // Not only with the icon: a subscription added, a server picked or
    // pinged with the VPN off changes the list alone.
    if ((_updateServers() || menuChanged) && Platform.isLinux) {
      // The panel keeps its copy of the menu until told the layout changed,
      // which nativeapi does only when the menu is set again.
      icon.setContextMenu(menu);
    }
  }

  /// The servers to switch to, the chosen one ticked. Picking one connects
  /// to it, or moves the running connection there. Returns whether the list
  /// changed.
  bool _updateServers() {
    final (list, key) = trayServers(state, max: _maxServers);
    if (key == _serversShown) return false;
    _serversShown = key;
    servers.clear();
    for (final i in _serverItems) {
      i.dispose();
    }
    _serverItems.clear();
    for (final (sub, n) in list) {
      final l = state.latencyOf(sub.id, n.fingerprint);
      final label = '${cleanNodeName(n.name)}${l != null && l.ok ? '   ${l.ms} мс' : ''}';
      final i = tray.MenuItem.createWithLabelAndType(label, tray.MenuItemType.checkbox);
      if (i == null) continue;
      i.state = state.isSelected(sub, n) ? tray.MenuItemState.checked : tray.MenuItemState.unchecked;
      i.isEnabled = state.online && !state.busy;
      i.addListener((e) {
        if (e is tray.MenuItemClickedEvent) {
          Timer.run(() {
            if (state.online && !state.busy) state.connect(subscription: sub.id, fingerprint: n.fingerprint, name: n.name);
          });
        }
      });
      servers.addItem(i);
      _serverItems.add(i);
    }
    if (list.isEmpty) {
      final i = tray.MenuItem.createWithLabelAndType('Нет серверов', tray.MenuItemType.normal);
      if (i != null) {
        i.isEnabled = false;
        servers.addItem(i);
        _serverItems.add(i);
      }
    }
    return true;
  }

  /// The current speed on a second line of the tooltip.
  String _speed() {
    if (state.speed.isEmpty) return '';
    final (up, down) = state.speed.last;
    return '\n↓ ${formatRate(down)}   ↑ ${formatRate(up)}';
  }

  void dispose() {
    state.removeListener(_update);
    icon.dispose();
  }
}
