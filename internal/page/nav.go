package page

// Window-level page Actions (browser merge ticket 06): navigate, back,
// forward and reload, wait, and eval, each over the Tab's own session.
//
//   - Navigation goes through Page.navigate, Page.getNavigationHistory with
//     Page.navigateToHistoryEntry, and Page.reload, then waits for the new
//     document to commit (its loaderId, or the history index) and to reach
//     the ready state asked for. A dialog the Page opens meanwhile (a
//     beforeunload "Leave site?", an alert on load) ends the wait with a
//     *DialogError: the Page waits for its answer.
//   - Wait polls one JS predicate (selector, text, URL, ready state) with
//     Runtime.evaluate, checking the Esc stop and the dialog watch between
//     polls.
//   - Eval runs one expression (Runtime.evaluate, returnByValue,
//     awaitPromise) and formats its value.
//
// Every expression winuse builds passes the agent's text as a JSON string
// literal, never spliced into code.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
)

// Load states a navigation can wait for.
const (
	LoadComplete = "load"             // document.readyState is complete
	LoadDOM      = "domcontentloaded" // interactive or complete
	LoadNone     = "none"             // do not wait
)

// ParseLoadState checks a --wait value ("" is load).
func ParseLoadState(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", LoadComplete:
		return LoadComplete, nil
	case LoadDOM:
		return LoadDOM, nil
	case LoadNone:
		return LoadNone, nil
	}
	return "", fmt.Errorf("the load state must be load, domcontentloaded or none, got %q", s)
}

// NavURL is the address navigate opens: https:// in front of one without
// a scheme (tab new's rule).
func NavURL(u string) string { return tabURL(u) }

// NavigateError is a navigation the browser refused or could not finish
// (Page.navigate's errorText, such as net::ERR_NAME_NOT_RESOLVED).
type NavigateError struct {
	URL  string
	Text string
}

func (e *NavigateError) Error() string {
	return fmt.Sprintf("the browser could not open %s: %s", e.URL, e.Text)
}

// raceDialog sends one command and waits for its answer, or for the Page
// to open a dialog first (the command then stays pending until the dialog
// is answered: a beforeunload on navigate, an alert in an expression).
func raceDialog(ctx context.Context, c *cdp.Client, session, method string, params any) (json.RawMessage, error) {
	w := WatchOf(c, session)
	if w == nil {
		return c.CallSession(ctx, session, method, params)
	}
	type answer struct {
		raw json.RawMessage
		err error
	}
	done := make(chan answer, 1)
	go func() {
		raw, err := c.CallSession(ctx, session, method, params)
		done <- answer{raw, err}
	}()
	for {
		changed := w.Changed()
		if d := w.Dialog(); d != nil {
			select {
			case a := <-done:
				return a.raw, a.err
			default:
			}
			return nil, &DialogError{Type: d.Type, Message: d.Message}
		}
		select {
		case a := <-done:
			return a.raw, a.err
		case <-changed:
		case <-ctx.Done():
			a := <-done
			return a.raw, a.err
		}
	}
}

// Navigate opens url in the Tab: Page.navigate. It returns the new
// document's loaderId, "" for a navigation inside the same document (a
// #fragment). errorText is a *NavigateError.
func Navigate(ctx context.Context, c *cdp.Client, session, url string) (string, error) {
	res, err := raceDialog(ctx, c, session, "Page.navigate", map[string]any{"url": url, "transitionType": "typed"})
	if err != nil {
		return "", err
	}
	var r struct {
		LoaderID   string `json:"loaderId"`
		ErrorText  string `json:"errorText"`
		IsDownload bool   `json:"isDownload"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return "", err
	}
	if r.IsDownload {
		return "", ErrDownloadStarted
	}
	if r.ErrorText != "" {
		return "", &NavigateError{URL: url, Text: r.ErrorText}
	}
	return r.LoaderID, nil
}

// ErrDownloadStarted reports a navigation whose URL is a file to download:
// the Tab stays on its page and the browser downloads it.
var ErrDownloadStarted = errors.New("the address is a file to download, so the Tab stayed on its page")

// HistoryEntry is one entry of a Tab's history.
type HistoryEntry struct {
	ID    int    `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

// History is the Tab's history and the index of its current entry.
func History(ctx context.Context, c *cdp.Client, session string) (int, []HistoryEntry, error) {
	res, err := c.CallSession(ctx, session, "Page.getNavigationHistory", nil)
	if err != nil {
		return 0, nil, err
	}
	var r struct {
		CurrentIndex int            `json:"currentIndex"`
		Entries      []HistoryEntry `json:"entries"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return 0, nil, err
	}
	return r.CurrentIndex, r.Entries, nil
}

// GoToEntry moves the Tab to a history entry.
func GoToEntry(ctx context.Context, c *cdp.Client, session string, id int) error {
	_, err := raceDialog(ctx, c, session, "Page.navigateToHistoryEntry", map[string]any{"entryId": id})
	return err
}

// Reload reloads the Tab's page.
func Reload(ctx context.Context, c *cdp.Client, session string) error {
	_, err := raceDialog(ctx, c, session, "Page.reload", map[string]any{"ignoreCache": false})
	return err
}

// MainFrame is the Tab's main frame: its loaderId and URL (no script, so
// it answers while a dialog is open).
func MainFrame(ctx context.Context, c *cdp.Client, session string) (loaderID, url string, err error) {
	res, err := c.CallSession(ctx, session, "Page.getFrameTree", nil)
	if err != nil {
		return "", "", err
	}
	var ft struct {
		FrameTree frameTree `json:"frameTree"`
	}
	if err := json.Unmarshal(res, &ft); err != nil {
		return "", "", err
	}
	return ft.FrameTree.Frame.LoaderID, ft.FrameTree.Frame.URL, nil
}

// LoadTimeoutError is a wait for a load state that ran out of time: the
// navigation was delivered, and the page may still be loading.
type LoadTimeoutError struct {
	State string // the readyState read last
	After time.Duration
}

func (e *LoadTimeoutError) Error() string {
	return fmt.Sprintf("the page was still %s after %s", e.State, e.After.Round(100*time.Millisecond))
}

// pollEvery is the pause between two polls of a wait.
var pollEvery = 100 * time.Millisecond

// pollCall bounds one poll's own call, so a Page that hangs is noticed
// between polls.
const pollCall = 2 * time.Second

// WaitLoad waits for the Tab's navigation to commit (committed: nil to
// skip) and its document to reach state (LoadComplete or LoadDOM). It
// stops at the Esc stop (ErrStopped), at a dialog (*DialogError) and at
// ctx's end (*LoadTimeoutError).
func WaitLoad(ctx context.Context, c *cdp.Client, session, state string, committed func(ctx context.Context) bool, stopped func() bool) error {
	if state == LoadNone {
		return nil
	}
	start := time.Now()
	last := "loading"
	for {
		if stopped != nil && stopped() {
			return ErrStopped
		}
		if d := OpenDialog(c, session); d != nil {
			return &DialogError{Type: d.Type, Message: d.Message}
		}
		if committed == nil || committed(ctx) {
			ok, rs, err := readyState(ctx, c, session, state)
			if err == nil {
				last = rs
				if ok {
					return nil
				}
			} else if errors.Is(staleOr(err), ErrStale) && !c.Alive() {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return &LoadTimeoutError{State: last, After: time.Since(start)}
		case <-time.After(pollEvery):
		}
	}
}

// readyState reads document.readyState and whether it reached state.
func readyState(ctx context.Context, c *cdp.Client, session, state string) (bool, string, error) {
	var rs string
	if err := evalValue(ctx, c, session, "document.readyState", &rs); err != nil {
		return false, "", err
	}
	if state == LoadDOM {
		return rs == "interactive" || rs == "complete", rs, nil
	}
	return rs == "complete", rs, nil
}

// evalValue runs a short expression with its own poll bound and decodes
// its value.
func evalValue(ctx context.Context, c *cdp.Client, session, expr string, out any) error {
	cctx, cancel := context.WithTimeout(ctx, pollCall)
	defer cancel()
	res, err := c.CallSession(cctx, session, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "silent": true})
	if err != nil {
		return err
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *exceptionDetails `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return err
	}
	if r.ExceptionDetails != nil {
		return &EvalError{Msg: r.ExceptionDetails.message()}
	}
	if len(r.Result.Value) == 0 {
		return nil
	}
	return json.Unmarshal(r.Result.Value, out)
}

// CurrentURL is the Tab's location.href, falling back to the main frame's
// URL when script does not answer.
func CurrentURL(ctx context.Context, c *cdp.Client, session string) string {
	var u string
	if evalValue(ctx, c, session, "location.href", &u) == nil && u != "" {
		return u
	}
	_, u, _ = MainFrame(ctx, c, session)
	return u
}

// Wait kinds.
const (
	WaitSelector = "selector"
	WaitText     = "text"
	WaitURL      = "url"
	WaitLoadKind = "load"
	WaitDownload = "download"
)

// WaitExpr builds the predicate a wait polls. The agent's value is a JSON
// string literal in the expression.
func WaitExpr(kind, value string) (string, error) {
	lit, _ := json.Marshal(value)
	switch kind {
	case WaitSelector:
		if strings.TrimSpace(value) == "" {
			return "", errors.New("wait selector needs a CSS selector")
		}
		return `(() => { /* winuse:wait-selector */ const el = document.querySelector(` + string(lit) + `); if (!el) return false;
  const r = el.getBoundingClientRect(), s = getComputedStyle(el);
  return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none'; })()`, nil
	case WaitText:
		if value == "" {
			return "", errors.New("wait text needs the text to wait for")
		}
		return `(() => { /* winuse:wait-text */ return !!document.body && document.body.innerText.includes(` + string(lit) + `); })()`, nil
	case WaitURL:
		if strings.TrimSpace(value) == "" {
			return "", errors.New("wait url needs a URL, part of one, or a pattern with *")
		}
		return `(() => { /* winuse:wait-url */ const p = ` + string(lit) + `, u = location.href;
  if (!p.includes('*')) return u.includes(p);
  const re = new RegExp('^' + p.split('*').map(s => s.replace(/[.+?^${}()|[\]\\]/g, '\\$&')).join('.*') + '$');
  return re.test(u); })()`, nil
	case WaitLoadKind:
		state, err := ParseLoadState(value)
		if err != nil || state == LoadNone {
			return "", fmt.Errorf("wait load takes load or domcontentloaded, got %q", value)
		}
		if state == LoadDOM {
			return `/* winuse:wait-load */ document.readyState !== 'loading'`, nil
		}
		return `/* winuse:wait-load */ document.readyState === 'complete'`, nil
	}
	return "", fmt.Errorf("unknown wait %q: wait for a selector, text, url, load or download", kind)
}

// WaitTimeoutError is a wait whose condition never held in time.
type WaitTimeoutError struct{ After time.Duration }

func (e *WaitTimeoutError) Error() string {
	return fmt.Sprintf("the condition did not hold within %s", e.After.Round(100*time.Millisecond))
}

// Poll evaluates expr until it is true. It fails at once on an expression
// that throws on the first poll (an invalid selector), and stops at the
// Esc stop (ErrStopped), a dialog (*DialogOpenError) or ctx's end
// (*WaitTimeoutError). A poll that meets a navigation (the context is
// gone) counts as false.
func Poll(ctx context.Context, c *cdp.Client, session, expr string, stopped func() bool) error {
	start := time.Now()
	for first := true; ; first = false {
		if stopped != nil && stopped() {
			return ErrStopped
		}
		if d := OpenDialog(c, session); d != nil {
			return &DialogOpenError{Dialog: *d}
		}
		var ok bool
		err := evalValue(ctx, c, session, expr, &ok)
		var ee *EvalError
		switch {
		case err == nil && ok:
			return nil
		case errors.As(err, &ee) && first:
			return err
		case err != nil && !c.Alive():
			return fmt.Errorf("%w: the Browser Connection ended", ErrStale)
		}
		select {
		case <-ctx.Done():
			return &WaitTimeoutError{After: time.Since(start)}
		case <-time.After(pollEvery):
		}
	}
}

// EvalError is an expression that threw.
type EvalError struct{ Msg string }

func (e *EvalError) Error() string { return e.Msg }

// exceptionDetails is Runtime's exceptionDetails.
type exceptionDetails struct {
	Text      string `json:"text"`
	Exception *struct {
		Description string          `json:"description"`
		Value       json.RawMessage `json:"value"`
	} `json:"exception"`
}

// message is the thrown error's first line ("TypeError: x is undefined").
func (d *exceptionDetails) message() string {
	msg := ""
	if d.Exception != nil {
		msg = d.Exception.Description
		if msg == "" && len(d.Exception.Value) > 0 {
			msg = "Uncaught " + string(d.Exception.Value)
		}
	}
	if msg == "" {
		msg = d.Text
	}
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimSpace(msg)
}

// EvalLimit caps the printed result of eval, in characters.
const EvalLimit = 4000

// EvalResult is eval's printed value.
type EvalResult struct {
	Text  string // the value: a string as is, anything else as compact JSON or its JS form
	Total int    // its length in characters before the cut
}

// Cut reports whether the printed text was cut at EvalLimit.
func (r EvalResult) Cut() bool { return r.Total > len([]rune(r.Text)) }

// ErrNotJSON is a result that cannot travel by value (a DOM node, a
// window, a function).
var ErrNotJSON = errors.New("the result cannot be printed as JSON (a DOM node, a window or a function); return a plain value instead, such as el.textContent or a list of strings")

// Eval runs an expression in the Tab's page and formats its value. The
// call races the dialog watch (an alert() in the expression is reported,
// not waited for) and polls stopped: on a stop, the running script is
// terminated (Runtime.terminateExecution) and ErrStopped returned.
func Eval(ctx context.Context, c *cdp.Client, session, expr string, stopped func() bool) (EvalResult, error) {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type answer struct {
		raw json.RawMessage
		err error
	}
	done := make(chan answer, 1)
	go func() {
		raw, err := raceDialog(cctx, c, session, "Runtime.evaluate", map[string]any{
			"expression": expr, "returnByValue": true, "awaitPromise": true, "replMode": true, "silent": false,
		})
		done <- answer{raw, err}
	}()
	var a answer
	for wait := true; wait; {
		select {
		case a = <-done:
			wait = false
		case <-time.After(pollEvery):
			if stopped != nil && stopped() {
				c.CallSession(context.WithoutCancel(ctx), session, "Runtime.terminateExecution", nil)
				return EvalResult{}, ErrStopped
			}
		}
	}
	if a.err != nil {
		var ce *cdp.Error
		if errors.As(a.err, &ce) && strings.Contains(strings.ToLower(ce.Message), "by value") || errors.As(a.err, &ce) && strings.Contains(strings.ToLower(ce.Message), "reference chain") {
			return EvalResult{}, ErrNotJSON
		}
		return EvalResult{}, a.err
	}
	var r struct {
		Result           RemoteObject      `json:"result"`
		ExceptionDetails *exceptionDetails `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(a.raw, &r); err != nil {
		return EvalResult{}, err
	}
	if r.ExceptionDetails != nil {
		return EvalResult{}, &EvalError{Msg: r.ExceptionDetails.message()}
	}
	if r.Result.Subtype == "node" || r.Result.Subtype == "window" || r.Result.Type == "function" {
		return EvalResult{}, ErrNotJSON // by value these print as {}, which says nothing
	}
	return FormatResult(r.Result), nil
}

// RemoteObject is the part of Runtime.RemoteObject eval prints.
type RemoteObject struct {
	Type                string          `json:"type"`
	Subtype             string          `json:"subtype"`
	Value               json.RawMessage `json:"value"`
	UnserializableValue string          `json:"unserializableValue"`
	Description         string          `json:"description"`
}

// FormatResult prints a value compactly: a string as is, undefined and
// the unserializable numbers (NaN, Infinity, -0, 123n) in their JS form,
// anything else as compact JSON; cut at EvalLimit characters.
func FormatResult(r RemoteObject) EvalResult {
	var text string
	switch {
	case r.Type == "undefined":
		text = "undefined"
	case r.UnserializableValue != "":
		text = r.UnserializableValue
	case r.Type == "string":
		json.Unmarshal(r.Value, &text)
	case len(r.Value) > 0:
		var b bytes.Buffer
		if err := json.Compact(&b, r.Value); err != nil {
			text = string(r.Value)
		} else {
			text = b.String()
		}
	case r.Description != "":
		text = r.Description
	default:
		text = "null"
	}
	runes := []rune(text)
	res := EvalResult{Text: text, Total: len(runes)}
	if len(runes) > EvalLimit {
		res.Text = string(runes[:EvalLimit])
	}
	return res
}
