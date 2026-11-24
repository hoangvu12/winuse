package page

// Offline tests of the main document's HTTP status (bladebro-ideas ticket
// 02) over a cdptest fake: responseStatus read in an isolated world of the
// main frame, Chrome's own error page for an empty-body answer, and the
// reason phrases. No browser runs.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/cdp/cdptest"
)

// statusTab is a fake Tab whose main document answered code; unreachable
// is the frame's unreachableUrl (Chrome's error page) when not "".
func statusTab(t *testing.T, code int, unreachable string) *fakeTab {
	t.Helper()
	f := newFakeTab(t)
	f.srv.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		frame := map[string]any{"id": "MAIN", "loaderId": "L1", "url": "https://a.test/x"}
		if unreachable != "" {
			frame["url"] = "chrome-error://chromewebdata/"
			frame["unreachableUrl"] = unreachable
		}
		return map[string]any{"frameTree": map[string]any{"frame": frame, "childFrames": []any{map[string]any{"frame": map[string]any{"id": "SUB"}}}}}, nil
	})
	f.srv.Handle("Page.createIsolatedWorld", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return map[string]any{"executionContextId": 31}, nil
	})
	f.srv.Handle("Runtime.evaluate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		var p struct {
			Expression string `json:"expression"`
			ContextID  int    `json:"contextId"`
		}
		json.Unmarshal(m.Params, &p)
		if p.ContextID != 31 || !strings.Contains(p.Expression, "performance.getEntriesByType('navigation')") || !strings.Contains(p.Expression, "responseStatus") {
			return map[string]any{"result": map[string]any{"type": "undefined"}}, nil
		}
		return map[string]any{"result": map[string]any{"type": "number", "value": code}}, nil
	})
	return f
}

func TestReadHTTPStatus(t *testing.T) {
	for _, code := range []int{200, 404, 429, 500, 503} {
		f := statusTab(t, code, "")
		st, err := ReadHTTPStatus(context.Background(), f.c, "S")
		if err != nil || st.Code != code || st.ErrorPage || st.URL != "https://a.test/x" {
			t.Errorf("status %d: %+v %v", code, st, err)
		}
		// One isolated world, in the main frame; no domain is turned on.
		var sawWorld bool
		for _, m := range f.srv.Received() {
			if strings.HasSuffix(m.Method, ".enable") {
				t.Errorf("status %d: %s was sent", code, m.Method)
			}
			if m.Method == "Page.createIsolatedWorld" {
				var p struct {
					FrameID string `json:"frameId"`
				}
				json.Unmarshal(m.Params, &p)
				sawWorld = p.FrameID == "MAIN"
			}
		}
		if !sawWorld {
			t.Errorf("status %d: not read in an isolated world of the main frame", code)
		}
	}
}

// Chrome's own error page for an empty-body 4xx/5xx still has the real
// status; the URL is the address it could not show.
func TestReadHTTPStatusOnChromesErrorPage(t *testing.T) {
	f := statusTab(t, 429, "https://a.test/limited")
	st, err := ReadHTTPStatus(context.Background(), f.c, "S")
	if err != nil || st.Code != 429 || !st.ErrorPage || st.URL != "https://a.test/limited" {
		t.Errorf("%+v %v", st, err)
	}
}

// A browser that gives no responseStatus reads as 0, not as a failure.
func TestReadHTTPStatusWithoutResponseStatus(t *testing.T) {
	f := statusTab(t, 0, "")
	st, err := ReadHTTPStatus(context.Background(), f.c, "S")
	if err != nil || st.Code != 0 || st.Failed() {
		t.Errorf("%+v %v", st, err)
	}
}

func TestReadHTTPStatusWithoutAWorld(t *testing.T) {
	f := newFakeTab(t)
	f.srv.Handle("Page.getFrameTree", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "MAIN", "loaderId": "L1", "url": "https://a.test/"}}}, nil
	})
	f.srv.Handle("Page.createIsolatedWorld", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return nil, &cdptest.ReplyError{Code: -32000, Message: "No frame for given id found"}
	})
	if _, err := ReadHTTPStatus(context.Background(), f.c, "S"); err == nil {
		t.Error("no error without an isolated world")
	}
}

func TestHTTPStatusFailed(t *testing.T) {
	for code, want := range map[int]bool{0: false, 200: false, 204: false, 301: false, 399: false, 400: true, 404: true, 429: true, 500: true, 503: true, 599: true, 600: false} {
		if got := (HTTPStatus{Code: code}).Failed(); got != want {
			t.Errorf("Failed(%d) = %v, want %v", code, got, want)
		}
	}
}

func TestStatusReason(t *testing.T) {
	for code, want := range map[int]string{
		400: "Bad Request", 401: "Unauthorized", 403: "Forbidden", 404: "Not Found", 410: "Gone",
		429: "Too Many Requests", 451: "Unavailable For Legal Reasons",
		500: "Internal Server Error", 502: "Bad Gateway", 503: "Service Unavailable", 504: "Gateway Timeout",
		499: "", 299: "",
	} {
		if got := StatusReason(code); got != want {
			t.Errorf("StatusReason(%d) = %q, want %q", code, got, want)
		}
	}
}

// Navigate keeps the loaderId of a navigation that landed on Chrome's error
// page, so the caller can wait for that page.
func TestNavigateErrorKeepsTheLoader(t *testing.T) {
	f := newFakeTab(t)
	f.srv.Handle("Page.navigate", func(c *cdptest.Conn, m cdptest.Message) (any, error) {
		return map[string]any{"frameId": "F1", "loaderId": "L7", "errorText": ErrHTTPResponseCode}, nil
	})
	_, err := Navigate(context.Background(), f.c, "S", "https://a.test/empty")
	var nav *NavigateError
	if !errors.As(err, &nav) || nav.LoaderID != "L7" || nav.Text != "net::ERR_HTTP_RESPONSE_CODE_FAILURE" || !nav.HTTPError() {
		t.Errorf("%+v", err)
	}
	if (&NavigateError{Text: "net::ERR_NAME_NOT_RESOLVED"}).HTTPError() {
		t.Error("a DNS failure is an HTTP error")
	}
}
