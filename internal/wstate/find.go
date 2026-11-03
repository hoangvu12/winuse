package wstate

// find's matching (desktop-parity ticket 04, agent-desktop's `find`): the
// filters an agent narrows a dense Window's Elements with, the roles a
// Window shows (agent-desktop's roles_present, for a role that matched
// nothing), and what counts as interactive for a region read. Pure, so
// the offline tests run it over recorded trees.

import (
	"fmt"
	"sort"
	"strings"
)

// Query is find's filters; an empty field matches every Element. Role is
// the tree's role, compared ignoring case and spacing; Name is a part of
// the Element's name, Text a part of its name, Value or description, both
// ignoring case; State is one of States.
type Query struct {
	Role  string
	Name  string
	Text  string
	State string
}

// States are the state filters find takes, as the tree prints them, plus
// "enabled" and "interactive".
var States = []string{"disabled", "enabled", "selected", "selectable", "expanded", "collapsed", "settable", "on", "off", "indeterminate", "interactive"}

// ValidState reports whether s is one of States (ignoring case).
func ValidState(s string) bool {
	s = strings.ToLower(Collapse(s))
	for _, st := range States {
		if st == s {
			return true
		}
	}
	return false
}

// Match reports whether an Element passes every filter of the query.
func (q Query) Match(e Element) bool {
	if r := fold(q.Role); r != "" && fold(e.Role) != r {
		return false
	}
	if n := fold(q.Name); n != "" && !strings.Contains(fold(e.Name), n) {
		return false
	}
	if t := fold(q.Text); t != "" && !strings.Contains(fold(e.Name), t) && !strings.Contains(fold(e.Value), t) && !strings.Contains(fold(e.Description), t) {
		return false
	}
	if s := fold(q.State); s != "" && !e.hasState(s) {
		return false
	}
	return true
}

// String is the query as find's messages name it: role "button", name
// "Save", ...
func (q Query) String() string {
	var parts []string
	for _, f := range []struct{ word, v string }{{"role", q.Role}, {"name", q.Name}, {"text", q.Text}, {"state", q.State}} {
		if v := Collapse(f.v); v != "" {
			parts = append(parts, fmt.Sprintf("%s %q", f.word, v))
		}
	}
	if len(parts) == 0 {
		return "no filter"
	}
	return strings.Join(parts, ", ")
}

// fold lower-cases and whitespace-collapses s for matching.
func fold(s string) string {
	return strings.ToLower(Collapse(s))
}

// hasState reports whether the Element shows one of States.
func (e Element) hasState(s string) bool {
	switch s {
	case "disabled":
		return e.Disabled
	case "enabled":
		return !e.Disabled
	case "selected":
		return e.Selected
	case "selectable":
		return e.Selectable
	case "expanded":
		return e.ExpandState == "expanded" || e.ExpandState == "partially expanded"
	case "collapsed":
		return e.ExpandState == "collapsed"
	case "settable":
		return e.Settable != ""
	case "on", "off", "indeterminate":
		return e.Toggle == s
	case "interactive":
		return e.Interactive()
	}
	return false
}

// interactiveRoles are the roles an agent acts on even when the Element
// shows no pattern state of its own.
var interactiveRoles = map[string]bool{
	"button": true, "check box": true, "combo box": true, "edit": true, "link": true,
	"list item": true, "menu item": true, "radio button": true, "slider": true,
	"spinner": true, "tab": true, "tree item": true, "split button": true,
	"data item": true, "header item": true,
}

// Interactive reports whether an Element is one an agent can act on: it is
// enabled, and it has a role it is acted on through, a value it can be
// set to, a toggle, a selection or expansion state, or a Secondary Action
// besides Raise.
func (e Element) Interactive() bool {
	if e.Disabled {
		return false
	}
	if interactiveRoles[e.Role] || e.Settable != "" || e.Toggle != "" || e.Selectable || e.ExpandState != "" {
		return true
	}
	for _, a := range e.SecondaryActions {
		if a != "Raise" {
			return true
		}
	}
	return false
}

// RolesPresent are the distinct roles of the Elements, sorted: what a role
// filter could have named.
func RolesPresent(elements []Element) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range elements {
		if r := Collapse(e.Role); r != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}
