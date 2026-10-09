part of '../servers_page.dart';

/// Picking servers to remove from their subscriptions, as messages are
/// picked in Telegram: a press held on a row picks it, and moving the finger
/// or the pointer on, still pressed, picks the rows it passes; once let go,
/// a tap picks a row or puts it back. Near the edge of the screen the list
/// scrolls by itself. The server in use cannot be picked.
class _Picking extends ChangeNotifier {
  final _ServersPageState page;
  _Picking(this.page);

  AppState get s => page.s;

  /// Whether the list is in picking mode: checkboxes instead of the radio,
  /// a bar instead of the toolbar.
  bool active = false;

  /// The picked servers, by subscription and fingerprint.
  final picked = <String>{};

  /// The rows in the order the list shows them, and their keys, which find
  /// the row under the finger.
  List<(Subscription, NodeView)> _rows = const [];
  final _keys = <String, GlobalKey>{};

  // The drag of a held press: the row it started on, the one it is over,
  // what was picked before it, and whether it picks or puts back.
  int? _anchor;
  int _at = 0;
  Set<String> _base = const {};
  bool _adding = true;
  Offset? _pointer;
  Timer? _scroll;
  double _speed = 0;

  static String _rowId(Subscription sub, NodeView n) => '${sub.id}/${n.fingerprint}/${n.name}';
  static String keyOf(Subscription sub, NodeView n) => AppStateServers.key(sub.id, n.fingerprint);

  GlobalKey rowKey(Subscription sub, NodeView n) => _keys.putIfAbsent(_rowId(sub, n), GlobalKey.new);

  bool isPicked(Subscription sub, NodeView n) => picked.contains(keyOf(sub, n));

  /// The connection runs on it: it stays.
  bool canPick(Subscription sub, NodeView n) => !(s.isSelected(sub, n) && s.status.active);

  static const _inUse = 'К этому серверу вы подключены — его не удалить';

  /// The rows the list shows now; called as the page is built. Servers that
  /// left the subscriptions, or that the connection now runs on, are no
  /// longer picked.
  void listed(List<(Subscription, NodeView)> rows) {
    _rows = rows;
    final ids = {for (final (sub, n) in rows) _rowId(sub, n)};
    _keys.removeWhere((id, _) => !ids.contains(id));
    final pickable = {
      for (final sub in s.subscriptions)
        for (final n in sub.nodes)
          if (canPick(sub, n)) keyOf(sub, n),
    };
    picked.retainWhere(pickable.contains);
  }

  int _index(Subscription sub, NodeView n) => _rows.indexWhere((r) => r.$1.id == sub.id && identical(r.$2, n));

  /// A press held on a row: picking starts there.
  void start(Subscription sub, NodeView n, Offset at) {
    final i = _index(sub, n);
    if (i < 0) return;
    if (!canPick(sub, n)) {
      s.toast(_inUse);
      if (!active) return;
    }
    HapticFeedback.selectionClick();
    active = true;
    _base = {...picked};
    _adding = !picked.contains(keyOf(sub, n));
    _anchor = _at = i;
    _pointer = at;
    _apply();
    notifyListeners();
  }

  /// The held press moved to [at], a global position.
  void move(Offset at) {
    if (_anchor == null) return;
    _pointer = at;
    _follow();
    _autoScroll();
  }

  /// The press was let go.
  void end() {
    _anchor = null;
    _pointer = null;
    _stopScroll();
    if (active && picked.isEmpty) {
      cancel();
    } else {
      notifyListeners();
    }
  }

  /// A tap in picking mode: the row is picked, or put back.
  void toggle(Subscription sub, NodeView n) {
    if (!canPick(sub, n)) {
      s.toast(_inUse);
      return;
    }
    final k = keyOf(sub, n);
    if (!picked.remove(k)) picked.add(k);
    // Like Telegram: putting back the last one ends the picking.
    if (picked.isEmpty) {
      cancel();
      return;
    }
    notifyListeners();
  }

  /// Every row the list shows, but the one in use.
  List<String> get _all => [
    for (final (sub, n) in _rows)
      if (canPick(sub, n)) keyOf(sub, n),
  ];

  bool get allPicked => _all.every(picked.contains);

  void pickAll() {
    picked.addAll(_all);
    notifyListeners();
  }

  void cancel() {
    if (!active && picked.isEmpty) return;
    active = false;
    picked.clear();
    _anchor = null;
    _stopScroll();
    // Picking was learnt: the phone's hint has done its job.
    if (s.prefs[serverHintPref] != true) s.setPref(serverHintPref, true);
    notifyListeners();
  }

  /// «Удалить из подписки»: the picked servers leave their lists at once,
  /// with one toast to bring them back.
  Future<void> delete() async {
    final rows = <(Subscription, NodeView)>[];
    final seen = <String>{};
    for (final sub in s.subscriptions) {
      for (final n in sub.nodes) {
        final k = keyOf(sub, n);
        if (picked.contains(k) && canPick(sub, n) && seen.add(k)) rows.add((sub, n));
      }
    }
    cancel();
    await s.hideNodes(rows, label: rows.length == 1 ? cleanNodeName(rows.single.$2.name) : null);
  }

  /// Esc ends the picking.
  bool onKey(KeyEvent e) {
    if (!active || e is! KeyDownEvent || e.logicalKey != LogicalKeyboardKey.escape) return false;
    cancel();
    return true;
  }

  /// Picks the rows from the one the press started on to the one it is
  /// over now, on top of what was picked before; started on a picked row,
  /// it puts them back instead.
  void _apply() {
    final from = _anchor;
    if (from == null) return;
    final range = <String>{
      for (var j = min(from, _at); j <= max(from, _at); j++)
        if (canPick(_rows[j].$1, _rows[j].$2)) keyOf(_rows[j].$1, _rows[j].$2),
    };
    picked
      ..clear()
      ..addAll(_adding ? _base.union(range) : _base.difference(range));
  }

  RenderBox? _box(int i) {
    final o = _keys[_rowId(_rows[i].$1, _rows[i].$2)]?.currentContext?.findRenderObject();
    return o is RenderBox && o.attached && o.hasSize ? o : null;
  }

  /// The row at the height [y] on the screen; above the first the first,
  /// past the last, or between two, the one before.
  int? _rowAt(double y) {
    int? last;
    for (var j = 0; j < _rows.length; j++) {
      final box = _box(j);
      if (box == null) continue;
      final top = box.localToGlobal(Offset.zero).dy;
      if (y < top) return last ?? j;
      if (y < top + box.size.height) return j;
      last = j;
    }
    return last;
  }

  void _follow() {
    final y = _pointer?.dy;
    if (y == null) return;
    final j = _rowAt(y);
    if (j == null || j == _at) return;
    _at = j;
    final before = {...picked};
    _apply();
    if (picked.length != before.length) HapticFeedback.selectionClick();
    notifyListeners();
  }

  ScrollableState? get _scrollable {
    for (var j = 0; j < _rows.length; j++) {
      final c = _keys[_rowId(_rows[j].$1, _rows[j].$2)]?.currentContext;
      if (c != null) return Scrollable.maybeOf(c);
    }
    return null;
  }

  /// Near the top or the bottom of the page the list scrolls on its own,
  /// faster the closer the finger is to the edge.
  void _autoScroll() {
    final sc = _scrollable;
    final y = _pointer?.dy;
    final box = sc?.context.findRenderObject();
    if (sc == null || y == null || box is! RenderBox || !box.hasSize) return _stopScroll();
    final top = box.localToGlobal(Offset.zero).dy, bottom = top + box.size.height;
    const edge = 64.0, step = 14.0;
    _speed = y < top + edge
        ? -step * ((top + edge - y) / edge).clamp(0.0, 1.0)
        : y > bottom - edge
        ? step * ((y - bottom + edge) / edge).clamp(0.0, 1.0)
        : 0;
    if (_speed == 0) return _stopScroll();
    _scroll ??= Timer.periodic(const Duration(milliseconds: 16), (_) {
      final pos = sc.position;
      final to = (pos.pixels + _speed).clamp(pos.minScrollExtent, pos.maxScrollExtent);
      if (to == pos.pixels) return;
      pos.jumpTo(to);
      // The rows have moved under the finger once laid out again.
      WidgetsBinding.instance.addPostFrameCallback((_) => _follow());
    });
  }

  void _stopScroll() {
    _scroll?.cancel();
    _scroll = null;
    _speed = 0;
  }

  @override
  void dispose() {
    _stopScroll();
    super.dispose();
  }
}

/// The bar that takes the toolbar's place while servers are picked: how many,
/// «Удалить из подписки», «Выбрать все» and «Отмена». A phone puts the
/// removal under the rest, the width of the screen.
class _PickBar extends StatelessWidget {
  final _Picking picking;
  final bool compact;
  const _PickBar({required this.picking, required this.compact});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final n = picking.picked.length;
    final cancel = Tooltip(
      message: compact ? 'Отмена' : 'Отмена (Esc)',
      child: InkResponse(
        onTap: picking.cancel,
        radius: 20,
        child: SizedBox(width: 32, height: 30, child: Icon(Icons.close, size: 20, color: p.muted)),
      ),
    );
    final count = Text('Выбрано: $n', style: display(compact ? 16 : 15));
    final all = TextButton(
      onPressed: picking.allPicked ? null : picking.pickAll,
      style: TextButton.styleFrom(
        foregroundColor: p.accentInk,
        padding: const EdgeInsets.symmetric(horizontal: 10),
        minimumSize: const Size(0, 30),
        tapTargetSize: MaterialTapTargetSize.shrinkWrap,
      ),
      // The style on the text: a button's own would drop the theme's font.
      child: const Text('Выбрать все', style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
    );
    final delete = Btn(label: 'Удалить из подписки', icon: Icons.delete_outline, kind: BtnKind.danger, small: true, onPressed: n == 0 ? null : picking.delete);
    final box = BoxDecoration(
      color: accent.withValues(alpha: .08),
      borderRadius: BorderRadius.circular(12),
      border: Border.all(color: accent.withValues(alpha: .35)),
    );
    if (compact) {
      return Container(
        decoration: box,
        padding: const EdgeInsets.fromLTRB(4, 2, 8, 6),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                cancel,
                const SizedBox(width: 4),
                Expanded(child: count),
                all,
              ],
            ),
            const SizedBox(height: 2),
            Align(
              alignment: Alignment.centerLeft,
              child: Padding(padding: const EdgeInsets.only(left: 6), child: delete),
            ),
          ],
        ),
      );
    }
    return Center(
      child: Container(
        decoration: box,
        padding: const EdgeInsets.fromLTRB(4, 3, 6, 3),
        child: Row(
          children: [
            cancel,
            const SizedBox(width: 6),
            Expanded(child: count),
            all,
            const SizedBox(width: 8),
            delete,
          ],
        ),
      ),
    );
  }
}

/// A row's checkbox while servers are picked, as large as the radio it
/// replaces; the server in use has none to tick.
class _PickBox extends StatelessWidget {
  final bool on;
  final bool enabled;
  const _PickBox({required this.on, required this.enabled});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    // The row's slot would stretch it: its own size, wherever it is put.
    return SizedBox.square(
      dimension: 16,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 120),
        decoration: BoxDecoration(
          color: on ? accent : Colors.transparent,
          borderRadius: BorderRadius.circular(4),
          border: Border.all(color: on ? accent : (enabled ? p.border2 : p.border), width: 2),
        ),
        child: on ? const Icon(Icons.check, size: 11, color: onAccent) : (enabled ? null : Icon(Icons.lock_outline, size: 9, color: p.dim)),
      ),
    );
  }
}
