package jev

// run's decisions (ADR 0023 amendment): one System One request per step
// asks for the next operation and, speculatively, a target for every
// operation that needs one, after browser-use/jev-ultrafast. The executor
// uses only the target of the chosen operation. Two nouls from
// use-browser's run guard it, each in a request of its own: danger, sent
// beside every step's request (a login, password or payment page: hand
// back), and achieved, asked when a step chooses DONE (DONE needs visible
// evidence).

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The operations a step can choose.
const (
	OpClick      = "CLICK"
	OpType       = "TYPE_TEXT"
	OpSelect     = "SELECT"
	OpScrollDown = "SCROLL_DOWN"
	OpScrollUp   = "SCROLL_UP"
	OpWait       = "WAIT"
	OpDone       = "DONE"
	OpBlocked    = "BLOCKED"
)

// The gates.
const (
	DangerAt  = 0.6 // stop before acting and hand back at or above this
	AchieveAt = 0.5 // accept DONE at or above this
)

// clickRoles are the roles a click advances: controls, not text or
// containers.
var clickRoles = []string{"button", "split button", "link", "menu item", "tab", "list item",
	"tree item", "check box", "radio button", "combo box", "data item", "header item", "option"}

// typeRoles are the roles set-value can type into. A desktop text editor
// (Notepad's) reads as a settable document; a Page's root document is not
// settable, so it never qualifies.
var typeRoles = []string{"edit", "combo box", "search box", "text box", "document"}

// Classify says what a step can do with an Element from its Window State
// line: click it, type into it. A disabled Element takes neither; a
// password field is never typed into (credentials are the user's).
func Classify(e Element) (click, typ bool) {
	role, states := roleAndStates(e.Line)
	if strings.Contains(states, "disabled") {
		return false, false
	}
	settableText := strings.Contains(states, "settable") && strings.Contains(states, "string") &&
		!strings.Contains(states, "password")
	for _, r := range typeRoles {
		if role == r && settableText {
			typ = true
		}
	}
	for _, r := range clickRoles {
		if role == r {
			click = true
		}
	}
	return click, typ
}

// roleAndStates splits a line into its role (the longest known role it
// starts with, else its first word) and its parenthesized states.
func roleAndStates(line string) (role, states string) {
	for _, r := range append(append([]string{}, clickRoles...), typeRoles...) {
		if (strings.HasPrefix(line, r+" ") || line == r) && len(r) > len(role) {
			role = r
		}
	}
	if role == "" {
		role, _, _ = strings.Cut(line, " ")
	}
	rest := strings.TrimPrefix(line, role)
	if s := strings.TrimSpace(rest); strings.HasPrefix(s, "(") {
		if end := strings.Index(s, ")"); end > 0 {
			states = s[1:end]
		}
	}
	return role, states
}

// Value is a text the caller supplied for TYPE_TEXT; Jev can choose
// which one, never write one.
type Value struct {
	ID   string // v1, v2, ...
	Text string
	Used bool
}

// RunStep is one executed step in the history the state carries.
type RunStep struct {
	Action      string `json:"action"`
	PageChanged *bool  `json:"page_changed,omitempty"`
}

// RunInput is what a step decides from.
type RunInput struct {
	Goal    string
	Head    string // the Window's line
	Els     []Element
	Values  []Value
	History []RunStep
	Scroll  bool            // the Window scrolls: a Page, or a desktop App with a ScrollContainer
	Prev    map[string]bool // the previous read's Element lines; nil on the first step
	Failed  map[int]bool    // Elements whose Action winuse refused since the Window last changed: not offered again
}

// offscreenKeep caps the off-screen Elements a step still sends: those that
// share words with the goal or an unused value (inView).
const offscreenKeep = 40

// inView is what a step sends of the Window: every Element on screen, as
// jev-ultrafast sends only the viewport, plus at most offscreenKeep
// off-screen Elements that share words with the goal or an unused value,
// which an Action reaches directly (a Page click scrolls its target into
// view first). The rest of a long Page stays out of the request; SCROLL
// brings it on screen. Window order; left counts what it left out.
func inView(els []Element, in RunInput) (out []Element, left int) {
	words := goalWords(in)
	type hit struct {
		i, hits int
	}
	var off []hit
	for i, e := range els {
		if e.Offscreen {
			if n := wordHits(e.Line, words); n > 0 {
				off = append(off, hit{i, n})
			}
		}
	}
	sort.SliceStable(off, func(a, b int) bool { return off[a].hits > off[b].hits })
	keep := map[int]bool{}
	for _, h := range off[:min(len(off), offscreenKeep)] {
		keep[h.i] = true
	}
	for i, e := range els {
		if !e.Offscreen || keep[i] {
			out = append(out, e)
		} else {
			left++
		}
	}
	return out, left
}

// goalWords are the words of the goal and the unused values a line can
// share: three letters or more, the stop words left out.
func goalWords(in RunInput) map[string]bool {
	words := map[string]bool{}
	text := strings.ToLower(in.Goal)
	for _, v := range in.Values {
		if !v.Used {
			text += " " + strings.ToLower(v.Text)
		}
	}
	for _, w := range strings.FieldsFunc(text, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127) }) {
		if len(w) >= 3 && !stopWords[w] {
			words[w] = true
		}
	}
	return words
}

// wordHits counts the words a line contains past its role: a goal that
// says "open the link" does not match every link.
func wordHits(line string, words map[string]bool) int {
	role, _ := roleAndStates(line)
	low, n := strings.ToLower(strings.TrimPrefix(line, role)), 0
	for w := range words {
		if strings.Contains(low, w) {
			n++
		}
	}
	return n
}

// shown is an Element's line as a step sends it: an off-screen one says so.
func shown(e Element) string {
	if e.Offscreen {
		return clip(e.Line) + " (off screen)"
	}
	return clip(e.Line)
}

// MaxTargets caps a target question: a choice takes 255 options, one is
// "none". One question, not chunks: chunks are normalized apart, so a
// chunk without the right Element still gives a distractor a high p (a
// run on Wikipedia clicked "349 languages" three times that way).
const MaxTargets = 254

// The request's size. Jev's context is 32k tokens for the state plus any
// one question and 64k for the state plus all questions, on every plan
// (docs.typesafe.ai/models; over it: 400 max_tokens_exceeded), and a
// Wikipedia article's whole Window State is more. Page lines run about 3
// characters a token, so the state's Element lines get 36,000 characters
// and each target question 24,000: about 20,000 tokens a question and at
// most about 36,000 a request. The long-context header would lift both
// limits to 262k, but the docs call it slower and less accurate.
const (
	stateBudget  = 36000
	targetBudget = 24000
)

// orderTargets keeps the likeliest candidates, at most MaxTargets and
// targetBudget characters, ranked by rank.
func orderTargets(els []Element, in RunInput) []Element {
	return within(rank(els, in), MaxTargets, targetBudget)
}

// within keeps the first Elements, at most n of them and budget
// characters of their (clipped) lines.
func within(els []Element, n, budget int) []Element {
	out := make([]Element, 0, min(len(els), n))
	used := 0
	for _, e := range els {
		used += len(clip(e.Line)) + 8
		if len(out) == n || used > budget {
			break
		}
		out = append(out, e)
	}
	return out
}

// rank orders Elements by how likely a step needs them: Elements that
// appeared since the previous read (the suggestions a typed query opens,
// a dialog's buttons), then those sharing words with the goal and the
// unused values, on-screen ones before off-screen ones, then the rest in
// Window order.
func rank(els []Element, in RunInput) []Element {
	words := goalWords(in)
	type scored struct {
		e     Element
		fresh bool
		hits  int
		pos   int
	}
	s := make([]scored, 0, len(els))
	for i, e := range els {
		s = append(s, scored{e: e, fresh: in.Prev != nil && !in.Prev[e.Line], hits: wordHits(e.Line, words), pos: i})
	}
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].fresh != s[j].fresh {
			return s[i].fresh
		}
		if s[i].hits != s[j].hits {
			return s[i].hits > s[j].hits
		}
		if s[i].e.Offscreen != s[j].e.Offscreen {
			return !s[i].e.Offscreen
		}
		return s[i].pos < s[j].pos
	})
	out := make([]Element, len(s))
	for i, x := range s {
		out[i] = x.e
	}
	return out
}

var stopWords = map[string]bool{"the": true, "and": true, "for": true, "with": true, "open": true, "into": true,
	"from": true, "that": true, "this": true, "then": true, "find": true, "about": true, "page": true, "click": true}

const runRules = `Advance the user's entire goal from the CURRENT window using one operation. ` +
	`Use current field values and the recent actions. Do not repeat a step the recent actions show is done. ` +
	`Fill required fields before submitting. A typed query still needs its matching suggestion clicked. ` +
	`Do not toggle a check box or radio button that is already in the requested state. ` +
	`Choose an option of a drop-down list with SELECT. ` +
	`If a Search or Submit button is visible and the required fields are filled, click it. ` +
	`WAIT only when the needed control is absent or disabled, or submitted results are still loading. ` +
	`Prefer a useful visible control over WAIT and over scrolling. ` +
	`DONE requires visible evidence that every requirement of the goal is satisfied; if asked to open something, ` +
	`a matching link is not enough. BLOCKED means no offered operation can make progress.`

const targetRules = `Choose the best element if the next operation is the one this question names. ` +
	`Another question decides which operation runs. Use the goal, field values and recent actions. ` +
	`Do not choose a field that already contains the requested value. Choose only an offered element.`

// RunRequest builds a step's request and returns the operations it
// offered.
func RunRequest(model string, in RunInput) (Request, map[string]string) {
	var click, typ, sel []Element
	boxes := SelectOptions(in.Els)
	isBox := map[int]bool{}
	for _, b := range boxes {
		isBox[b.Index] = true
	}
	view, _ := inView(in.Els, in)
	for _, e := range view {
		if in.Failed[e.Index] {
			continue
		}
		if b, ok := boxes[e.Index]; ok {
			// An option is chosen through its list (set-value), not
			// clicked: a click opens the list's popup or is refused.
			sel = append(sel, Element{Index: e.Index, Depth: e.Depth, Offscreen: e.Offscreen,
				Line: e.Line + " (an option of: " + clip(b.Line) + ")"})
			continue
		}
		if isBox[e.Index] {
			continue
		}
		c, t := Classify(e)
		if c {
			click = append(click, e)
		}
		if t {
			typ = append(typ, e)
		}
	}
	var unused []Value
	for _, v := range in.Values {
		if !v.Used {
			unused = append(unused, v)
		}
	}
	ops := map[string]string{
		OpWait:    "The needed control is absent or disabled, or submitted results are still loading",
		OpDone:    "Every requirement of the goal is visibly satisfied in this window",
		OpBlocked: "No offered operation can make progress toward the goal",
	}
	if len(click) > 0 {
		ops[OpClick] = "Click an element: a button, link, menu item, tab, list item, check box or suggestion"
	}
	if len(typ) > 0 && len(unused) > 0 {
		ops[OpType] = "Type one of the supplied text values into an editable field"
	}
	if len(sel) > 0 {
		ops[OpSelect] = "Choose an option of a drop-down list"
	}
	if in.Scroll {
		ops[OpScrollDown] = "Scroll down to reveal more of the window's content"
		ops[OpScrollUp] = "Scroll up to an earlier part of the window's content"
	}
	goal := "Goal: " + in.Goal + "\n"
	qs := map[string]Question{
		"operation": {Type: "choice", Instructions: goal + runRules, Criteria: anyMap(ops)},
	}
	addTargets(qs, "click_target", orderTargets(click, in), goal+targetRules+" The operation: CLICK.")
	addTargets(qs, "select_target", orderTargets(sel, in), goal+targetRules+" The operation: SELECT.")
	if _, ok := ops[OpType]; ok {
		addTargets(qs, "type_target", orderTargets(typ, in), goal+targetRules+" The operation: TYPE_TEXT.")
		vals := map[string]any{"none": "no supplied value is needed now"}
		for _, v := range unused {
			vals[v.ID] = v.Text
		}
		qs["value_next"] = Question{Type: "choice", Criteria: vals,
			Instructions: goal + "If the next operation types text, which supplied value goes in next?"}
	}
	return Request{Model: model, State: runState(in), Questions: qs}, ops
}

// SelectOptions maps each option of a drop-down list (a combo box that
// takes no typed text, as a Page's <select> reads) to its list. A
// disabled option is left out.
func SelectOptions(els []Element) map[int]Element {
	out := map[int]Element{}
	var open []Element // the lists enclosing the current Element
	for _, e := range els {
		for len(open) > 0 && open[len(open)-1].Depth >= e.Depth {
			open = open[:len(open)-1]
		}
		role, states := roleAndStates(e.Line)
		if role == "combo box" && !strings.Contains(states, "settable") {
			open = append(open, e)
			continue
		}
		if len(open) > 0 && (role == "list item" || role == "option") && !strings.Contains(states, "disabled") {
			out[e.Index] = open[len(open)-1]
		}
	}
	return out
}

// ScrollContainer is what SCROLL scrolls in a desktop App: of the Elements
// that scroll vertically (their line offers Scroll Up or Scroll Down), the
// one holding the most Elements, as Settings' page pane holds more than its
// navigation pane. A Page scrolls its own window instead (run's eval). ok is
// false when nothing in the Window scrolls.
func ScrollContainer(els []Element) (best Element, ok bool) {
	most := -1
	for i, e := range els {
		_, actions, _ := strings.Cut(e.Line, "Secondary Actions:")
		if !strings.Contains(actions, "Scroll Down") && !strings.Contains(actions, "Scroll Up") {
			continue
		}
		if _, states := roleAndStates(e.Line); strings.Contains(states, "disabled") {
			continue
		}
		n := 0
		for _, d := range els[i+1:] {
			if d.Depth <= e.Depth {
				break
			}
			n++
		}
		if n > most {
			best, most, ok = e, n, true
		}
	}
	return best, ok
}

// OptionLabel is an option's text as set-value takes it: its line
// without the role, the states and the trailing fields.
func OptionLabel(line string) string {
	role, _ := roleAndStates(line)
	s := strings.TrimSpace(strings.TrimPrefix(line, role))
	if strings.HasPrefix(s, "(") {
		if end := strings.Index(s, ")"); end > 0 {
			s = strings.TrimSpace(s[end+1:])
		}
	}
	for _, f := range []string{" Secondary Actions:", " Description:", " ID:", " Value:", " (an option of:"} {
		if i := strings.Index(s, f); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

// The guards are nouls, and each is asked in a request of its own: next
// to other questions a noul drifts. In the step's request, danger gave
// 0.00-0.04 on a login form that alone it rated 0.99, and achieved gave
// 0.59-0.80 on a DuckDuckGo page still showing only the typed query (the
// goal was a GitHub repository); alone it gave 0.09 there, and 0.99-1.00
// on pages that did show the goals. Paired with danger, achieved still
// gave 0.62 (.scratch/jev/achieved.py, danger.py, guards.py).

// DangerRequest asks whether going on needs a sign-in, a password or a
// payment. run sends it beside every step's request, concurrently.
func DangerRequest(model string, in RunInput) Request {
	return Request{Model: model, State: runState(in), Questions: map[string]Question{
		"danger": {Type: "noul",
			Instructions: "Would continuing this task require signing in, entering a password, entering payment card details, or confirming a payment in this window?",
			Criteria: map[string]any{
				"true":  "The window shows a sign-in form that must be completed to go on, a password field, a payment card form, or a checkout or payment confirmation step",
				"false": "An ordinary window or form; an optional sign-in link is not a wall"}},
	}}
}

// ReadDanger reads DangerRequest's answer: the probability that going on
// needs a sign-in, a password or a payment.
func ReadDanger(resp *Response) (float64, error) {
	a, ok := resp.Answers["danger"]
	if !ok || a.Type != "" && a.Type != "noul" {
		return 0, ErrInvalid
	}
	return a.Noul, nil
}

// AchievedRequest asks whether the Window shows the goal achieved: a DONE
// is confirmed by it before the run ends.
func AchievedRequest(model string, in RunInput) Request {
	return Request{Model: model, State: runState(in), Questions: map[string]Question{
		"achieved": {Type: "noul",
			Instructions: "Goal: " + in.Goal + "\nDoes the current window show visible evidence that the entire goal is achieved?",
			Criteria: map[string]any{
				"true":  "Every requirement of the goal is satisfied by what the window shows",
				"false": "Some requirement of the goal is not yet satisfied"}},
	}}
}

// ConfirmDone reads AchievedRequest's answer into a DONE decision: below
// AchieveAt the DONE becomes a Stall.
func ConfirmDone(d *Decision, resp *Response) error {
	a, ok := resp.Answers["achieved"]
	if !ok || a.Type != "" && a.Type != "noul" {
		return ErrInvalid
	}
	d.Achieved = a.Noul
	if d.Achieved < AchieveAt {
		d.Stall = fmt.Sprintf("DONE rejected: the window does not show the goal achieved (achieved=%.2f)", d.Achieved)
	}
	return nil
}

// addTargets adds one choice question over the (ordered, capped) targets,
// each option described by its Element line.
func addTargets(qs map[string]Question, prefix string, els []Element, instr string) {
	if len(els) == 0 {
		return
	}
	crit := make(Options, 0, len(els)+1) // in orderTargets' order
	for _, e := range els {
		crit = append(crit, Option{strconv.Itoa(e.Index), shown(e)})
	}
	crit = append(crit, Option{noneOption, "no offered element fits"})
	qs[prefix+"0"] = Question{Type: "choice", Instructions: instr, Criteria: crit}
}

// runState is the state a step sends: the goal, the Window, the supplied
// values still unused, the last 8 steps and the Window's Elements, in
// Window order. Over stateBudget it keeps the Elements rank puts first
// and says how many it left out.
func runState(in RunInput) map[string]any {
	view, offLeft := inView(in.Els, in)
	keep := map[int]bool{}
	for _, e := range within(rank(view, in), len(view), stateBudget) {
		keep[e.Index] = true
	}
	lines := make([]string, 0, len(keep))
	for _, e := range view {
		if keep[e.Index] {
			lines = append(lines, strconv.Itoa(e.Index)+" "+shown(e))
		}
	}
	vals := map[string]string{}
	for _, v := range in.Values {
		if !v.Used {
			vals[v.ID] = v.Text
		}
	}
	hist := in.History
	if len(hist) > 8 {
		hist = hist[len(hist)-8:]
	}
	if hist == nil {
		hist = []RunStep{}
	}
	state := map[string]any{
		"goal":            in.Goal,
		"window":          in.Head,
		"supplied_values": vals,
		"recent_actions":  hist,
		"elements":        lines,
	}
	if left := len(view) - len(lines); left > 0 {
		state["elements_left_out"] = left
	}
	if offLeft > 0 {
		state["elements_scrolled_out_of_view"] = offLeft
	}
	return state
}

func anyMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Decision is a step's validated answer.
type Decision struct {
	Op       string
	OpP      float64
	Target   *Candidate // CLICK, TYPE_TEXT and SELECT (the option)
	Box      *Element   // SELECT: the drop-down list the option is set on
	Value    *Value     // TYPE_TEXT
	Danger   float64    // from DangerRequest's answer (ReadDanger)
	Achieved float64    // DONE, once ConfirmDone has read it
	Stall    string     // why the chosen operation cannot run; empty when it can
}

// ErrInvalid is an answer that does not fit the request: nothing runs.
var ErrInvalid = errors.New("the server's answer does not fit the question; nothing was done")

// Decide validates a step's answers and resolves the chosen operation's
// target. A target below MinP or a missing value becomes a Stall: the
// step does nothing and says why. A DONE still needs ConfirmDone.
func Decide(resp *Response, in RunInput, ops map[string]string) (Decision, error) {
	op, ok := resp.Answers["operation"]
	if !ok || op.Choice == "" {
		return Decision{}, ErrInvalid
	}
	if _, offered := ops[op.Choice]; !offered {
		return Decision{}, fmt.Errorf("%w (operation %q was not offered)", ErrInvalid, op.Choice)
	}
	chosen := jointChoice(resp, in, ops, op)
	d := Decision{Op: chosen, OpP: op.Probabilities[chosen]}
	switch d.Op {
	case OpClick, OpType, OpSelect:
		best := bestTarget(resp, in.Els, targetPrefix[d.Op])
		if best != nil && d.Op == OpSelect {
			box, ok := SelectOptions(in.Els)[best.Index]
			if !ok {
				return Decision{}, fmt.Errorf("%w (%d is no option of a drop-down list)", ErrInvalid, best.Index)
			}
			d.Box = &box
		}
		if best == nil || best.P < MinP {
			d.Stall = fmt.Sprintf("%s chosen, but no target reached p=%.1f", d.Op, MinP)
			if best != nil {
				d.Stall += fmt.Sprintf(" (best %d %s p=%.2f)", best.Index, clip(best.Line), best.P)
			}
			return d, nil
		}
		d.Target = best
		if d.Op == OpType {
			id := resp.Answers["value_next"].Choice
			for i := range in.Values {
				if in.Values[i].ID == id && !in.Values[i].Used {
					d.Value = &in.Values[i]
				}
			}
			if d.Value == nil {
				d.Stall = "TYPE_TEXT chosen, but no supplied value was chosen for it"
			}
		}
	}
	return d, nil
}

var targetPrefix = map[string]string{OpClick: "click_target", OpType: "type_target", OpSelect: "select_target"}

// jointChoice picks the operation by P(operation) times P(its best target
// is a real Element, not "none"); an operation without a target counts
// its own P. The operation question alone can lean to an operation whose
// own target question finds nothing: on a drop-down page TypeSafe's Jev
// gave CLICK 0.70 and SELECT 0.29, with the click target "none" at 1.0 and
// the select target Option 2 at 1.0. A tie keeps the operation question's
// choice.
func jointChoice(resp *Response, in RunInput, ops map[string]string, op Answer) string {
	score := func(name string) float64 {
		p := op.Probabilities[name]
		if prefix, ok := targetPrefix[name]; ok {
			best := bestTarget(resp, in.Els, prefix)
			if best == nil {
				return 0
			}
			p *= best.P
		}
		return p
	}
	chosen, top := op.Choice, score(op.Choice)
	names := make([]string, 0, len(ops))
	for name := range ops {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if s := score(name); s > top {
			chosen, top = name, s
		}
	}
	return chosen
}

// bestTarget is the likeliest Element across a target's chunks.
func bestTarget(resp *Response, els []Element, prefix string) *Candidate {
	sub := &Response{Answers: map[string]Answer{}}
	for id, a := range resp.Answers {
		if strings.HasPrefix(id, prefix) {
			sub.Answers["where"+strings.TrimPrefix(id, prefix)] = a
		}
	}
	r := Ranked(sub, els)
	if len(r) == 0 {
		return nil
	}
	sort.SliceStable(r, func(i, j int) bool { return r[i].P > r[j].P })
	return &r[0]
}
