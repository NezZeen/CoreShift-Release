package service

import (
	"strings"
	"sync"

	"coreshift/engine/internal/msg"
	"coreshift/engine/internal/store"
)

// announceLog remembers the announcement last journaled for each
// subscription, so that a refresh bringing the same text says nothing.
type announceLog struct {
	mu   sync.Mutex
	last map[string]string
}

// announceExcerpt is how much of an announcement the journal line carries.
const announceExcerpt = 80

// noteAnnouncement journals a provider's new announcement: one line, with
// the subscription's name and the start of the text, never its link.
func (s *Service) noteAnnouncement(c store.Change) {
	if s.cfg.Store == nil {
		return
	}
	a := &s.announced
	switch c.What {
	case "subscription-removed":
		a.mu.Lock()
		delete(a.last, c.ID)
		a.mu.Unlock()
		return
	case "subscription-added", "subscription-updated":
	default:
		return
	}
	sub, ok := s.cfg.Store.Subscription(c.ID)
	if !ok {
		return
	}
	text := sub.Info.Announce
	a.mu.Lock()
	if a.last == nil {
		a.last = map[string]string{}
	}
	prev := a.last[c.ID]
	a.last[c.ID] = text
	a.mu.Unlock()
	if text == "" || text == prev {
		return
	}
	s.LogActionMsg("подписка", announceLine(announceName(sub), text))
}

// announceName is the subscription's name as the app shows it.
func announceName(sub store.Subscription) msg.Msg {
	switch n := sub.DisplayName(); n {
	case "Local nodes":
		return msg.New("sub.name.local")
	case "Subscription":
		return msg.New("sub.name.default")
	default:
		return msg.Raw(n)
	}
}

// announceLine is the journal's line: the provider's text, said as it is.
func announceLine(name msg.Msg, text string) msg.Msg {
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > announceExcerpt {
		text = strings.TrimSpace(string(r[:announceExcerpt])) + "…"
	}
	return msg.New("announce.line", "name", name, "text", text)
}
