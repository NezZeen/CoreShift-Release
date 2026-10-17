/// The UI's texts of the rules page, own rules, lists, leak and DNS checks, keys "rules.…", "leak.…", "dnscheck.…" (see strings.dart):
/// Russian, the source of truth, and English, key for key.
library;

const routingRu = <String, String>{
  // The rules page (ui/pages/routing_page.dart).
  'rules.page.title': 'Правила',
  'rules.page.sub.selected': 'Через VPN идут только выбранные сайты и программы, остальное — напрямую.',
  'rules.page.sub.all': 'Сайты и программы, которые работают напрямую, без VPN. Всё остальное идёт через VPN.',
  'rules.always_direct.tip': 'Что всегда работает напрямую',
  'rules.always_direct.title': 'Всегда напрямую',
  'rules.always_direct.selected': 'Локальная сеть и российские сайты в этом режиме и так работают напрямую.',
  'rules.always_direct.all': 'Локальная сеть (10.0.0.0/8, 192.168.0.0/16, .local, .lan) и адрес VPN-сервера всегда работают напрямую.',
  'rules.sub.domains': 'сайт и все его поддомены',
  'rules.add': 'Добавить',
  'rules.list.empty': 'Пока пусто',
  'rules.list.remove': 'Убрать из списка',
  'rules.proxy.title': 'Сайты и адреса через VPN',
  'rules.proxy.desc': 'Только они идут через VPN. Всё остальное открывается напрямую, как без VPN.',
  'rules.proxy.empty': 'Пока пусто — всё идёт напрямую',
  'rules.proxy.remove': 'Больше не через VPN',
  'rules.proxy.apps.desc': 'Весь трафик этих программ идёт через VPN, куда бы они ни подключались. Удобно для мессенджеров и игр, заблокированных целиком.',
  'rules.direct.title': 'Сайты и адреса без VPN',
  'rules.direct.desc': 'Можно указать IP-адрес или подсеть, например 10.8.0.0/16 для рабочей сети.',
  'rules.direct.empty': 'Пока пусто — всё идёт через VPN',
  'rules.direct.remove': 'Снова через VPN',
  'rules.always.title': 'Всегда через VPN',
  'rules.always.sub': 'важнее исключений',
  'rules.always.desc': 'Для сайтов, которые попали под исключения, но без VPN не открываются: например, заблокированный сайт в зоне .ru.',
  'rules.notice.title': 'Сейчас включён режим «Только прокси».',
  'rules.notice.text': 'В нём через VPN идут только программы, настроенные на прокси, и правила ниже не действуют.',
  'rules.notice.btn': 'Все приложения',
  'rules.mode.all.title': 'Всё через VPN',
  'rules.mode.selected.title': 'Только выбранное',
  'rules.mode.all.text': 'Кроме исключений. Скрывает весь трафик, российские сайты можно пустить напрямую.',
  'rules.mode.selected.text': 'Через VPN идут только выбранные сервисы, остальное напрямую. Банки и российские сайты работают как без VPN.',
  'rules.presets.title': 'Готовые наборы',
  'rules.presets.russia.title': 'Российские сайты напрямую',
  'rules.presets.russia.desc':
      'Госуслуги, банки, Яндекс, VK и другие российские сервисы откроются без VPN — '
      'быстрее и без блокировок «из-за границы». Google и YouTube всегда идут через VPN, как и сайты, заблокированные в России, даже на .ru. '
      'Списки обновляются сами.',
  'rules.presets.abroad.title': 'Российские сайты на зарубежных серверах — через VPN',
  'rules.presets.abroad.desc':
      'Сайт на .ru за Cloudflare или у зарубежного хостинга пойдёт через VPN: напрямую такие часто тормозят. '
      'Если сервис не пускает из-за границы, добавьте его в «Сайты и адреса без VPN».',
  'rules.geo.fold': 'Источник баз',
  'rules.block.title': 'Блокировать',
  'rules.block.sub': 'не открывать совсем',
  'rules.block.desc': 'Сайты из этого списка не откроются ни через VPN, ни напрямую: реклама, трекеры, отвлекающие сайты.',
  'rules.block.remove': 'Разблокировать',

  // Popular services (ui/pages/rule_lists.dart).
  'rules.list.toast.added': 'Добавлено: {n}',
  'rules.svc.title': 'Популярные сервисы',
  'rules.svc.sub': 'в один клик',
  'rules.svc.addresses': '+ адреса серверов',
  'rules.svc.instagram_facebook': 'Instagram и Facebook',
  'rules.svc.toast.off': '{name} больше не идёт через VPN',
  'rules.svc.toast.on': '{name} теперь идёт через VPN',

  // The user's own rules (ui/pages/user_rules.dart).
  'rules.own.title': 'Свои правила',
  'rules.own.sub': 'сверху вниз',
  'rules.own.info':
      'Правила проверяются по порядку: первое подходящее решает. Они важнее готовых наборов, '
      'но ваши списки сайтов «без VPN», «через VPN» и «Блокировать» важнее правил. '
      'Правила geoip срабатывают для сайтов, которых нет ни в одном правиле по имени.',
  'rules.own.desc':
      'Категория сайтов, адресов, сайт или подсеть. Например: geosite:category-ads-all — блокировать, '
      'geosite:youtube — через VPN, geoip:ru — напрямую.',
  'rules.own.empty': 'Пока нет своих правил',
  'rules.own.toast.changed': 'Правило изменено',
  'rules.own.toast.added': 'Правило добавлено',
  'rules.own.up': 'Выше',
  'rules.own.down': 'Ниже',
  'rules.own.delete': 'Удалить правило',
  'rules.geo.err': 'База не загрузилась: категорию проверить нельзя.',
  'rules.geo.idle': 'База ещё не загружена.',
  'rules.geo.loading': 'База ещё загружается…',
  'rules.geo.loading_pct': 'База ещё загружается… {pct}%',
  'rules.geo.found': 'Есть {where}.',
  'rules.geo.resolved': 'Будет использована',
  'rules.geo.ambiguous': 'Подходит несколько категорий, выберите:',
  'rules.geo.typo': 'Нет в базе. Может быть,',
  'rules.geo.missing': 'Нет такой категории {where}.',
  'rules.note.waiting': 'ждёт базу',
  'rules.note.missing': 'нет в базе',
  'rules.note.maybe': 'Может быть:',
  'rules.note.ambiguous': 'неоднозначно',
  'rules.note.choose': 'Выберите:',
  'rules.note.error': 'база не загрузилась',
  'rules.geo.retry': 'Повторить',
  'rules.geo.load': 'Загрузить',
  'rules.geo.categories': '{n} {n|категория|категории|категорий}',
  'rules.geo.sub.loading': '{name} · загружается…',
  'rules.geo.sub.error': '{name} · ошибка загрузки',
  'rules.geo.src.runetfreedom':
      'Списки runetfreedom (russia-blocked-geosite): ru-blocked — сайты, заблокированные в России, category-ads-all — реклама, '
      'и категории v2fly. Это один файл geosite.dat на десятки мегабайт; категории geoip берутся у SagerNet.',
  'rules.geo.src.custom':
      'Ссылка https с {name} на файлы .srs, например https://example.org/geosite-{name}.srs, '
      'или ссылка на geosite.dat / geoip.dat в формате v2ray. Пустое поле — эти категории у SagerNet.',
  'rules.geo.src.sagernet': 'Категории SagerNet (sing-geosite, sing-geoip): geosite:youtube, geosite:category-ads-all, geoip:ru и другие.',
  'rules.geo.src.intro': 'Откуда берутся категории geosite: и geoip: для своих правил.',
  'rules.geo.src.geosite_url': 'Категории сайтов (geosite)',
  'rules.geo.src.geoip_url': 'Категории адресов (geoip)',
  'rules.geo.src.presets.title': 'Готовые наборы — тоже отсюда',
  'rules.geo.src.presets.desc':
      'Списки «Российские сайты напрямую» и блокировки рекламы возьмутся из этого источника. '
      'Если нужной категории там нет, работает встроенная копия.',
  'rules.geo.src.third_party': 'Списки ведёт сторонний источник: CoreShift проверяет только их формат и размер.',

  // The DNS leak check (ui/pages/leak_check.dart).
  'leak.title': 'Проверка утечки DNS',
  'leak.desc.ready': 'Узнать, видит ли провайдер ваши DNS-запросы. Проверка идёт через туннель, как у приложений. Использует сервис bash.ws; страну адреса VPN-сервера (не вашего) сверяет ещё с несколькими сервисами: ipwho.is, ipinfo.io и другими',
  'leak.desc.not_ready': 'Подключитесь в режиме «Все приложения», чтобы проверить',
  'leak.btn': 'Проверить DNS',
  'leak.failed': 'Проверка не удалась',
  'leak.advice.android':
      'Что сделать: уберите «Частный DNS», заданный вручную в настройках Android, включите «Защиту от утечек DNS» выше, '
      'переподключитесь и проверьте снова.',
  'leak.advice.other':
      'Что сделать: включите «Защиту от утечек DNS» выше, переподключитесь и проверьте снова. '
      'Если не поможет, проверьте, не идёт ли bash.ws напрямую по вашим правилам.',
  'leak.own': 'ваш DNS',
  'leak.dns.apps': 'DNS, которым пользуются приложения (через VPN){provider}',
  'leak.ok.title': 'Утечки нет',
  'leak.ok.own': 'DNS идёт через VPN к выбранному вами DNS{place}. Провайдер этих запросов не видит.',
  'leak.ok.fakeip': 'Адреса сайтов приложениям выдаёт сам туннель, а имена разрешает VPN-сервер. Провайдер ваших DNS-запросов не видит.',
  'leak.ok.vpn': 'DNS-запросы приложений уходят через VPN к серверам на его стороне. Провайдер их не видит.',
  'leak.leak.title': 'Есть утечка DNS',
  'leak.leak.text': 'Часть DNS-запросов обрабатывает DNS вашего провайдера или сети: он видит, какие сайты вы открываете.',
  'leak.bypass.title': 'Проверка прошла мимо VPN',
  'leak.bypass.text':
      'bash.ws увидел ваш настоящий адрес, поэтому результат недостоверен. Похоже, сервер пускает этот сайт напрямую. '
      'Выберите другой сервер или переподключитесь и проверьте снова.',
  'leak.unknown.title': 'Результат неточный',
  'leak.unknown.no_exit': 'bash.ws не сообщил, откуда пришла проверка. Попробуйте ещё раз.',
  'leak.unknown.no_home': 'Не удалось узнать ваш адрес без VPN, поэтому нельзя сказать, шла ли проверка через VPN. Попробуйте ещё раз.',
  'leak.exit.seen': 'Сайты видят вас из',
  'leak.exit.vpn': 'Адрес VPN-сервера для сайтов',
  'leak.home': 'Ваш адрес без VPN',
  'leak.dns.servers': 'DNS-серверы',
  'leak.tunnel_answers': 'Отвечает сам туннель, наружу запросы не уходят',
  'leak.dns.vpn_side': 'DNS на стороне VPN-сервера',
  'leak.country_note':
      'Страна — по базе сервиса проверки, у разных баз может отличаться; '
      'у DNS вроде AdGuard и Cloudflare серверы по всему миру, отвечает ближайший к VPN-серверу',
  'leak.apps_failed': 'Запросы приложений через туннель проверить не удалось.',
  'leak.vote': '{place} — {votes} из {total} баз',
  'leak.vote.tie': '{place} — {votes} из {total} баз (поровну)',
  'leak.geo.more': 'Подробнее по базам',
  'leak.geo.no_answer': 'не ответил',

  // The DNS fields of the settings (ui/pages/dns_check.dart, state/app_state/dns_check.dart).
  'dnscheck.hint':
      'Свой DNS с блокировкой рекламы (например, AdGuard) лучше указывать в поле «через VPN»: туда провайдер не дотянется. '
      'В поле «напрямую» его может заблокировать провайдер или белый список.',
  'dnscheck.not_yet': 'ещё не проверен',
  'dnscheck.running': 'проверяю…',
  'dnscheck.check': 'Проверить',
  'dnscheck.again': 'Проверить ещё раз',
  'dnscheck.name.direct': 'DNS напрямую',
  'dnscheck.name.remote': 'DNS через VPN',
  'dnscheck.failed': 'не удалось проверить: {error}',
  'dnscheck.log.net': 'сеть',
};

const routingEn = <String, String>{
  // The rules page (ui/pages/routing_page.dart).
  'rules.page.title': 'Rules',
  'rules.page.sub.selected': 'Only the selected sites and apps go through the VPN, everything else goes direct.',
  'rules.page.sub.all': 'Sites and apps that work direct, outside the VPN. Everything else goes through the VPN.',
  'rules.always_direct.tip': 'What always works direct',
  'rules.always_direct.title': 'Always direct',
  'rules.always_direct.selected': 'The local network and Russian sites already work direct in this mode.',
  'rules.always_direct.all': 'The local network (10.0.0.0/8, 192.168.0.0/16, .local, .lan) and the VPN server address always work direct.',
  'rules.sub.domains': 'a site and all its subdomains',
  'rules.add': 'Add',
  'rules.list.empty': 'Empty for now',
  'rules.list.remove': 'Remove from the list',
  'rules.proxy.title': 'Sites and addresses through VPN',
  'rules.proxy.desc': 'Only these go through the VPN. Everything else opens direct, as without a VPN.',
  'rules.proxy.empty': 'Empty for now — everything goes direct',
  'rules.proxy.remove': 'No longer through VPN',
  'rules.proxy.apps.desc': 'All traffic of these apps goes through the VPN, wherever they connect. Handy for messengers and games that are blocked entirely.',
  'rules.direct.title': 'Sites and addresses without VPN',
  'rules.direct.desc': 'You can enter an IP address or a subnet, for example 10.8.0.0/16 for a work network.',
  'rules.direct.empty': 'Empty for now — everything goes through the VPN',
  'rules.direct.remove': 'Through VPN again',
  'rules.always.title': 'Always through VPN',
  'rules.always.sub': 'overrides exceptions',
  'rules.always.desc': 'For sites that fall under the exceptions but do not open without a VPN: for example, a blocked site in the .ru zone.',
  'rules.notice.title': 'Proxy-only mode is on.',
  'rules.notice.text': 'In it only apps set up for the proxy go through the VPN, and the rules below do not apply.',
  'rules.notice.btn': 'All apps',
  'rules.mode.all.title': 'Everything through VPN',
  'rules.mode.selected.title': 'Only selected',
  'rules.mode.all.text': 'Except for exceptions. Hides all your traffic; Russian sites can go direct.',
  'rules.mode.selected.text': 'Only the selected services go through the VPN, the rest goes direct. Banks and Russian sites work as without a VPN.',
  'rules.presets.title': 'Ready-made sets',
  'rules.presets.russia.title': 'Russian sites direct',
  'rules.presets.russia.desc':
      'Gosuslugi, banks, Yandex, VK and other Russian services open without a VPN — '
      'faster and without “from abroad” blocks. Google and YouTube always go through the VPN, as do sites blocked in Russia, even on .ru. '
      'The lists update themselves.',
  'rules.presets.abroad.title': 'Russian sites on foreign servers — through VPN',
  'rules.presets.abroad.desc':
      'A .ru site behind Cloudflare or on foreign hosting goes through the VPN: direct, such sites are often slow. '
      'If a service does not let you in from abroad, add it to “Sites and addresses without VPN”.',
  'rules.geo.fold': 'Database source',
  'rules.block.title': 'Block',
  'rules.block.sub': 'do not open at all',
  'rules.block.desc': 'Sites in this list will not open, neither through the VPN nor direct: ads, trackers, distracting sites.',
  'rules.block.remove': 'Unblock',

  // Popular services (ui/pages/rule_lists.dart).
  'rules.list.toast.added': 'Added: {n}',
  'rules.svc.title': 'Popular services',
  'rules.svc.sub': 'in one click',
  'rules.svc.addresses': '+ server addresses',
  'rules.svc.instagram_facebook': 'Instagram and Facebook',
  'rules.svc.toast.off': '{name} no longer goes through the VPN',
  'rules.svc.toast.on': '{name} now goes through the VPN',

  // The user's own rules (ui/pages/user_rules.dart).
  'rules.own.title': 'Your rules',
  'rules.own.sub': 'top to bottom',
  'rules.own.info':
      'Rules are checked in order: the first match decides. They take precedence over the ready-made sets, '
      'but your “without VPN”, “through VPN” and “Block” site lists take precedence over the rules. '
      'geoip rules apply to sites that no name rule matches.',
  'rules.own.desc':
      'A category of sites or addresses, a site or a subnet. For example: geosite:category-ads-all — block, '
      'geosite:youtube — through VPN, geoip:ru — direct.',
  'rules.own.empty': 'No rules of your own yet',
  'rules.own.toast.changed': 'Rule changed',
  'rules.own.toast.added': 'Rule added',
  'rules.own.up': 'Up',
  'rules.own.down': 'Down',
  'rules.own.delete': 'Delete rule',
  'rules.geo.err': 'The database failed to load: the category cannot be checked.',
  'rules.geo.idle': 'The database is not loaded yet.',
  'rules.geo.loading': 'The database is still loading…',
  'rules.geo.loading_pct': 'The database is still loading… {pct}%',
  'rules.geo.found': 'Found {where}.',
  'rules.geo.resolved': 'Will be used:',
  'rules.geo.ambiguous': 'Several categories fit, pick one:',
  'rules.geo.typo': 'Not in the database. Maybe',
  'rules.geo.missing': 'No such category {where}.',
  'rules.note.waiting': 'waiting for database',
  'rules.note.missing': 'not in database',
  'rules.note.maybe': 'Maybe:',
  'rules.note.ambiguous': 'ambiguous',
  'rules.note.choose': 'Pick one:',
  'rules.note.error': 'database failed to load',
  'rules.geo.retry': 'Retry',
  'rules.geo.load': 'Load',
  'rules.geo.categories': '{n} {n|category|categories}',
  'rules.geo.sub.loading': '{name} · loading…',
  'rules.geo.sub.error': '{name} · load error',
  'rules.geo.src.runetfreedom':
      'runetfreedom lists (russia-blocked-geosite): ru-blocked — sites blocked in Russia, category-ads-all — ads, '
      'and the v2fly categories. It is one geosite.dat file of tens of megabytes; the geoip categories come from SagerNet.',
  'rules.geo.src.custom':
      'An https link with {name} for .srs files, for example https://example.org/geosite-{name}.srs, '
      'or a link to geosite.dat / geoip.dat in the v2ray format. With an empty field, these categories come from SagerNet.',
  'rules.geo.src.sagernet': 'SagerNet categories (sing-geosite, sing-geoip): geosite:youtube, geosite:category-ads-all, geoip:ru and others.',
  'rules.geo.src.intro': 'Where the geosite: and geoip: categories for your rules come from.',
  'rules.geo.src.geosite_url': 'Site categories (geosite)',
  'rules.geo.src.geoip_url': 'Address categories (geoip)',
  'rules.geo.src.presets.title': 'Ready-made sets come from here too',
  'rules.geo.src.presets.desc':
      'The “Russian sites direct” and ad-blocking lists will be taken from this source. '
      'If the category you need is not there, the built-in copy is used.',
  'rules.geo.src.third_party': 'The lists are maintained by a third party: CoreShift only checks their format and size.',

  // The DNS leak check (ui/pages/leak_check.dart).
  'leak.title': 'DNS leak check',
  'leak.desc.ready': 'Find out whether your provider can see your DNS queries. The check goes through the tunnel, like your apps. It uses the bash.ws service; the country of the VPN server address (not yours) is also checked against several other services: ipwho.is, ipinfo.io and others',
  'leak.desc.not_ready': 'Connect in “All apps” mode to run the check',
  'leak.btn': 'Check DNS',
  'leak.failed': 'The check failed',
  'leak.advice.android':
      'What to do: remove the “Private DNS” set manually in Android settings, turn on “DNS leak protection” above, '
      'reconnect and check again.',
  'leak.advice.other':
      'What to do: turn on “DNS leak protection” above, reconnect and check again. '
      'If that does not help, check whether bash.ws goes direct under your rules.',
  'leak.own': 'your DNS',
  'leak.dns.apps': 'DNS used by apps (through VPN){provider}',
  'leak.ok.title': 'No leak',
  'leak.ok.own': 'DNS goes through the VPN to the DNS you chose{place}. Your provider cannot see these queries.',
  'leak.ok.fakeip': 'The tunnel itself gives apps the site addresses, and the VPN server resolves the names. Your provider cannot see your DNS queries.',
  'leak.ok.vpn': 'The apps\' DNS queries go through the VPN to servers on its side. Your provider cannot see them.',
  'leak.leak.title': 'DNS leak found',
  'leak.leak.text': 'Some DNS queries are handled by your provider\'s or network\'s DNS: it sees which sites you open.',
  'leak.bypass.title': 'The check went around the VPN',
  'leak.bypass.text':
      'bash.ws saw your real address, so the result is unreliable. The server seems to let this site go direct. '
      'Pick another server or reconnect and check again.',
  'leak.unknown.title': 'Result is inconclusive',
  'leak.unknown.no_exit': 'bash.ws did not say where the check came from. Try again.',
  'leak.unknown.no_home': 'Could not find out your address without the VPN, so it is unclear whether the check went through the VPN. Try again.',
  'leak.exit.seen': 'Sites see you from',
  'leak.exit.vpn': 'VPN server address as sites see it',
  'leak.home': 'Your address without VPN',
  'leak.dns.servers': 'DNS servers',
  'leak.tunnel_answers': 'The tunnel itself answers, no queries leave it',
  'leak.dns.vpn_side': 'DNS on the VPN server side',
  'leak.country_note':
      'The country comes from the check service\'s database and may differ between databases; '
      'DNS like AdGuard and Cloudflare have servers worldwide, the one closest to the VPN server answers',
  'leak.apps_failed': 'Could not check the apps\' queries through the tunnel.',
  'leak.vote': '{place} — {votes} of {total} databases',
  'leak.vote.tie': '{place} — {votes} of {total} databases (tie)',
  'leak.geo.more': 'Details by database',
  'leak.geo.no_answer': 'no answer',

  // The DNS fields of the settings (ui/pages/dns_check.dart, state/app_state/dns_check.dart).
  'dnscheck.hint':
      'A custom DNS with ad blocking (for example, AdGuard) is best set in the “through VPN” field: your provider cannot reach it there. '
      'In the “direct” field your provider or a whitelist may block it.',
  'dnscheck.not_yet': 'not checked yet',
  'dnscheck.running': 'checking…',
  'dnscheck.check': 'Check',
  'dnscheck.again': 'Check again',
  'dnscheck.name.direct': 'Direct DNS',
  'dnscheck.name.remote': 'DNS through VPN',
  'dnscheck.failed': 'could not check: {error}',
  'dnscheck.log.net': 'network',
};
