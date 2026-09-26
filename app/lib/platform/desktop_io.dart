import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:tray_manager/tray_manager.dart' as tray;
import 'package:window_manager/window_manager.dart';

import '../api/models.dart';
import '../state/app_state.dart';
import '../ui/theme.dart';
import '../ui/widgets.dart' show formatRate;
import 'tray_balloon.dart';

bool _enabled = false;

/// Whether the system can show CoreShift's notifications.
bool get canNotify => Platform.isWindows || Platform.isLinux;

/// Takes over the window: CoreShift draws its own title bar, and closing the
/// window hides it to the tray. [hidden] starts in the tray, as after a
/// self-update. Returns false where there is no such window, and then
/// [DesktopFrame] adds nothing.
Future<bool> initWindow({bool hidden = false}) async {
  if (!Platform.isWindows && !Platform.isLinux) return false;
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

  /// Quits the app. The VPN is the service's, so it stays as it is.
  Future<void> _exit() async {
    await windowManager.hide();
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
  void onWindowClose() => windowManager.hide();

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
        onTap: widget.onTap,
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
/// opens the window, and its menu connects, disconnects and quits.
class _Tray {
  final AppState state;
  final tray.TrayIcon icon;
  final tray.MenuItem toggle;
  // The native menu stays alive only while something refers to it.
  final tray.Menu menu;
  String _shown = '';

  final VoidCallback onOpen;

  _Tray._(this.state, this.icon, this.menu, this.toggle, this.onOpen);

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
    menu.addSeparator();
    item('Выход', onExit);
    if (toggle == null) return null;
    t = _Tray._(state, icon, menu, toggle, onOpen);

    icon.setContextMenu(menu);
    icon.setContextMenuTrigger(tray.ContextMenuTrigger.rightClicked);
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
    if (key == _shown) return;
    _shown = key;
    icon.icon = tray.ImageAsset.fromAsset('assets/tray/$image.png');
    icon.setTooltip(tip);
    toggle.label = st.active ? 'Отключить' : 'Подключить';
    toggle.isEnabled = state.online && !state.busy;
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
