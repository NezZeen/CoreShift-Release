part of '../widgets.dart';

/// Megabits per second, the unit internet plans are sold in.
String formatRate(int bytesPerSecond) {
  final bits = bytesPerSecond * 8.0;
  if (bits >= 1e9) return tr('fmt.rate.gbit', {'v': (bits / 1e9).toStringAsFixed(1)});
  if (bits >= 1e6) return tr('fmt.rate.mbit', {'v': (bits / 1e6).toStringAsFixed(bits >= 1e8 ? 0 : 1)});
  if (bits >= 1e3) return tr('fmt.rate.kbit', {'v': (bits / 1e3).round()});
  return bits == 0 ? '0' : tr('fmt.rate.bit', {'v': bits.round()});
}

String formatBytes(num b) {
  if (b >= 1e12) return tr('fmt.bytes.tb', {'v': (b / 1e12).toStringAsFixed(1)});
  if (b >= 1e9) return tr('fmt.bytes.gb', {'v': (b / 1e9).toStringAsFixed(b >= 1e11 ? 0 : 1)});
  if (b >= 1e6) return tr('fmt.bytes.mb', {'v': (b / 1e6).toStringAsFixed(0)});
  return tr('fmt.bytes.kb', {'v': (b / 1e3).toStringAsFixed(0)});
}

String formatAgo(DateTime? t) {
  if (t == null) return tr('fmt.ago.never');
  final d = DateTime.now().difference(t);
  if (d.isNegative) return formatIn(t);
  if (d.inMinutes < 1) return tr('fmt.ago.justNow');
  if (d.inHours < 1) return tr('fmt.ago.minutes', {'n': d.inMinutes});
  if (d.inDays < 1) return tr('fmt.ago.hours', {'n': d.inHours});
  return tr('fmt.ago.days', {'n': d.inDays});
}

String formatIn(DateTime t) {
  final d = t.difference(DateTime.now());
  if (d.inMinutes < 1) return tr('fmt.in.now');
  if (d.inHours < 1) return tr('fmt.in.minutes', {'n': d.inMinutes});
  if (d.inDays < 1) return tr('fmt.in.hours', {'n': d.inHours});
  return tr('fmt.in.days', {'n': d.inDays});
}

/// "17.10.2026" in Russian, "17 Oct 2026" in English.
String formatDate(DateTime t) => tr('fmt.date', {'d': t.day.toString().padLeft(2, '0'), 'mon': tr('fmt.month.${t.month}'), 'y': t.year});

String formatDuration(Duration d) {
  String two(int n) => n.toString().padLeft(2, '0');
  return '${two(d.inHours)}:${two(d.inMinutes % 60)}:${two(d.inSeconds % 60)}';
}
