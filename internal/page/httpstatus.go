package page

// The main document's HTTP status (bladebro-ideas ticket 02, method from
// ticket 11): performance.getEntriesByType('navigation')[0].responseStatus,
// read in an isolated world of the main frame once a navigation lands. It
// needs no domain (never Runtime.enable, and Network.enable never except
// on a Tab where --block is on), stays right
// through redirect chains, and still gives the real status on Chrome's own
// error page for an empty-body 4xx/5xx. It gives no headers, so no
// Retry-After.

import (
	"context"
	"encoding/json"
)

// statusWorld names the isolated world the status is read in.
const statusWorld = "winuse-status"

// statusJS reads the main document's status, 0 when the browser gives none.
const statusJS = `/* winuse:http-status */ (() => { const e = performance.getEntriesByType('navigation')[0];
  return e && typeof e.responseStatus === 'number' ? e.responseStatus : 0; })()`

// ErrHTTPResponseCode is Page.navigate's errorText for a 4xx/5xx answered
// with an empty body: Chrome shows its own error page instead.
const ErrHTTPResponseCode = "net::ERR_HTTP_RESPONSE_CODE_FAILURE"

// HTTPError reports a navigation that failed only because the server
// answered a 4xx/5xx with an empty body: it landed on Chrome's error page.
func (e *NavigateError) HTTPError() bool { return e.Text == ErrHTTPResponseCode }

// HTTPStatus is the HTTP status of the Tab's main document.
type HTTPStatus struct {
	Code int // responseStatus; 0 when the browser gives none
	// ErrorPage: the main frame shows Chrome's own error page, and URL is
	// the address it could not show (unreachableUrl); else URL is the
	// frame's.
	ErrorPage bool
	URL       string
}

// Failed reports a 4xx or 5xx answer.
func (s HTTPStatus) Failed() bool { return s.Code >= 400 && s.Code <= 599 }

// ReadHTTPStatus reads the main document's HTTP status in an isolated
// world of the session's main frame.
func ReadHTTPStatus(ctx context.Context, c Caller, session string) (HTTPStatus, error) {
	fr, err := mainFrame(ctx, c, session)
	if err != nil {
		return HTTPStatus{}, err
	}
	st := HTTPStatus{URL: fr.URL}
	if fr.UnreachableURL != "" {
		st.ErrorPage, st.URL = true, fr.UnreachableURL
	}
	world, err := isolatedWorld(ctx, c, session, fr.ID, statusWorld)
	if err != nil {
		return st, err
	}
	res, err := c.CallSession(ctx, session, "Runtime.evaluate", map[string]any{"expression": statusJS, "contextId": world, "returnByValue": true, "silent": true})
	if err != nil {
		return st, staleOr(err)
	}
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *exceptionDetails `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return st, err
	}
	if r.ExceptionDetails != nil {
		return st, &EvalError{Msg: r.ExceptionDetails.message()}
	}
	if len(r.Result.Value) > 0 {
		var code float64
		if json.Unmarshal(r.Result.Value, &code) == nil {
			st.Code = int(code)
		}
	}
	return st, nil
}

// StatusReason is a 4xx/5xx code's reason phrase, "" for one the table
// does not have. responseStatus gives no status text, so it is fixed here.
func StatusReason(code int) string { return statusReasons[code] }

var statusReasons = map[int]string{
	400: "Bad Request",
	401: "Unauthorized",
	402: "Payment Required",
	403: "Forbidden",
	404: "Not Found",
	405: "Method Not Allowed",
	406: "Not Acceptable",
	407: "Proxy Authentication Required",
	408: "Request Timeout",
	409: "Conflict",
	410: "Gone",
	411: "Length Required",
	412: "Precondition Failed",
	413: "Content Too Large",
	414: "URI Too Long",
	415: "Unsupported Media Type",
	416: "Range Not Satisfiable",
	417: "Expectation Failed",
	418: "I'm a teapot",
	421: "Misdirected Request",
	422: "Unprocessable Content",
	423: "Locked",
	424: "Failed Dependency",
	425: "Too Early",
	426: "Upgrade Required",
	428: "Precondition Required",
	429: "Too Many Requests",
	431: "Request Header Fields Too Large",
	451: "Unavailable For Legal Reasons",
	500: "Internal Server Error",
	501: "Not Implemented",
	502: "Bad Gateway",
	503: "Service Unavailable",
	504: "Gateway Timeout",
	505: "HTTP Version Not Supported",
	506: "Variant Also Negotiates",
	507: "Insufficient Storage",
	508: "Loop Detected",
	510: "Not Extended",
	511: "Network Authentication Required",
}
