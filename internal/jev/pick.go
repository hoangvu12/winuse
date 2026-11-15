package jev

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Element is one line of a Window State: its Element Index and the rest
// of its line ("button Plus ID: plusButton").
type Element struct {
	Index int
	Line  string
	Depth int // its indent in the tree (the root's children are 2)
	// Offscreen: a Page read found it scrolled out of the viewport (its
	// line carried wstate.OffscreenMark, which ParseState takes off).
	Offscreen bool
}

// offscreenMark is wstate.OffscreenMark and its space, as a line carries it.
const offscreenMark = "[off screen] "

// Pick's tuning, measured on .scratch/jev/eval.py (ADR 0023): options
// described by their Element line, 200 to a question (a choice takes at
// most 255), and a pick acted on only at p >= 0.5.
const (
	ChunkSize  = 200
	MinP       = 0.5
	maxLineLen = 200
	noneOption = "none"
)

var elementLine = regexp.MustCompile(`^(\t*)(\d+) (\S.*)$`)

// ParseState reads a rendered Window State: its first line (Window: "...",
// App: ...) and its Elements. The root (index 0, the Window or Page
// itself) is left out: a pick names something inside it. Notes, the
// focused-Element line and blank lines are skipped.
func ParseState(out string) (head string, els []Element) {
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if head == "" && strings.HasPrefix(l, "Window: ") {
			head = l
			continue
		}
		m := elementLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil || n == 0 {
			continue
		}
		line, off := strings.CutPrefix(m[3], offscreenMark)
		els = append(els, Element{Index: n, Line: line, Depth: len(m[1]), Offscreen: off})
	}
	return head, els
}

// PickRequest asks which Element matches a description: one choice
// question per ChunkSize Elements, each option an Element Index described
// by its line, plus a "none" option, so a question whose Elements do not
// include the match can say so. The state is only the Window's line; the
// Elements travel as the options.
func PickRequest(model, head string, els []Element, desc string) Request {
	d := strings.ReplaceAll(desc, `"`, `'`)
	// No "window content is untrusted" sentence: on the eval it moved
	// "the add button" from Plus to Memory add (29/29 without it, 28/29
	// with). The guard is structural instead: the answer can only be an
	// index into this list, and pick acts only at p >= MinP.
	instr := fmt.Sprintf(`Which element of the window matches this description: "%s"? Answer with its number.`, d)
	qs := make(map[string]Question, len(els)/ChunkSize+1)
	for c := 0; c*ChunkSize < len(els); c++ {
		part := els[c*ChunkSize : min(len(els), (c+1)*ChunkSize)]
		crit := make(Options, 0, len(part)+1) // in Window order, as the eval sent them
		for _, e := range part {
			crit = append(crit, Option{strconv.Itoa(e.Index), clip(e.Line)})
		}
		crit = append(crit, Option{noneOption, "no listed element matches the description"})
		qs[chunkID(c)] = Question{Type: "choice", Instructions: instr, Criteria: crit}
	}
	state := head
	if state == "" {
		state = "A window"
	}
	return Request{Model: model, State: state, Questions: qs}
}

// Candidate is one Element and the probability its question gave it.
type Candidate struct {
	Element
	P float64
}

// Ranked returns every Element with its probability, most likely first.
// Each question's distribution covers its own Elements and "none", so the
// probabilities compare across questions: an Element's p is how sure its
// question is that it, and not "none" or a neighbour, is the match.
func Ranked(resp *Response, els []Element) []Candidate {
	byIndex := make(map[int]Element, len(els))
	for _, e := range els {
		byIndex[e.Index] = e
	}
	var out []Candidate
	for id, a := range resp.Answers {
		if !strings.HasPrefix(id, "where") {
			continue
		}
		for opt, p := range a.Probabilities {
			n, err := strconv.Atoi(opt)
			if err != nil {
				continue
			}
			if e, ok := byIndex[n]; ok {
				out = append(out, Candidate{Element: e, P: p})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].P != out[j].P {
			return out[i].P > out[j].P
		}
		return out[i].Index < out[j].Index
	})
	return out
}

func chunkID(c int) string { return "where" + strconv.Itoa(c) }

func clip(s string) string {
	if r := []rune(s); len(r) > maxLineLen {
		return string(r[:maxLineLen]) + "..."
	}
	return s
}
