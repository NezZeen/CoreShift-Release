/// The UI's texts in Russian: the source of truth (see strings.dart). One
/// entry a line, grouped by where they are shown.
const uiRu = <String, String>{
  // The navigation (ui/shell.dart).
  'nav.home': 'Главная',
  'nav.servers': 'Серверы',
  'nav.routing': 'Правила',
  'nav.logs': 'Журнал',
  'nav.settings': 'Настройки',

  // The connection's state on the home page (ui/pages/home/hero.dart).
  'home.state.connected': 'Подключено',
  'home.state.connecting': 'Подключение…',
  'home.state.disconnecting': 'Отключение…',
  'home.state.failed': 'Ошибка подключения',
  'home.state.waiting': 'Ждём сеть…',
  'home.state.no_network': 'Нет сети',
  'home.state.idle': 'Отключено',
  'home.connect': 'Подключить',
  'home.disconnect': 'Отключить',

  // The settings (ui/pages/settings_page.dart).
  'settings.language': 'Язык',
  'settings.language.system': 'Как в системе',
  'settings.language.ru': 'Русский',
  'settings.language.en': 'English',
};
