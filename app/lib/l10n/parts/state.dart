/// The UI's texts of the rest of the app state: connection, updates, speed, imports, keys "state.…" (see strings.dart):
/// Russian, the source of truth, and English, key for key.
library;

const stateRu = <String, String>{
  // The sources of the journal's lines the app words itself.
  'state.src.update': 'обновление',
  'state.src.version': 'версия',
  'state.src.cores': 'ядра',
  'state.src.network': 'сеть',
  'state.src.speed': 'скорость',

  'state.ip.error': 'Не удалось узнать адрес',

  'state.appUpdate.allowInstall': 'Разрешите CoreShift устанавливать приложения и вернитесь: обновление продолжится.',
  'state.appUpdate.log.latest': 'проверено: CoreShift {version} — последняя версия',
  'state.appUpdate.log.downloadedWaiting': 'скачана версия {version}, установится после отключения VPN',
  'state.appUpdate.log.downloaded': 'скачана версия {version}',
  'state.appUpdate.log.downloadedInstalling': 'скачана версия {version}, устанавливается',

  'state.version.updated': 'CoreShift обновлён: {old} → {version}',
  'state.version.older': 'Установлена более ранняя версия CoreShift: {old} → {version}',
  'state.version.rebuilt': 'CoreShift пересобран: {version}',
  'state.version.updatedTitle': 'CoreShift обновлён',

  'state.daemon.streamClosed': 'поток событий закрыт',
  'state.daemon.silentTitle': 'Служба CoreShift не отвечает',
  'state.daemon.silentBody': 'VPN может не работать. Откройте CoreShift, чтобы узнать подробности.',
  'state.daemon.starting': 'Запускаем службу CoreShift',

  'state.direct.logAllVpn': 'всё через VPN: прямые соединения в этой сети не проходят',

  'state.link.cannotOpen': 'Панель прислала ссылку, которую нельзя открыть',
  'state.link.demo': 'В демо-режиме ссылки не открываются',
  'state.link.noApp': 'Не удалось открыть ссылку: нет приложения, которое её откроет',

  'state.import.qrNoLink': 'В QR-коде нет ссылки на подписку или сервер',
  'state.import.notLink': 'Это не ссылка на подписку или сервер',
  'state.import.alreadyAdded': 'Эта подписка уже добавлена',
  'state.import.happCrypt': 'Это зашифрованная ссылка Happ: её открывает только Happ. Скопируйте из панели обычную ссылку на подписку.',

  'state.settingsDiff.yes': 'да',
  'state.settingsDiff.no': 'нет',
  'state.settingsDiff.emptyText': '«»',
  'state.settingsDiff.entries': '{n} {n|запись|записи|записей}',
  'state.settingsDiff.listChanged': '{path}: изменён список ({count})',
  'state.settingsDiff.changed': '{path}: изменён',

  'state.speed.logStartVpn': 'тест скорости через «{node}»',
  'state.speed.logStartPlain': 'тест скорости без VPN',
  'state.speed.logTestServer': ', сервер теста {name}',
  'state.speed.logDone': 'загрузка {down}, отдача {up}, задержка {ms} мс{where}',
  'state.speed.logFailed': 'тест не удался: {message}',
  'state.speed.alreadyRunning': 'Тест скорости уже идёт',
  'state.speed.mbit': '{v} Мбит/с',
};

const stateEn = <String, String>{
  'state.src.update': 'update',
  'state.src.version': 'version',
  'state.src.cores': 'cores',
  'state.src.network': 'network',
  'state.src.speed': 'speed',

  'state.ip.error': 'Could not find out the address',

  'state.appUpdate.allowInstall': 'Allow CoreShift to install apps and come back: the update will continue.',
  'state.appUpdate.log.latest': 'checked: CoreShift {version} is the latest version',
  'state.appUpdate.log.downloadedWaiting': 'version {version} downloaded, will install after the VPN disconnects',
  'state.appUpdate.log.downloaded': 'version {version} downloaded',
  'state.appUpdate.log.downloadedInstalling': 'version {version} downloaded, installing',

  'state.version.updated': 'CoreShift updated: {old} → {version}',
  'state.version.older': 'An earlier version of CoreShift installed: {old} → {version}',
  'state.version.rebuilt': 'CoreShift rebuilt: {version}',
  'state.version.updatedTitle': 'CoreShift updated',

  'state.daemon.streamClosed': 'event stream closed',
  'state.daemon.silentTitle': 'The CoreShift service is not responding',
  'state.daemon.silentBody': 'The VPN may not work. Open CoreShift for details.',
  'state.daemon.starting': 'Starting the CoreShift service',

  'state.direct.logAllVpn': 'everything through VPN: direct connections do not work in this network',

  'state.link.cannotOpen': 'The panel sent a link that cannot be opened',
  'state.link.demo': 'Links do not open in demo mode',
  'state.link.noApp': 'Could not open the link: no app to open it',

  'state.import.qrNoLink': 'The QR code has no subscription or server link',
  'state.import.notLink': 'This is not a subscription or server link',
  'state.import.alreadyAdded': 'This subscription is already added',
  'state.import.happCrypt': 'This is an encrypted Happ link: only Happ can open it. Copy a regular subscription link from the panel.',

  'state.settingsDiff.yes': 'yes',
  'state.settingsDiff.no': 'no',
  'state.settingsDiff.emptyText': '“”',
  'state.settingsDiff.entries': '{n} {n|entry|entries}',
  'state.settingsDiff.listChanged': '{path}: list changed ({count})',
  'state.settingsDiff.changed': '{path}: changed',

  'state.speed.logStartVpn': 'speed test through “{node}”',
  'state.speed.logStartPlain': 'speed test without VPN',
  'state.speed.logTestServer': ', test server {name}',
  'state.speed.logDone': 'download {down}, upload {up}, latency {ms} ms{where}',
  'state.speed.logFailed': 'test failed: {message}',
  'state.speed.alreadyRunning': 'A speed test is already running',
  'state.speed.mbit': '{v} Mbit/s',
};
