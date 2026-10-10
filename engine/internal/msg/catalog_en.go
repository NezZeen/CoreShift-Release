package msg

// en has the English of what the engine itself shows the user, without the
// app's dictionary: Android's notifications about a subscription running
// out, posted while the app is closed (mobile/subwarn.go). Everything else
// the app words itself (app/lib/l10n/engine_strings.dart); a code missing
// here is said in Russian. Keep the texts the same as the app's English,
// so that a notification and the app's banner are one.
var en = map[string]string{
	"sub.name.local":         "My servers",
	"speed.nodata":           "nothing was downloaded through the server: {err}",
	"sub.name.default":       "Subscription",
	"sub.expired.title":      "Subscription “{name}” has ended",
	"sub.expired.body":       "Renew it with your provider: the servers do not work without it.",
	"sub.today.title":        "Subscription “{name}” ends today",
	"sub.tomorrow.title":     "Subscription “{name}” ends tomorrow",
	"sub.day.body":           "Renew it with your provider so the VPN does not stop.",
	"sub.days.title":         "Subscription “{name}” ends in {days} {days|day|days}",
	"sub.days.body":          "Renew it with your provider in advance.",
	"sub.traffic_over.title": "Subscription “{name}” is out of traffic",
	"sub.traffic_over.body":  "Buy more traffic from your provider or wait for it to renew.",
	"sub.traffic_low.title":  "Subscription “{name}” is almost out of traffic",
	"sub.traffic_low.body":   "{left} of {total} left.",
	"unit.tb":                "{n} TB",
	"unit.gb":                "{n} GB",
	"unit.mb":                "{n} MB",
	"unit.kb":                "{n} KB",
	// The direct resolver (dnsfallback.go, dnscheck.go).
	"dns.direct_down":             "direct DNS ({addr}) is unreachable on this network ({why}) — using the network's DNS",
	"dns.direct_restored":         "direct DNS ({addr}) answers again",
	"dns.why.refused":             "connection refused",
	"dns.why.timeout":             "no answer",
	"dns.why.tls":                 "could not set up a secure connection",
	"dns.why.reset":               "the connection was cut",
	"dns.why.unreachable":         "the network cannot reach this address",
	"dns.why.rcode":               "answered with the error {code}",
	"dns.why.http":                "the web server answered with the error {code}",
	"dnscheck.ok":                 "answers ({ms} ms)",
	"dnscheck.fail":               "does not answer on this network: {why}",
	"dnscheck.blocked":            "does not answer on this network: {why} — the provider seems to block this DNS",
	"dnscheck.remote_fail":        "does not answer through the VPN: {why}",
	"dnscheck.skip.not_connected": "will be checked through the VPN once it is connected",
	"dnscheck.skip.unsupported":   "this kind of DNS (QUIC, HTTP/3) is not checked",
	"dnscheck.skip.no_physical":   "could not reach the network around the VPN to check",
}
