package msg

// en has the English of what the engine itself shows the user, without the
// app's dictionary: Android's notifications about a subscription running
// out, posted while the app is closed (mobile/subwarn.go). Everything else
// the app words itself (app/lib/l10n/engine_strings.dart); a code missing
// here is said in Russian. Keep the texts the same as the app's English,
// so that a notification and the app's banner are one.
var en = map[string]string{
	"sub.name.local":         "My servers",
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
}
