package store

import (
	"slices"
	"strings"
)

// Fingerprints returns the fingerprints of sub's nodes, in their order.
// They are computed once per node list: computing one encodes the node.
func (sub *Subscription) Fingerprints() []string {
	if len(sub.Nodes) == 0 {
		return nil
	}
	if sub.fpOf != &sub.Nodes[0] || len(sub.fps) != len(sub.Nodes) {
		fps := make([]string, len(sub.Nodes))
		for i := range sub.Nodes {
			fps[i] = sub.Nodes[i].Fingerprint()
		}
		sub.fps, sub.fpOf = fps, &sub.Nodes[0]
	}
	return sub.fps
}

// IsHidden reports whether the user removed the server with fingerprint fp
// from the list.
func (sub *Subscription) IsHidden(fp string) bool { return slices.Contains(sub.Hidden, fp) }

// Insecure reports a subscription fetched over plain HTTP: its link, with
// the access token, crosses the network unencrypted.
func (sub *Subscription) Insecure() bool { return InsecureURL(sub.URL) }

// InsecureURL reports a plain HTTP subscription URL.
func InsecureURL(u string) bool {
	return len(u) >= 7 && strings.EqualFold(u[:7], "http://")
}

// DisplayName is the user's label, else the panel's title, else a
// placeholder.
func (s *Subscription) DisplayName() string {
	switch {
	case s.Name != "":
		return s.Name
	case s.Info.Title != "":
		return s.Info.Title
	case s.URL == "":
		return "Local nodes"
	}
	return "Subscription"
}
