// The check that whoever answers at the address of api.json is the
// CoreShift service: it must know the token. The service answers a nonce
// with HMAC-SHA256 of it keyed with the token (APIProof in
// engine/internal/service/api.go). A program that took the port of a
// service that crashed, leaving its api.json behind, cannot.
//
// SHA-256 is written out here: the app takes no packages beyond the ones
// it has, and dart:io has no hashes.

import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';

/// The headers of the proof.
const nonceHeader = 'X-CoreShift-Nonce';
const proofHeader = 'X-CoreShift-Proof';

final _random = Random.secure();

/// A new nonce: 16 random bytes in hex.
String newNonce() => hex(List<int>.generate(16, (_) => _random.nextInt(256)));

/// What the service answers to [nonce] when it knows [token].
String apiProof(String token, String nonce) => hex(hmacSha256(utf8.encode(token), [...utf8.encode('coreshift-api-proof'), 0, ...utf8.encode(nonce)]));

/// Whether [got], the service's answer to [nonce], proves it knows [token].
/// Compared in constant time.
bool proofValid(String? got, String token, String nonce) {
  if (got == null) return false;
  final want = apiProof(token, nonce);
  final g = got.trim().toLowerCase();
  if (g.length != want.length) return false;
  var diff = 0;
  for (var i = 0; i < want.length; i++) {
    diff |= g.codeUnitAt(i) ^ want.codeUnitAt(i);
  }
  return diff == 0;
}

String hex(List<int> bytes) => bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

const _blockSize = 64;

/// HMAC-SHA256 (RFC 2104).
Uint8List hmacSha256(List<int> key, List<int> message) {
  var k = key.length > _blockSize ? sha256(key) : key;
  final padded = Uint8List(_blockSize)..setRange(0, k.length, k);
  final inner = Uint8List(_blockSize);
  final outer = Uint8List(_blockSize);
  for (var i = 0; i < _blockSize; i++) {
    inner[i] = padded[i] ^ 0x36;
    outer[i] = padded[i] ^ 0x5c;
  }
  return sha256([
    ...outer,
    ...sha256([...inner, ...message]),
  ]);
}

const _k = <int>[
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, //
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

const _mask = 0xffffffff;

int _rotr(int x, int n) => ((x >> n) | (x << (32 - n))) & _mask;

/// SHA-256 (FIPS 180-4).
Uint8List sha256(List<int> data) {
  // The message, a 1 bit, zeros, and its length in bits: whole blocks.
  final bitLength = data.length * 8;
  final total = ((data.length + 9 + _blockSize - 1) ~/ _blockSize) * _blockSize;
  final msg = Uint8List(total)..setRange(0, data.length, data);
  msg[data.length] = 0x80;
  final view = ByteData.sublistView(msg);
  view.setUint32(total - 8, (bitLength ~/ 0x100000000) & _mask);
  view.setUint32(total - 4, bitLength & _mask);

  final h = <int>[0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
  final w = List<int>.filled(64, 0);
  for (var off = 0; off < total; off += _blockSize) {
    for (var i = 0; i < 16; i++) {
      w[i] = view.getUint32(off + i * 4);
    }
    for (var i = 16; i < 64; i++) {
      final s0 = _rotr(w[i - 15], 7) ^ _rotr(w[i - 15], 18) ^ (w[i - 15] >> 3);
      final s1 = _rotr(w[i - 2], 17) ^ _rotr(w[i - 2], 19) ^ (w[i - 2] >> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) & _mask;
    }
    var a = h[0], b = h[1], c = h[2], d = h[3], e = h[4], f = h[5], g = h[6], hh = h[7];
    for (var i = 0; i < 64; i++) {
      final s1 = _rotr(e, 6) ^ _rotr(e, 11) ^ _rotr(e, 25);
      final ch = (e & f) ^ ((~e & _mask) & g);
      final t1 = (hh + s1 + ch + _k[i] + w[i]) & _mask;
      final s0 = _rotr(a, 2) ^ _rotr(a, 13) ^ _rotr(a, 22);
      final maj = (a & b) ^ (a & c) ^ (b & c);
      final t2 = (s0 + maj) & _mask;
      hh = g;
      g = f;
      f = e;
      e = (d + t1) & _mask;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) & _mask;
    }
    h[0] = (h[0] + a) & _mask;
    h[1] = (h[1] + b) & _mask;
    h[2] = (h[2] + c) & _mask;
    h[3] = (h[3] + d) & _mask;
    h[4] = (h[4] + e) & _mask;
    h[5] = (h[5] + f) & _mask;
    h[6] = (h[6] + g) & _mask;
    h[7] = (h[7] + hh) & _mask;
  }
  final out = Uint8List(32);
  final ov = ByteData.sublistView(out);
  for (var i = 0; i < 8; i++) {
    ov.setUint32(i * 4, h[i]);
  }
  return out;
}
