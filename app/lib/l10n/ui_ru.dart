/// The UI's texts in Russian: the source of truth (see strings.dart). One
/// entry a line, grouped by where they are shown; each area of the app
/// keeps its own in parts/, under a prefix of its own.
library;

import 'parts/errors.dart';
import 'parts/settings.dart';
import 'parts/routing.dart';
import 'parts/servers.dart';
import 'parts/home.dart';
import 'parts/events.dart';
import 'parts/state.dart';
import 'parts/shell.dart';

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

  // The areas (parts/).
  ...errorsRu,
  ...settingsRu,
  ...routingRu,
  ...serversRu,
  ...homeRu,
  ...eventsRu,
  ...stateRu,
  ...shellRu,
};
