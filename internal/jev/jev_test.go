package jev

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const calcState = "Window: \"Calculator\", App: CalculatorApp.exe.\n" +
	"\t0 window Calculator Secondary Actions: Raise\n" +
	"\t\t35 button Minus ID: minusButton\n" +
	"\t\t36 button Plus ID: plusButton\n" +
	"\t\t37 button Equals ID: equalButton\n" +
	"\n" +
	"The focused UI element is 14 text Display is 0 ID: CalculatorResults.\n" +
	"Note: --interactive left out 2 of the 8 Elements; their Element Indexes still work\n"

func TestParseStateKeepsElementsNotTheRootOrNotes(t *testing.T) {
	head, els := ParseState(strings.ReplaceAll(calcState, "\n", "\r\n"))
	if head != `Window: "Calculator", App: CalculatorApp.exe.` {
		t.Errorf("head = %q", head)
	}
	want := []Element{{35, "button Minus ID: minusButton", 2, false}, {36, "button Plus ID: plusButton", 2, false}, {37, "button Equals ID: equalButton", 2, false}}
	if len(els) != len(want) {
		t.Fatalf("els = %v, want %v", els, want)
	}
	for i := range want {
		if els[i] != want[i] {
			t.Errorf("els[%d] = %v, want %v", i, els[i], want[i])
		}
	}
}

func TestPickRequestDescribesEachOptionByItsLineAndChunks(t *testing.T) {
	var els []Element
	for i := 1; i <= ChunkSize+5; i++ {
		els = append(els, Element{i, "button B", 2, false})
	}
	els[0].Line = strings.Repeat("x", 300)
	req := PickRequest("jev-latest", "Window: \"W\"", els, `the "OK" button`)
	if req.Model != "jev-latest" || req.State != `Window: "W"` {
		t.Errorf("model %q, state %v", req.Model, req.State)
	}
	if len(req.Questions) != 2 {
		t.Fatalf("%d questions, want 2 (a chunk of %d and one of 5)", len(req.Questions), ChunkSize)
	}
	q0 := req.Questions["where0"]
	c0 := q0.Criteria.(Options)
	if q0.Type != "choice" || len(c0) != ChunkSize+1 || c0[len(c0)-1].Name != "none" {
		t.Errorf("where0: type %q, %d options, last %v", q0.Type, len(c0), c0[len(c0)-1])
	}
	if s := c0[0].Desc.(string); len([]rune(s)) != maxLineLen+3 {
		t.Errorf("a long line is not clipped: %d runes", len([]rune(s)))
	}
	if c1 := req.Questions["where1"].Criteria.(Options); len(c1) != 6 || c1[4] != (Option{"205", "button B"}) {
		t.Errorf("where1 = %v", c1)
	}
	// The options reach the wire in Window order, not sorted as strings.
	raw0, _ := json.Marshal(q0)
	if i1, i2, i10 := strings.Index(string(raw0), `"1":`), strings.Index(string(raw0), `"2":`), strings.Index(string(raw0), `"10":`); !(i1 < i2 && i2 < i10) {
		t.Errorf("options out of Window order: %s", string(raw0)[:200])
	}
	if !strings.Contains(q0.Instructions, `"the 'OK' button"`) {
		t.Errorf("instructions = %q", q0.Instructions)
	}
	// Only Jev's own fields go on the wire.
	raw, _ := json.Marshal(req)
	var top map[string]any
	_ = json.Unmarshal(raw, &top)
	for k := range top {
		if k != "model" && k != "state" && k != "questions" {
			t.Errorf("request carries %q, which Jev's contract does not have", k)
		}
	}
}

func TestRankedComparesAcrossQuestionsAndSkipsNone(t *testing.T) {
	els := []Element{{1, "a", 2, false}, {2, "b", 2, false}, {300, "c", 2, false}}
	resp := &Response{Answers: map[string]Answer{
		"where0": {Type: "choice", Choice: "none", Probabilities: map[string]float64{"1": 0.1, "2": 0.2, "none": 0.7}},
		"where1": {Type: "choice", Choice: "300", Probabilities: map[string]float64{"300": 0.9, "none": 0.1, "999": 0.5}},
		"exists": {Type: "noul", Noul: 0.9},
	}}
	r := Ranked(resp, els)
	if len(r) != 3 || r[0].Index != 300 || r[0].P != 0.9 || r[1].Index != 2 || r[2].Index != 1 {
		t.Errorf("ranked = %+v", r)
	}
}

func TestSystemOneSendsTheWireShapeAndReadsAnswers(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer k" || r.Method != http.MethodPost {
			t.Errorf("%s %s auth %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		io.WriteString(w, `{"model":"openjev-0.1","answers":{"q":{"type":"choice","choice":"a","probabilities":{"a":0.8,"b":0.2},"confidence":0.3}},"usage":{"input_tokens":12,"output_tokens":0}}`)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/", Key: "k"}
	resp, err := c.SystemOne(context.Background(), Request{Model: "m", State: "s", Questions: map[string]Question{"q": {Type: "choice", Criteria: map[string]any{"a": nil, "b": "B"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Model != "openjev-0.1" || resp.Answers["q"].Choice != "a" || resp.Answers["q"].Probabilities["b"] != 0.2 || resp.Usage.InputTokens != 12 {
		t.Errorf("resp = %+v", resp)
	}
	if got["model"] != "m" || got["state"] != "s" {
		t.Errorf("sent %v", got)
	}
}

func TestSystemOneRetriesRateLimitsButNotAUsedUpQuota(t *testing.T) {
	calls := 0
	var slept []time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.Header.Get("Authorization") {
		case "Bearer quota":
			w.WriteHeader(429)
			io.WriteString(w, `{"detail":{"error_type":"quota_exceeded_error","message":"quota used up"}}`)
		default:
			if calls < 3 {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(529)
				io.WriteString(w, `{"detail":{"error_type":"overloaded_error","message":"busy"}}`)
				return
			}
			io.WriteString(w, `{"model":"x","answers":{}}`)
		}
	}))
	defer srv.Close()
	sleep := func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }

	c := &Client{BaseURL: srv.URL, Key: "k", Sleep: sleep}
	if _, err := c.SystemOne(context.Background(), Request{}); err != nil {
		t.Fatalf("after two 529s: %v", err)
	}
	if calls != 3 || len(slept) != 2 || slept[0] != maxRetryAfter {
		t.Errorf("calls %d, slept %v (retry-after capped at %v)", calls, slept, maxRetryAfter)
	}

	calls = 0
	c.Key = "quota"
	_, err := c.SystemOne(context.Background(), Request{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Type != "quota_exceeded_error" || calls != 1 {
		t.Errorf("quota: err %v after %d calls; want one call and quota_exceeded_error", err, calls)
	}
	if !strings.Contains(err.Error(), "quota used up") {
		t.Errorf("message lost: %v", err)
	}
}

func TestSystemOneReadsAValidationList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		io.WriteString(w, `{"detail":[{"loc":["body","questions"],"msg":"too many options"}]}`)
	}))
	defer srv.Close()
	_, err := (&Client{BaseURL: srv.URL, Key: "k"}).SystemOne(context.Background(), Request{})
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "too many options") {
		t.Errorf("err = %v", err)
	}
}

func TestResolvePutsTheEnvironmentOverTheFileOverDefaults(t *testing.T) {
	env := map[string]string{"TYPESAFE_API_KEY": "envkey"}
	c := Resolve(Config{Key: "filekey", BaseURL: "https://api.codiv.ai/"}, func(k string) string { return env[k] })
	if c.Key != "envkey" || c.KeyFrom != "env" || c.BaseURL != "https://api.codiv.ai" || c.BaseURLFrom != "file" || c.Model != DefaultModel || c.ModelFrom != "default" {
		t.Errorf("resolved %+v", c)
	}
	c = Resolve(Config{}, func(string) string { return "" })
	if c.Key != "" || c.KeyFrom != "" || c.BaseURL != DefaultBaseURL {
		t.Errorf("empty: %+v", c)
	}
}

func TestStoredSettingsRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "winuse", "jev.json")
	if c, err := ReadStored(file); err != nil || c != (Config{}) {
		t.Fatalf("missing file: %+v %v", c, err)
	}
	if err := WriteStored(file, Config{Key: "k", Model: "openjev-0.1"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	os.WriteFile(file, append([]byte{0xEF, 0xBB, 0xBF}, raw...), 0o600)
	if c, err := ReadStored(file); err != nil || c.Key != "k" || c.Model != "openjev-0.1" {
		t.Errorf("read back %+v %v", c, err)
	}
}

func TestMaskNeverShowsTheWholeKey(t *testing.T) {
	k := "sk-codiv-abcdefghijklmnopqrstuvwxyz"
	if m := Mask(k); strings.Contains(m, "mnopqrst") || !strings.HasPrefix(m, "sk-codiv-a") {
		t.Errorf("Mask = %q", m)
	}
	if Mask("short") != "*****" {
		t.Errorf("short key: %q", Mask("short"))
	}
}
