/// What Android answered when asked for the VPN (VpnService.prepare, see
/// MainActivity.kt). [unasked]: refused at once, without its request on the
/// screen, as when another app is the "always-on" VPN; [denied]: the user
/// declined the request, or it was not shown long enough to tell.
enum VpnConsent { granted, denied, unasked }
