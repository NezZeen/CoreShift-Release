part of '../widgets.dart';

/// Megabits per second, the unit internet plans are sold in.
String formatRate(int bytesPerSecond) {
  final bits = bytesPerSecond * 8.0;
  if (bits >= 1e9) return '${(bits / 1e9).toStringAsFixed(1)} Гбит/с';
  if (bits >= 1e6) return '${(bits / 1e6).toStringAsFixed(bits >= 1e8 ? 0 : 1)} Мбит/с';
  if (bits >= 1e3) return '${(bits / 1e3).round()} Кбит/с';
  return bits == 0 ? '0' : '${bits.round()} бит/с';
}

String formatBytes(num b) {
  if (b >= 1e12) return '${(b / 1e12).toStringAsFixed(1)} ТБ';
  if (b >= 1e9) return '${(b / 1e9).toStringAsFixed(b >= 1e11 ? 0 : 1)} ГБ';
  if (b >= 1e6) return '${(b / 1e6).toStringAsFixed(0)} МБ';
  return '${(b / 1e3).toStringAsFixed(0)} КБ';
}

String formatAgo(DateTime? t) {
  if (t == null) return 'никогда';
  final d = DateTime.now().difference(t);
  if (d.isNegative) return formatIn(t);
  if (d.inMinutes < 1) return 'только что';
  if (d.inHours < 1) return '${d.inMinutes} мин назад';
  if (d.inDays < 1) return '${d.inHours} ч назад';
  return '${d.inDays} дн назад';
}

String formatIn(DateTime t) {
  final d = t.difference(DateTime.now());
  if (d.inMinutes < 1) return 'сейчас';
  if (d.inHours < 1) return 'через ${d.inMinutes} мин';
  if (d.inDays < 1) return 'через ${d.inHours} ч';
  return 'через ${d.inDays} дн';
}

String formatDate(DateTime t) => '${t.day.toString().padLeft(2, '0')}.${t.month.toString().padLeft(2, '0')}.${t.year}';

String formatDuration(Duration d) {
  String two(int n) => n.toString().padLeft(2, '0');
  return '${two(d.inHours)}:${two(d.inMinutes % 60)}:${two(d.inSeconds % 60)}';
}
