package store

import (
	"fmt"

	"coreshift/engine/internal/node"
)

// Select makes node fingerprint of subscription subID the one to connect.
// Select selects a node. name tells apart nodes that differ only in name,
// which share a fingerprint; it may be empty.
func (s *Store) Select(subID, fingerprint, name string) (Selection, error) {
	var sel Selection
	err := s.modify(Change{What: "selection", ID: subID}, func(d *fileData) error {
		i := s.index(subID)
		if i < 0 {
			return ErrNotFound
		}
		n, ok := findNode(&d.Subscriptions[i], fingerprint, name)
		if !ok || n.Fingerprint() != fingerprint {
			return fmt.Errorf("subscription has no node %s", fingerprint)
		}
		sel = Selection{Subscription: subID, Fingerprint: fingerprint, Name: n.Name}
		d.Selection = &sel
		return nil
	})
	return sel, err
}

// Selected returns the selection and its node. ok is false when nothing is
// selected or the node has disappeared from its subscription; the selection
// is still returned in the latter case so the UI can say which one.
func (s *Store) Selected() (sel Selection, n node.Node, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Selection == nil {
		return Selection{}, node.Node{}, false
	}
	sel = *s.data.Selection
	if i := s.index(sel.Subscription); i >= 0 {
		n, ok = findNode(&s.data.Subscriptions[i], sel.Fingerprint, sel.Name)
	}
	return sel, n, ok
}

// repointSelection follows the selected node through a refresh: same
// fingerprint, else the only node with the same name.
func repointSelection(d *fileData, sub *Subscription) {
	sel := d.Selection
	if sel == nil || sel.Subscription != sub.ID {
		return
	}
	if n, ok := findNode(sub, sel.Fingerprint, sel.Name); ok {
		sel.Fingerprint, sel.Name = n.Fingerprint(), n.Name
	}
}

// findNode finds a node by fingerprint, preferring the one named name when
// several share it, else by name alone if that is unambiguous.
func findNode(sub *Subscription, fingerprint, name string) (node.Node, bool) {
	nodes, fps := sub.Nodes, sub.Fingerprints()
	var byFP *node.Node
	for i := range nodes {
		if fps[i] != fingerprint {
			continue
		}
		if nodes[i].Name == name {
			return nodes[i], true
		}
		if byFP == nil {
			byFP = &nodes[i]
		}
	}
	if byFP != nil {
		return *byFP, true
	}
	if name == "" {
		return node.Node{}, false
	}
	var match *node.Node
	for i := range nodes {
		if nodes[i].Name == name {
			if match != nil {
				return node.Node{}, false // ambiguous
			}
			match = &nodes[i]
		}
	}
	if match == nil {
		return node.Node{}, false
	}
	return *match, true
}
