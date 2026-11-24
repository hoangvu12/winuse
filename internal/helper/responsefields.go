package helper

// What a response carries beside its text, for --json: what a Headless Page
// click or key did (effect, bladebro-ideas 06, pageeffect.go), the 4xx/5xx
// a navigation's Page answered (http_status, 02) and the challenge or block
// wall a Page looked like (wall, 05). The commands record them on the
// request's context as they print their notes; the server puts them on the
// response (Server.handle), so the CLI never reads them back from the text.

import (
	"context"
	"sync"
)

// responseFields are the recorded values; zero is none.
type responseFields struct {
	Effect     string
	HTTPStatus int
	Wall       string
}

// responseRecord is a request's slot for its responseFields.
type responseRecord struct {
	mu sync.Mutex
	f  responseFields
}

type responseRecordKey struct{}

// withResponseRecord gives a request its slot.
func withResponseRecord(ctx context.Context) (context.Context, *responseRecord) {
	r := &responseRecord{}
	return context.WithValue(ctx, responseRecordKey{}, r), r
}

// record changes the request's fields; outside a request it does nothing.
func record(ctx context.Context, set func(*responseFields)) {
	if r, ok := ctx.Value(responseRecordKey{}).(*responseRecord); ok {
		r.mu.Lock()
		set(&r.f)
		r.mu.Unlock()
	}
}

// recordEffect records the Action's effect.
func recordEffect(ctx context.Context, kind string) {
	record(ctx, func(f *responseFields) { f.Effect = kind })
}

// recordHTTPStatus records the 4xx/5xx a navigation's Page answered.
func recordHTTPStatus(ctx context.Context, code int) {
	record(ctx, func(f *responseFields) { f.HTTPStatus = code })
}

// recordWall records the wall a Page looks like; the last one noted wins.
func recordWall(ctx context.Context, kind string) {
	if kind != "" {
		record(ctx, func(f *responseFields) { f.Wall = kind })
	}
}

func (r *responseRecord) fields() responseFields {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f
}
