import '../../state/app_state.dart' show ruPlural;

/// Every text of the first-run wizard in one place, to move into the
/// dictionary (lib/l10n/) as they are. Texts with a number or a name in them
/// are functions.
abstract final class WizardStrings {
  // Common.
  static const skip = 'Пропустить';
  static const back = 'Назад';
  static const next = 'Дальше';
  static String stepOf(int step, int total) => 'Шаг $step из $total';

  // 1. Welcome.
  static const welcomeTitle = 'Добро пожаловать в CoreShift';
  static const welcomeLine1 = 'CoreShift подключает ваш интернет к серверам из подписки провайдера и сам следит, чтобы связь не пропадала.';
  static const welcomeLine2 = 'Добавьте подписку, проверьте серверы и выберите режим: это займёт около минуты.';
  static const welcomeStart = 'Начать';

  // 2. Subscription.
  static const addTitle = 'Добавьте подписку';
  static const addHint = 'Вставьте ссылку на подписку, которую дал провайдер. Обычно она начинается с https://';
  static const addFieldHint = 'https://… или vless://…';
  static const addPaste = 'Вставить';
  static const addClear = 'Изменить';
  static const addSubmit = 'Добавить';
  static const addManual = 'Добавить сервер вручную';
  static const addFromClipboard = 'Ссылка взята из буфера обмена. Проверьте и нажмите «Добавить».';
  static const addProgress = 'Скачиваем подписку и читаем серверы…';
  static const addEmpty = 'Вставьте ссылку или список серверов';
  static const addClipboardEmpty = 'В буфере обмена нет ссылки на подписку или сервер';
  static const addSubscriptionLabel = 'Подписка';
  static const addServersLabel = 'Серверы';
  static String addServersInText(int n) => '$n ${ruPlural(n, 'сервер', 'сервера', 'серверов')} в тексте';

  // 3. Servers.
  static const checkTitle = 'Проверяем серверы';
  static const checkHintDone = 'Выберите сервер, через который подключаться. Лучшим мы считаем тот, что отвечает быстрее всех.';
  static const checkHintBusy = 'Измеряем, как быстро отвечает каждый сервер.';
  static const checkAuto = 'Автовыбор';
  static const checkAutoText = 'Лучший из серверов, которые выбрал провайдер. Если он перестанет отвечать, подключится следующий.';
  static const checkBest = 'Лучший';
  static const checkAgain = 'Проверить ещё раз';
  static const checkNoneAnswered =
      'Ни один сервер не ответил. Возможно, нет интернета или серверы заблокированы. Можно продолжить и проверить позже на странице «Серверы».';
  static const checkNoServers = 'В подписке нет серверов, которые CoreShift умеет запускать.';
  static String checkProgress(int done, int total) => total > 0 ? 'Проверено серверов: $done из $total' : 'Проверяем серверы…';
  static String checkMs(int ms) => '$ms мс';
  static String checkMore(int n) => 'Ещё ${ruPlural(n, 'сервер', 'сервера', 'серверов')}: $n. Все они на странице «Серверы».';

  // 4. Mode.
  static const modeTitle = 'Как подключаться';
  static const modeHint = 'Это можно изменить потом в настройках.';
  static const modeRecommended = 'Рекомендуем';
  static const modeAllTitle = 'Все приложения';
  static const modeAllText = 'Через VPN идёт весь интернет-трафик компьютера, программам ничего настраивать не нужно. Режим TUN.';
  static const modeProxyTitle = 'Только прокси';
  static const modeProxyText = 'Через VPN пойдут только программы, в которых указан прокси SOCKS5 127.0.0.1:17890. Остальные работают как раньше.';
  static const modeVpnTitle = 'VPN';
  static const modeVpnText = 'Через VPN идёт весь трафик телефона. Android один раз спросит разрешение на VPN.';
  static const modeUnavailable = 'Недоступно на этом компьютере';
  static const modeFailed = 'Не удалось сохранить режим';

  // 5. Done.
  static const doneTitle = 'Всё готово';
  static const doneHint = 'Осталось подключиться. Статус, скорость и смену сервера вы найдёте на главной странице.';
  static const doneServer = 'Сервер';
  static const doneMode = 'Режим';
  static const doneConnect = 'Подключить';
  static const doneLater = 'Позже';
}
