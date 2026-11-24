package page

// The settle (bladebro-ideas 07, ADR 0021 amendment): before --observe
// reads a Page after an Action, and for `wait <window> idle`, wait until
// the Page has stopped changing, up to a bound.
//
// After bladebro's settle (bb/src/page/perception/wait.rs:89-179), with
// two changes. Requests are not counted through the Network domain: ticket
// 11 measured Network.enable as a detectable timing cost in the user's
// Tabs, so the Page counts them itself, with a PerformanceObserver on
// resource entries in an isolated world. It sees a request only once it
// finished, so the rule is "no request finished for SettleNetQuiet",
// counted from the settle's start: a request the Action started gets that
// long to finish, and one that runs longer is not waited for. A websocket
// or a long poll that stays open is no entry, so it never holds the
// settle. The DOM side is bladebro's: SettleDOMQuiet with no mutation
// (a MutationObserver on the document, attributes and text included).
//
// The observers live in an isolated world of the main frame, so the
// Page's own script never sees them, and they are disconnected when the
// settle ends. A navigation replaces the world; the next probe creates
// one in the new document, and a document still loading counts as busy.
//
// The decision loop (settleLoop) runs on a clock it is given, so the
// tests drive it on a fake one.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/cdp"
)

const (
	// SettleDOMQuiet is how long the DOM must go without a mutation
	// (bladebro's 110 ms).
	SettleDOMQuiet = 110 * time.Millisecond
	// SettleNetQuiet is how long no request may finish (bladebro's 280 ms
	// plateau).
	SettleNetQuiet = 280 * time.Millisecond
	// SettleBound caps the settle before --observe reads: past it the read
	// goes ahead, with a note.
	SettleBound = time.Second
	// SettlePoll is the most the settle sleeps between two probes.
	SettlePoll = 50 * time.Millisecond
)

// What kept a settle from ending, for its note.
const (
	SettleBusyDOM        = "its DOM kept changing"
	SettleBusyNet        = "it kept loading resources"
	SettleBusyLoading    = "its document was still loading"
	SettleBusyNavigating = "it was navigating"
)

// SettleSample is one probe of the Page: how long its DOM and its finished
// requests have been quiet, and whether its document is still loading.
type SettleSample struct {
	DOMQuiet time.Duration
	NetQuiet time.Duration
	Loading  bool
}

// SettleResult is how a settle ended: Settled, or the bound passed while
// the Page was Busy (one of the SettleBusy reasons); After is how long it
// took.
type SettleResult struct {
	Settled bool
	Busy    string
	After   time.Duration
}

// Note is the line --observe adds when the Page had not settled.
func (r SettleResult) Note() string {
	if r.Settled {
		return ""
	}
	return fmt.Sprintf("the Page had not settled after %s (%s), so this read may miss what it does next", r.After.Round(100*time.Millisecond), r.Busy)
}

// settleClock is the settle's time: the real one, or a test's.
type settleClock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration)
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) Sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// settleFatal reports a probe error that ends the settle: a dialog, the Esc
// stop, a session or connection that is gone, or an effect watch's own end
// (its navigation, its lost world). Any other error (the world gone with a
// navigation) is a busy probe.
func settleFatal(err error) bool {
	var d *DialogOpenError
	return errors.As(err, &d) || errors.Is(err, ErrStopped) || errors.Is(err, ErrStale) ||
		errors.Is(err, errWatchNavigated) || errors.Is(err, errWatchLost)
}

// settleLoop probes until the Page settles, bound passes or ctx ends (both
// a result with Busy, not an error). A dialog, the Esc stop and a gone
// session are errors.
func settleLoop(ctx context.Context, clk settleClock, bound time.Duration, stopped func() bool, probe func(context.Context) (SettleSample, error)) (SettleResult, error) {
	start := clk.Now()
	busy := ""
	for {
		if stopped != nil && stopped() {
			return SettleResult{}, ErrStopped
		}
		s, err := probe(ctx)
		elapsed := clk.Now().Sub(start)
		need := time.Duration(0)
		switch {
		case err != nil && settleFatal(err):
			return SettleResult{}, err
		case err != nil && ctx.Err() != nil && busy != "":
			// The probe failed with the settle's end: the Page's last answer
			// says why it had not settled.
			need = SettlePoll
		case err != nil:
			busy, need = SettleBusyNavigating, SettlePoll
		case s.Loading:
			busy, need = SettleBusyLoading, SettlePoll
		case s.DOMQuiet < SettleDOMQuiet:
			busy, need = SettleBusyDOM, SettleDOMQuiet-s.DOMQuiet
		case s.NetQuiet < SettleNetQuiet:
			busy, need = SettleBusyNet, SettleNetQuiet-s.NetQuiet
		default:
			return SettleResult{Settled: true, After: elapsed}, nil
		}
		if elapsed >= bound || ctx.Err() != nil {
			return SettleResult{Busy: busy, After: elapsed}, nil
		}
		clk.Sleep(ctx, min(need, SettlePoll, bound-elapsed))
		if ctx.Err() != nil {
			return SettleResult{Busy: busy, After: clk.Now().Sub(start)}, nil
		}
	}
}

// settleWorld is the isolated world's name.
const settleWorld = "winuse-settle"

// requestWatchJS defines the request side of the settle, which page.Settle
// and the effect watch share: watchRequests(s) starts a PerformanceObserver
// that keeps s.res at the end of the latest request the Page finished
// (s.res starts when the watch does), and readRequests(s, now) reads the
// timeline too, since the observer's callbacks come late. Times are
// performance.now() milliseconds.
const requestWatchJS = `
  const requestEnd = e => e.responseEnd || e.startTime + e.duration;
  const watchRequests = s => {
    try {
      const p = new PerformanceObserver(l => { for (const e of l.getEntries()) { const end = requestEnd(e); if (end > s.res) s.res = end; } });
      p.observe({ type: 'resource' });
      return p;
    } catch (e) { return null; }
  };
  const readRequests = (s, now) => {
    const rs = performance.getEntriesByType('resource');
    for (let i = Math.max(0, rs.length - 20); i < rs.length; i++) {
      const end = requestEnd(rs[i]);
      if (end > s.res && end <= now) s.res = end;
    }
  };
`

// pageSettleJS installs the observers once per world and document, and reads
// them. Times are performance.now() milliseconds.
const pageSettleJS = `(() => { /* winuse:settle */` + requestWatchJS + `
  const k = '__winuseSettle', now = performance.now();
  let s = window[k];
  if (!s || s.doc !== document) {
    if (s) s.stop();
    s = window[k] = { doc: document, mut: now, res: now, obs: [] };
    s.stop = () => { for (const o of s.obs) { try { o.disconnect(); } catch (e) {} } s.obs = []; };
    try {
      const m = new MutationObserver(() => { s.mut = performance.now(); });
      m.observe(document, { subtree: true, childList: true, attributes: true, characterData: true });
      s.obs.push(m);
    } catch (e) {}
    const p = watchRequests(s);
    if (p) s.obs.push(p);
  }
  readRequests(s, now);
  return { dom: now - s.mut, net: now - s.res, loading: document.readyState === 'loading' };
})()`

// pageSettleStopJS disconnects the observers.
const pageSettleStopJS = `(() => { const s = window.__winuseSettle; if (s) { s.stop(); delete window.__winuseSettle; } })()`

// Settle waits until the Page of session has settled, or bound passes (a
// result that is not Settled, no error). It stops at the Esc stop
// (ErrStopped), a dialog (*DialogOpenError) and a session that is gone
// (ErrStale).
func Settle(ctx context.Context, c *cdp.Client, session string, bound time.Duration, stopped func() bool) (SettleResult, error) {
	world := 0
	defer func() {
		if world != 0 {
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pollCall)
			defer cancel()
			c.CallSession(cctx, session, "Runtime.evaluate", map[string]any{"expression": pageSettleStopJS, "contextId": world, "silent": true})
		}
	}()
	probe := func(ctx context.Context) (SettleSample, error) {
		if d := OpenDialog(c, session); d != nil {
			return SettleSample{}, &DialogOpenError{Dialog: *d}
		}
		if !c.Alive() {
			return SettleSample{}, fmt.Errorf("%w: the Browser Connection ended", ErrStale)
		}
		cctx, cancel := context.WithTimeout(ctx, pollCall)
		defer cancel()
		if world == 0 {
			f, err := mainFrame(cctx, c, session)
			if err == nil && f.ID == "" {
				err = errors.New("the Tab has no main frame")
			}
			if err == nil {
				world, err = newWorld(cctx, c, session, f.ID, settleWorld)
			}
			if err != nil {
				return SettleSample{}, settleProbeErr(c, err)
			}
		}
		res, err := c.CallSession(cctx, session, "Runtime.evaluate", map[string]any{"expression": pageSettleJS, "contextId": world, "returnByValue": true, "silent": true})
		if err == nil {
			var r struct {
				Result struct {
					Value *struct {
						DOM     float64 `json:"dom"`
						Net     float64 `json:"net"`
						Loading bool    `json:"loading"`
					} `json:"value"`
				} `json:"result"`
				ExceptionDetails *exceptionDetails `json:"exceptionDetails"`
			}
			switch {
			case json.Unmarshal(res, &r) != nil:
				err = errors.New("unreadable settle probe")
			case r.ExceptionDetails != nil:
				err = &EvalError{Msg: r.ExceptionDetails.message()}
			case r.Result.Value == nil:
				err = errors.New("empty settle probe")
			default:
				v := r.Result.Value
				return SettleSample{DOMQuiet: ms(v.DOM), NetQuiet: ms(v.Net), Loading: v.Loading}, nil
			}
		}
		// The world went with its document (a navigation): a new one next.
		world = 0
		return SettleSample{}, settleProbeErr(c, err)
	}
	return settleLoop(ctx, realClock{}, bound, stopped, probe)
}

// settleProbeErr sorts a failed probe call: a gone session or connection
// is ErrStale (fatal); anything else, such as "Cannot find context" while
// a navigation replaces the document, is a busy probe.
func settleProbeErr(c *cdp.Client, err error) error {
	if !c.Alive() || errors.Is(err, cdp.ErrSessionDetached) || errors.Is(err, cdp.ErrClosed) {
		return fmt.Errorf("%w: %v", ErrStale, err)
	}
	var ce *cdp.Error
	if errors.As(err, &ce) {
		msg := strings.ToLower(ce.Message)
		if strings.Contains(msg, "session with given id") || strings.Contains(msg, "no session") {
			return fmt.Errorf("%w: %v", ErrStale, err)
		}
	}
	return err
}

// ms turns performance.now() milliseconds into a Duration.
func ms(v float64) time.Duration { return time.Duration(v * float64(time.Millisecond)) }
