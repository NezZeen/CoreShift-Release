// The web preview sets no system proxy.

import '../l10n/strings.dart';

/// Whether this system can have its proxy set by CoreShift.
bool get systemProxySupported => false;

/// Runs `coreshiftd sysproxy <args>` and returns its report as JSON.
Future<Map<String, dynamic>> runSystemProxy(List<String> args) async => {
  'action': args.isEmpty ? '' : args.first,
  'errors': [tr('proxy.err.web')],
};

/// Whether a journal of settings to put back is there.
bool systemProxyJournalExists() => false;
