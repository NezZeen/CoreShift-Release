// What the app tells a user who may not use the CoreShift service on
// Windows: only members of a local group may read its api.json (the
// installer adds the user who installed CoreShift; engine/internal/service,
// apigroup_windows.go).

/// The local group whose members may use the service; APIGroupName in the
/// engine.
const windowsGroup = 'CoreShift Users';

/// How every "no access" message begins, on Windows and Linux.
const accessDeniedPrefix = 'Нет доступа к службе CoreShift:';

/// The account to name in `net localgroup`: the bare name for a local
/// account, DOMAIN\name for a domain one.
String windowsAccount(Map<String, String> env) {
  final user = env['USERNAME'] ?? '';
  final domain = env['USERDOMAIN'] ?? '';
  final computer = env['COMPUTERNAME'] ?? '';
  if (user.isEmpty) return 'ИМЯ';
  return domain.isEmpty || domain.toUpperCase() == computer.toUpperCase() ? user : '$domain\\$user';
}

/// What to tell [account] when Windows refuses it the API file. The
/// service gives the group's members access within half a minute, and
/// the app keeps trying: nobody has to sign out.
String windowsGroupHint(String account) =>
    '$accessDeniedPrefix учётная запись $account не входит в группу «$windowsGroup». '
    'Администратор компьютера может добавить её командой net localgroup "$windowsGroup" "$account" /add '
    '(в командной строке от имени администратора). После этого CoreShift подключится сам, выходить из системы не нужно.';
