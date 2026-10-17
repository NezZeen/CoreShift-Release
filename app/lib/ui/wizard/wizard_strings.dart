import '../../l10n/strings.dart';

/// Every text of the first-run wizard in one place; the words are in the
/// dictionary (lib/l10n/parts/shell.dart, keys "wizard.…"). They are getters,
/// said in the language of the moment. Texts with a number or a name in them
/// are functions.
abstract final class WizardStrings {
  // Common.
  static String get skip => tr('wizard.skip');
  static String get back => tr('wizard.back');
  static String get next => tr('wizard.next');
  static String stepOf(int step, int total) => tr('wizard.stepOf', {'step': step, 'total': total});

  // 1. Welcome.
  static String get welcomeTitle => tr('wizard.welcome.title');
  static String get welcomeLine1 => tr('wizard.welcome.line1');
  static String get welcomeLine2 => tr('wizard.welcome.line2');
  static String get welcomeStart => tr('wizard.welcome.start');

  // 2. Subscription.
  static String get addTitle => tr('wizard.add.title');
  static String get addHint => tr('wizard.add.hint');
  static String get addFieldHint => tr('wizard.add.fieldHint');
  static String get addPaste => tr('wizard.add.paste');
  static String get addClear => tr('wizard.add.clear');
  static String get addSubmit => tr('wizard.add.submit');
  static String get addManual => tr('wizard.add.manual');
  static String get addFromClipboard => tr('wizard.add.fromClipboard');
  static String get addProgress => tr('wizard.add.progress');
  static String get addEmpty => tr('wizard.add.empty');
  static String get addClipboardEmpty => tr('wizard.add.clipboardEmpty');
  static String get addSubscriptionLabel => tr('wizard.add.subscriptionLabel');
  static String get addServersLabel => tr('wizard.add.serversLabel');
  static String addServersInText(int n) => tr('wizard.add.serversInText', {'n': n});

  // 3. Servers.
  static String get checkTitle => tr('wizard.check.title');
  static String get checkHintDone => tr('wizard.check.hintDone');
  static String get checkHintBusy => tr('wizard.check.hintBusy');
  static String get checkAuto => tr('wizard.check.auto');
  static String get checkAutoText => tr('wizard.check.autoText');
  static String get checkBest => tr('wizard.check.best');
  static String get checkAgain => tr('wizard.check.again');
  static String get checkNoneAnswered => tr('wizard.check.noneAnswered');
  static String get checkNoServers => tr('wizard.check.noServers');
  static String checkProgress(int done, int total) => total > 0 ? tr('wizard.check.progress', {'done': done, 'total': total}) : tr('wizard.check.progressNone');
  static String checkMs(int ms) => tr('wizard.check.ms', {'ms': ms});
  static String checkMore(int n) => tr('wizard.check.more', {'n': n});

  // 4. Mode.
  static String get modeTitle => tr('wizard.mode.title');
  static String get modeHint => tr('wizard.mode.hint');
  static String get modeRecommended => tr('wizard.mode.recommended');
  static String get modeAllTitle => tr('wizard.mode.all.title');
  static String get modeAllText => tr('wizard.mode.all.text');
  static String get modeProxyTitle => tr('wizard.mode.proxy.title');
  static String get modeProxyText => tr('wizard.mode.proxy.text');
  static String get modeSystemProxyTitle => tr('wizard.mode.systemProxy.title');
  static String get modeSystemProxyText => tr('wizard.mode.systemProxy.text');
  static String get modeProxyOnlyTitle => tr('wizard.mode.proxyOnly.title');
  static String get modeProxyOnlyText => tr('wizard.mode.proxyOnly.text');
  static String get modeNeedsUpdate => tr('wizard.mode.needsUpdate');
  static String get modeVpnTitle => tr('wizard.mode.vpn.title');
  static String get modeVpnText => tr('wizard.mode.vpn.text');
  static String get modeUnavailable => tr('wizard.mode.unavailable');
  static String get modeFailed => tr('wizard.mode.failed');

  // 5. Done.
  static String get doneTitle => tr('wizard.done.title');
  static String get doneHint => tr('wizard.done.hint');
  static String get doneServer => tr('wizard.done.server');
  static String get doneMode => tr('wizard.done.mode');
  static String get doneConnect => tr('wizard.done.connect');
  static String get doneLater => tr('wizard.done.later');
}
