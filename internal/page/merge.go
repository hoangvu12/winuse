package page

import (
	"strings"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// The caps of ADR 0004, shared by the frame and the Page (the uia walk's
// DefaultMaxNodes, DefaultMaxDepth and DefaultMaxChildren; uia is not
// imported here so this package stays pure).
const (
	MaxNodes    = 3000
	MaxDepth    = 48
	MaxChildren = 1000
)

// partialPrefix is the walk's partial-tree note prefix (uia's
// partialNotePrefix): the Page's cut reasons join the frame's note.
const partialPrefix = "this Window State is partial:"

// Merged is a browser Window's one Window State: the UIA frame with the
// descendants of its document Element replaced by the Page, and the stale
// Tabs' documents left out (DocRule).
type Merged struct {
	Tree *wstate.Tree
	// Doc is the Element Index of the UIA document Element in Tree, kept as
	// is; the Page's Elements take the indexes Doc+1 to Doc+len(Origins).
	// End is the frame index just past the document's UIA descendants.
	Doc, End int
	// From is the frame index behind each Element of Tree, or -1 for a
	// Page Element: what the live-Element store keeps (uia SplicePage).
	From []int
	// Origins are the Page Elements' origins, in index order.
	Origins []Origin
}

// Merge splices a Raw read into the frame read through UI Automation. The
// document Element is the one DocRule picks; there must be one. Its own
// line stays (it is the UIA Element ticket 05's cursor transform starts
// from); its UIA descendants are replaced by the Page's Elements, which
// share the frame's caps: together they never pass the element cap, and
// the depth cap counts from the window. A stale Tab's document is left
// out with its descendants. The focused Element is the Page's when the
// frame's focus is in the document, in a stale document or nowhere. The
// frame's notes stay, and the Page's cut reasons join its partial-tree
// note.
//
// frame is not modified.
func Merge(frame *wstate.Tree, raw *Raw) (*Merged, error) {
	doc, stale, err := findDocument(frame.Elements, RuleFor(raw))
	if err != nil {
		return nil, err
	}
	end := subtreeEnd(frame.Elements, doc)
	// The stale Tabs' documents go with their descendants. One under the
	// document is replaced with the rest of it.
	gone := make([]bool, len(frame.Elements))
	dropped := 0
	for _, r := range stale {
		if r[0] > doc && r[0] < end {
			continue
		}
		for i := r[0]; i < r[1]; i++ {
			gone[i] = true
		}
		dropped += r[1] - r[0]
	}
	docDepth := frame.Elements[doc].Depth
	outside := len(frame.Elements) - (end - doc - 1) - dropped
	lim := Limits{
		MaxNodes:    MaxNodes - outside,
		MaxDepth:    MaxDepth - (docDepth + 1),
		MaxChildren: MaxChildren,
		NodeLabel:   MaxNodes,
		DepthLabel:  MaxDepth,
	}
	if lim.MaxNodes < 1 || lim.MaxDepth < 0 {
		return nil, &Unavailable{Reason: "the browser frame alone fills the Window State's caps"}
	}
	res := Build(raw, lim)

	els := make([]wstate.Element, 0, outside+len(res.Elements))
	from := make([]int, 0, cap(els))
	pos := make([]int, len(frame.Elements)) // each frame Element's new index, or -1
	at := -1                                // the document's new index
	for i := range frame.Elements {
		pos[i] = -1
		if gone[i] || (i > doc && i < end) {
			continue
		}
		pos[i] = len(els)
		e := frame.Elements[i]
		if i == doc {
			e.Truncated = res.DocTruncated
			at = len(els)
		}
		els = append(els, e)
		from = append(from, i)
		if i == doc {
			for _, pe := range res.Elements {
				pe.Depth += docDepth + 1
				els = append(els, pe)
				from = append(from, -1)
			}
		}
	}

	tree := *frame
	tree.Elements = els
	tree.Notes = mergeNotes(frame.Notes, res.Reasons)
	tree.FocusedIndex = -1
	f := frame.FocusedIndex
	inDoc := f >= doc && f < end
	switch {
	case f >= 0 && f < len(pos) && !inDoc && pos[f] >= 0:
		// The focus is in the frame (the address bar, a menu): keep it.
		tree.FocusedIndex = pos[f]
	case res.Focused >= 0:
		tree.FocusedIndex = at + 1 + res.Focused
	case inDoc || res.RootFocused:
		tree.FocusedIndex = at
	}

	origins := make([]Origin, len(res.Origins))
	for i, o := range res.Origins {
		o.DocIndex = at
		origins[i] = o
	}
	return &Merged{Tree: &tree, Doc: at, End: end, From: from, Origins: origins}, nil
}

// NamedAfter reports whether a document Element's name is the Tab's
// title: the name half of DocRule.
func NamedAfter(name, tabTitle string) bool {
	t := Collapse(tabTitle)
	return t != "" && Collapse(name) == t
}

// DocRule picks the UIA document Element that shows the active Tab among
// a browser frame's documents. Merge and the UI Automation walk's cut
// (uia.DocumentCut) share it, so both pick the same one.
//
// UI Automation gives a document its URL as its Value. A document whose
// Value is the URL of another of the browser's Tabs, and not the active
// Tab's, is a stale Tab's document: after a Headless tab switch through
// the extension, Chrome keeps the previous Tab's document in the tree,
// moved to the window's top, and shows the active Tab's place as an empty
// document with no name. A stale document is never picked, and Merge
// leaves it out with its descendants. A Value that is no Tab's URL proves
// nothing (an iframe, a side panel, a URL history.pushState changed), so
// that document stays a candidate.
//
// The pick: the first document, in index order, named after the Tab; else
// the only candidate (the empty placeholder included). A document under a
// stale one is no candidate. Several candidates none named after the Tab
// stay unpicked: a Value equal to the Tab's URL cannot break that tie,
// since two Tabs can show one URL, and that tie is exactly a stale
// document of such a Tab next to the placeholder.
type DocRule struct {
	title  string
	others map[string]bool // the other Tabs' URLs (pageURL), the Tab's own left out
}

// RuleFor is the document rule for a Page read. The zero DocRule picks no
// document by name and calls none stale.
func RuleFor(raw *Raw) DocRule {
	r := DocRule{title: raw.Title}
	own := pageURL(raw.URL)
	for _, u := range raw.Others {
		if u = pageURL(u); u != "" && u != own {
			if r.others == nil {
				r.others = map[string]bool{}
			}
			r.others[u] = true
		}
	}
	return r
}

// Stale reports whether a document whose Value is value shows another Tab.
func (r DocRule) Stale(value string) bool {
	return value != "" && r.others[pageURL(value)]
}

// Named reports whether a document is picked by its name: named after the
// Tab and not stale. The first such document is the one; the walk leaves
// out its descendants.
func (r DocRule) Named(name, value string) bool {
	return NamedAfter(name, r.title) && !r.Stale(value)
}

// pageURL is a URL without its fragment: a fragment change stays on the
// same page.
func pageURL(u string) string {
	u, _, _ = strings.Cut(u, "#")
	return u
}

// findDocument picks the frame's document Element for the active Tab
// (DocRule), and lists the stale Tabs' documents as index ranges with
// their descendants.
func findDocument(els []wstate.Element, rule DocRule) (doc int, stale [][2]int, err error) {
	var docs []int
	for i := 0; i < len(els); i++ {
		if els[i].Role != "document" {
			continue
		}
		if rule.Stale(els[i].Value) {
			end := subtreeEnd(els, i)
			stale = append(stale, [2]int{i, end})
			i = end - 1
			continue
		}
		docs = append(docs, i)
	}
	for _, i := range docs {
		if NamedAfter(els[i].Name, rule.title) {
			return i, stale, nil
		}
	}
	switch len(docs) {
	case 0:
		return 0, nil, &Unavailable{Reason: "UI Automation shows no document for the active Tab"}
	case 1:
		return docs[0], stale, nil
	}
	return 0, nil, &Unavailable{Reason: "UI Automation shows several documents and none is named after the active Tab"}
}

// subtreeEnd is the index just past the descendants of the Element at i.
func subtreeEnd(els []wstate.Element, i int) int {
	end := i + 1
	for end < len(els) && els[end].Depth > els[i].Depth {
		end++
	}
	return end
}

// mergeNotes adds the Page's cut reasons to the frame's partial-tree note,
// or as a new one.
func mergeNotes(notes []string, reasons []string) []string {
	out := append([]string(nil), notes...)
	if len(reasons) == 0 {
		return out
	}
	for i, n := range out {
		if strings.HasPrefix(n, partialPrefix) {
			for _, r := range reasons {
				if !strings.Contains(n, r) {
					n += "; " + r
				}
			}
			out[i] = n
			return out
		}
	}
	return append(out, partialPrefix+" "+strings.Join(reasons, "; "))
}
