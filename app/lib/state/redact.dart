/// What the journal copied for support hides: the user pastes it into chats.
///
/// The copy keeps what explains a failure: the error, the sites and the
/// addresses it went to, the server's address. It drops only the secrets
/// that now and then reach a line:
///
/// - links lose everything after the scheme: their path and query carry
///   subscription tokens;
/// - UUIDs (VLESS and VMess users) become `<uuid>`.
String redactForSupport(String line) {
  final s = line.replaceAllMapped(_urlRe, (m) => '${m[1]}://…');
  return s.replaceAll(_uuidRe, '<uuid>');
}

final _urlRe = RegExp(
  r'\b([a-zA-Z][a-zA-Z0-9+.-]{1,15})://[^\s"'
  "'"
  r'<>)\]]+',
);
final _uuidRe = RegExp(r'\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b');
