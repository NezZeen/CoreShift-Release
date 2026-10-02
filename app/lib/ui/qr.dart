import 'dart:convert';
import 'dart:math';

import 'package:flutter/material.dart';

/// A QR code (ISO/IEC 18004) of some text, in byte mode with error
/// correction level M, the smallest version that fits. Written here to keep
/// the app free of third-party packages; it follows the reference algorithm
/// of Project Nayuki's QR Code generator.
class QrCode {
  /// 1 to 40; the code is 17 + 4 × version modules wide.
  final int version;
  final int size;

  /// The chosen mask pattern, 0 to 7.
  late final int mask;

  /// modules[y][x]: true is dark.
  final List<List<bool>> modules;
  final List<List<bool>> _function;

  QrCode._(this.version) : size = version * 4 + 17, modules = _grid(version * 4 + 17), _function = _grid(version * 4 + 17);

  static List<List<bool>> _grid(int n) => List.generate(n, (_) => List.filled(n, false));

  /// Whether module ([x], [y]) belongs to a fixed pattern rather than data.
  @visibleForTesting
  bool isFunction(int x, int y) => _function[y][x];

  /// The code of [text], UTF-8 encoded. Throws [ArgumentError] when it is
  /// too long for any version.
  factory QrCode.text(String text) {
    final data = utf8.encode(text);
    for (var v = 1; v <= 40; v++) {
      final ccBits = v < 10 ? 8 : 16;
      final capacity = dataCodewords(v) * 8;
      if (4 + ccBits + data.length * 8 <= capacity && data.length < (1 << ccBits)) {
        final bits = _Bits()
          ..add(4, 4) // byte mode
          ..add(data.length, ccBits);
        for (final b in data) {
          bits.add(b, 8);
        }
        bits.add(0, min(4, capacity - bits.length));
        bits.add(0, (8 - bits.length % 8) % 8);
        for (var pad = 0xEC; bits.length < capacity; pad ^= 0xEC ^ 0x11) {
          bits.add(pad, 8);
        }
        return QrCode._(v).._build(bits.bytes());
      }
    }
    throw ArgumentError('text too long for a QR code');
  }

  void _build(List<int> data) {
    _drawFunctionPatterns();
    _drawCodewords(_addEccAndInterleave(data));
    var best = 0, bestPenalty = 1 << 30;
    for (var m = 0; m < 8; m++) {
      _applyMask(m);
      _drawFormatBits(m);
      final p = _penalty();
      if (p < bestPenalty) {
        best = m;
        bestPenalty = p;
      }
      _applyMask(m); // undo
    }
    mask = best;
    _applyMask(mask);
    _drawFormatBits(mask);
  }

  // ------------------------------------------------------------ patterns

  void _set(int x, int y, bool dark) {
    modules[y][x] = dark;
    _function[y][x] = true;
  }

  void _drawFunctionPatterns() {
    for (var i = 0; i < size; i++) {
      _set(6, i, i.isEven);
      _set(i, 6, i.isEven);
    }
    _drawFinder(3, 3);
    _drawFinder(size - 4, 3);
    _drawFinder(3, size - 4);
    final pos = alignmentPositions(version);
    final n = pos.length;
    for (var i = 0; i < n; i++) {
      for (var j = 0; j < n; j++) {
        // Not over the three finder patterns.
        if ((i == 0 && j == 0) || (i == 0 && j == n - 1) || (i == n - 1 && j == 0)) continue;
        for (var dy = -2; dy <= 2; dy++) {
          for (var dx = -2; dx <= 2; dx++) {
            _set(pos[i] + dx, pos[j] + dy, max(dx.abs(), dy.abs()) != 1);
          }
        }
      }
    }
    _drawFormatBits(0); // reserves the area; redrawn with the real mask
    _drawVersion();
  }

  void _drawFinder(int x, int y) {
    for (var dy = -4; dy <= 4; dy++) {
      for (var dx = -4; dx <= 4; dx++) {
        final d = max(dx.abs(), dy.abs());
        final xx = x + dx, yy = y + dy;
        if (xx >= 0 && xx < size && yy >= 0 && yy < size) _set(xx, yy, d != 2 && d != 4);
      }
    }
  }

  void _drawFormatBits(int mask) {
    final bits = formatBits(mask);
    bool bit(int i) => (bits >> i) & 1 == 1;
    for (var i = 0; i <= 5; i++) {
      _set(8, i, bit(i));
    }
    _set(8, 7, bit(6));
    _set(8, 8, bit(7));
    _set(7, 8, bit(8));
    for (var i = 9; i < 15; i++) {
      _set(14 - i, 8, bit(i));
    }
    for (var i = 0; i < 8; i++) {
      _set(size - 1 - i, 8, bit(i));
    }
    for (var i = 8; i < 15; i++) {
      _set(8, size - 15 + i, bit(i));
    }
    _set(8, size - 8, true); // the dark module
  }

  void _drawVersion() {
    if (version < 7) return;
    final bits = versionBits(version);
    for (var i = 0; i < 18; i++) {
      final dark = (bits >> i) & 1 == 1;
      final a = size - 11 + i % 3, b = i ~/ 3;
      _set(a, b, dark);
      _set(b, a, dark);
    }
  }

  // ---------------------------------------------------------------- data

  List<int> _addEccAndInterleave(List<int> data) {
    final blocks = _blocksM[version], eccLen = _eccM[version];
    final raw = rawDataModules(version) ~/ 8;
    final shortBlocks = blocks - raw % blocks, shortLen = raw ~/ blocks;
    final divisor = reedSolomonDivisor(eccLen);
    final all = <List<int>>[];
    for (var i = 0, k = 0; i < blocks; i++) {
      final len = shortLen - eccLen + (i < shortBlocks ? 0 : 1);
      final dat = data.sublist(k, k + len);
      k += len;
      final ecc = reedSolomonRemainder(dat, divisor);
      all.add([...dat, if (i < shortBlocks) 0, ...ecc]);
    }
    final out = <int>[];
    for (var i = 0; i < all[0].length; i++) {
      for (var j = 0; j < all.length; j++) {
        // The padding byte of the short blocks is not sent.
        if (i != shortLen - eccLen || j >= shortBlocks) out.add(all[j][i]);
      }
    }
    return out;
  }

  void _drawCodewords(List<int> data) {
    var i = 0;
    for (var right = size - 1; right >= 1; right -= 2) {
      if (right == 6) right = 5; // the vertical timing pattern
      for (var vert = 0; vert < size; vert++) {
        for (var j = 0; j < 2; j++) {
          final x = right - j;
          final upward = (right + 1) & 2 == 0;
          final y = upward ? size - 1 - vert : vert;
          if (!_function[y][x] && i < data.length * 8) {
            modules[y][x] = (data[i >> 3] >> (7 - (i & 7))) & 1 == 1;
            i++;
          }
        }
      }
    }
  }

  void _applyMask(int m) {
    for (var y = 0; y < size; y++) {
      for (var x = 0; x < size; x++) {
        if (!_function[y][x] && maskBit(m, x, y)) modules[y][x] = !modules[y][x];
      }
    }
  }

  /// Whether mask pattern [m] inverts module ([x], [y]).
  static bool maskBit(int m, int x, int y) => switch (m) {
    0 => (x + y) % 2 == 0,
    1 => y % 2 == 0,
    2 => x % 3 == 0,
    3 => (x + y) % 3 == 0,
    4 => (x ~/ 3 + y ~/ 2) % 2 == 0,
    5 => x * y % 2 + x * y % 3 == 0,
    6 => (x * y % 2 + x * y % 3) % 2 == 0,
    _ => ((x + y) % 2 + x * y % 3) % 2 == 0,
  };

  /// How hard the code is to read: runs of one colour, 2×2 blocks and an
  /// uneven share of dark modules. The finder-like pattern rule of the
  /// standard is left out; any mask decodes, this only picks a good one.
  int _penalty() {
    var p = 0;
    for (var a = 0; a < size; a++) {
      var runRow = 1, runCol = 1;
      for (var b = 1; b < size; b++) {
        if (modules[a][b] == modules[a][b - 1]) {
          runRow++;
        } else {
          if (runRow >= 5) p += runRow - 2;
          runRow = 1;
        }
        if (modules[b][a] == modules[b - 1][a]) {
          runCol++;
        } else {
          if (runCol >= 5) p += runCol - 2;
          runCol = 1;
        }
      }
      if (runRow >= 5) p += runRow - 2;
      if (runCol >= 5) p += runCol - 2;
    }
    var dark = 0;
    for (var y = 0; y < size; y++) {
      for (var x = 0; x < size; x++) {
        if (modules[y][x]) dark++;
        if (x > 0 && y > 0) {
          final c = modules[y][x];
          if (c == modules[y][x - 1] && c == modules[y - 1][x] && c == modules[y - 1][x - 1]) p += 3;
        }
      }
    }
    final total = size * size;
    p += ((dark * 20 - total * 10).abs() + total - 1) ~/ total * 10;
    return p;
  }

  // -------------------------------------------------------------- tables

  /// The 15 format bits for level M and [mask], with their BCH code.
  static int formatBits(int mask) {
    final data = 0 << 3 | mask; // level M is 00
    var rem = data;
    for (var i = 0; i < 10; i++) {
      rem = (rem << 1) ^ ((rem >> 9) * 0x537);
    }
    return (data << 10 | rem) ^ 0x5412;
  }

  /// The 18 version bits, with their BCH code (versions 7 and up).
  static int versionBits(int version) {
    var rem = version;
    for (var i = 0; i < 12; i++) {
      rem = (rem << 1) ^ ((rem >> 11) * 0x1F25);
    }
    return version << 12 | rem;
  }

  /// The centres of the alignment patterns, as both rows and columns.
  static List<int> alignmentPositions(int version) {
    if (version == 1) return const [];
    final n = version ~/ 7 + 2;
    final step = (version * 8 + n * 3 + 5) ~/ (n * 4 - 4) * 2;
    final size = version * 4 + 17;
    return [6, for (var i = n - 2; i >= 0; i--) size - 7 - i * step];
  }

  /// The modules left for data and error correction.
  static int rawDataModules(int version) {
    var r = (16 * version + 128) * version + 64;
    if (version >= 2) {
      final n = version ~/ 7 + 2;
      r -= (25 * n - 10) * n - 55;
      if (version >= 7) r -= 36;
    }
    return r;
  }

  /// How many 8-bit data codewords version [version] holds at level M.
  static int dataCodewords(int version) => rawDataModules(version) ~/ 8 - _eccM[version] * _blocksM[version];

  static const _eccM = [
    -1, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, //
    26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28,
  ];
  static const _blocksM = [
    -1, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16, //
    17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49,
  ];

  // ------------------------------------------------------- Reed-Solomon

  static List<int> reedSolomonDivisor(int degree) {
    final r = List.filled(degree, 0);
    r[degree - 1] = 1;
    var root = 1;
    for (var i = 0; i < degree; i++) {
      for (var j = 0; j < degree; j++) {
        r[j] = _mul(r[j], root);
        if (j + 1 < degree) r[j] ^= r[j + 1];
      }
      root = _mul(root, 0x02);
    }
    return r;
  }

  static List<int> reedSolomonRemainder(List<int> data, List<int> divisor) {
    final r = List.filled(divisor.length, 0, growable: true);
    for (final b in data) {
      final factor = b ^ r.removeAt(0);
      r.add(0);
      for (var i = 0; i < r.length; i++) {
        r[i] ^= _mul(divisor[i], factor);
      }
    }
    return r;
  }

  /// Multiplication in GF(2^8) modulo x^8 + x^4 + x^3 + x^2 + 1.
  static int _mul(int x, int y) {
    var z = 0;
    for (var i = 7; i >= 0; i--) {
      z = (z << 1) ^ ((z >> 7) * 0x11D);
      z ^= ((y >> i) & 1) * x;
    }
    return z;
  }
}

class _Bits {
  final _bits = <bool>[];
  int get length => _bits.length;

  void add(int value, int n) {
    for (var i = n - 1; i >= 0; i--) {
      _bits.add((value >> i) & 1 == 1);
    }
  }

  List<int> bytes() => [for (var i = 0; i < _bits.length; i += 8) _bits.sublist(i, i + 8).fold(0, (b, bit) => b << 1 | (bit ? 1 : 0))];
}

/// Draws [code] black on white with the quiet zone the standard asks for,
/// whatever the theme: readers expect dark modules on a light ground.
class QrView extends StatelessWidget {
  final QrCode code;
  final double size;
  const QrView({super.key, required this.code, this.size = 240});

  @override
  Widget build(BuildContext context) => Container(
    width: size,
    height: size,
    decoration: BoxDecoration(color: Colors.white, borderRadius: BorderRadius.circular(10)),
    child: CustomPaint(painter: _QrPainter(code)),
  );
}

class _QrPainter extends CustomPainter {
  final QrCode code;
  _QrPainter(this.code);

  static const _quiet = 4;

  @override
  void paint(Canvas canvas, Size size) {
    final n = code.size + 2 * _quiet;
    final m = size.shortestSide / n;
    final paint = Paint()
      ..color = Colors.black
      ..isAntiAlias = false;
    for (var y = 0; y < code.size; y++) {
      for (var x = 0; x < code.size; x++) {
        if (code.modules[y][x]) {
          // A hair larger, so neighbours leave no seams.
          canvas.drawRect(Rect.fromLTWH((x + _quiet) * m, (y + _quiet) * m, m + .5, m + .5), paint);
        }
      }
    }
  }

  @override
  bool shouldRepaint(_QrPainter old) => old.code != code;
}
