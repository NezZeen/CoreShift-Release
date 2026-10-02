import 'dart:convert';

import 'package:coreshift/ui/qr.dart';
import 'package:flutter_test/flutter_test.dart';

/// Reads [code] back the way a scanner does: the format bits, the mask, the
/// codewords in their zigzag, the blocks with their error correction, and
/// the byte-mode text. Fails on any inconsistency.
String decode(QrCode code) {
  final size = code.size;
  bool m(int x, int y) => code.modules[y][x];

  var format = 0;
  for (var i = 0; i <= 5; i++) {
    format |= (m(8, i) ? 1 : 0) << i;
  }
  format |= (m(8, 7) ? 1 : 0) << 6;
  format |= (m(8, 8) ? 1 : 0) << 7;
  format |= (m(7, 8) ? 1 : 0) << 8;
  for (var i = 9; i < 15; i++) {
    format |= (m(14 - i, 8) ? 1 : 0) << i;
  }
  final mask = format ^ 0x5412;
  expect(mask >> 13, 0, reason: 'level M');
  final pattern = (mask >> 10) & 7;
  expect(QrCode.formatBits(pattern), format);

  final bits = <bool>[];
  for (var right = size - 1; right >= 1; right -= 2) {
    if (right == 6) right = 5;
    for (var vert = 0; vert < size; vert++) {
      for (var j = 0; j < 2; j++) {
        final x = right - j;
        final y = (right + 1) & 2 == 0 ? size - 1 - vert : vert;
        if (!code.isFunction(x, y)) bits.add(m(x, y) ^ QrCode.maskBit(pattern, x, y));
      }
    }
  }
  final raw = QrCode.rawDataModules(code.version) ~/ 8;
  final codewords = [for (var i = 0; i < raw * 8; i += 8) bits.sublist(i, i + 8).fold<int>(0, (b, bit) => b << 1 | (bit ? 1 : 0))];

  // De-interleave: the data codewords column by column, then the ECC ones.
  final dataLen = QrCode.dataCodewords(code.version);
  final eccPerBlock = (raw - dataLen);
  final blocks = _blocks(code.version);
  final ecc = eccPerBlock ~/ blocks;
  final shortBlocks = blocks - raw % blocks, shortData = raw ~/ blocks - ecc;
  final dataBlocks = [for (var b = 0; b < blocks; b++) <int>[]];
  var k = 0;
  for (var i = 0; i <= shortData; i++) {
    for (var b = 0; b < blocks; b++) {
      if (i < shortData || b >= shortBlocks) dataBlocks[b].add(codewords[k++]);
    }
  }
  final eccBlocks = [for (var b = 0; b < blocks; b++) <int>[]];
  for (var i = 0; i < ecc; i++) {
    for (var b = 0; b < blocks; b++) {
      eccBlocks[b].add(codewords[k++]);
    }
  }
  expect(k, raw);
  final divisor = QrCode.reedSolomonDivisor(ecc);
  for (var b = 0; b < blocks; b++) {
    expect(QrCode.reedSolomonRemainder(dataBlocks[b], divisor), eccBlocks[b], reason: 'block $b');
  }

  final data = [for (final d in dataBlocks) ...d];
  var pos = 0;
  int read(int n) {
    var v = 0;
    for (var i = 0; i < n; i++, pos++) {
      v = v << 1 | ((data[pos >> 3] >> (7 - (pos & 7))) & 1);
    }
    return v;
  }

  expect(read(4), 4, reason: 'byte mode');
  final len = read(code.version < 10 ? 8 : 16);
  return utf8.decode([for (var i = 0; i < len; i++) read(8)]);
}

int _blocks(int version) {
  final raw = QrCode.rawDataModules(version) ~/ 8;
  const ecc = [
    -1, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, //
    26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28,
  ];
  return (raw - QrCode.dataCodewords(version)) ~/ ecc[version];
}

void main() {
  test('Reed-Solomon matches the standard\'s worked example', () {
    // "HELLO WORLD", version 1-M (thonky.com's QR code tutorial).
    final data = [32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236, 17, 236, 17];
    expect(QrCode.reedSolomonRemainder(data, QrCode.reedSolomonDivisor(10)), [196, 35, 39, 119, 235, 215, 231, 226, 93, 23]);
  });

  test('format and version bits match the standard\'s tables', () {
    expect(QrCode.formatBits(0), int.parse('101010000010010', radix: 2));
    expect(QrCode.formatBits(7), int.parse('100101010100000', radix: 2));
    expect(QrCode.versionBits(7), int.parse('000111110010010100', radix: 2));
    expect(QrCode.versionBits(40), int.parse('101000110001101001', radix: 2));
  });

  test('alignment patterns sit where the standard puts them', () {
    expect(QrCode.alignmentPositions(1), isEmpty);
    expect(QrCode.alignmentPositions(2), [6, 18]);
    expect(QrCode.alignmentPositions(7), [6, 22, 38]);
    expect(QrCode.alignmentPositions(15), [6, 26, 48, 70]);
    expect(QrCode.alignmentPositions(32), [6, 34, 60, 86, 112, 138]);
    expect(QrCode.alignmentPositions(40), [6, 30, 58, 86, 114, 142, 170]);
  });

  test('versions hold as many bytes as the standard says at level M', () {
    int bytes(int v) => (QrCode.dataCodewords(v) * 8 - 4 - (v < 10 ? 8 : 16)) ~/ 8;
    expect(
      {
        for (final v in [1, 2, 3, 7, 10, 40]) v: bytes(v),
      },
      {1: 14, 2: 26, 3: 42, 7: 122, 10: 213, 40: 2331},
    );
  });

  test('codes read back to their text', () {
    for (final text in [
      'https://sub.example.com/AbCdEf1234567890',
      'https://panel.example.org:2096/sub/7f3c9a1e-5b2d-4e8f-9a0c-1d2e3f4a5b6c?name=Мой провайдер',
      'vless://${'x' * 600}',
    ]) {
      final code = QrCode.text(text);
      expect(code.size, code.version * 4 + 17);
      expect(decode(code), text);
    }
  });

  test('picks the smallest version that fits', () {
    expect(QrCode.text('a' * 14).version, 1);
    expect(QrCode.text('a' * 15).version, 2);
    expect(() => QrCode.text('a' * 3000), throwsArgumentError);
  });
}
