// Package extract is what `winuse extract` prints: the rows a backend
// found in one container (a list, a table, a page's JSON-LD; ticket 26 on
// Pages, ticket 27 on native lists and grids), as one header line and a
// JSON array.
//
//	list: <ul> "Search results": 10 of 10 rows (complete)
//	[
//	{"title":"...","url":"..."},
//	...
//	]
//	hint: if this is the wrong list, pass --root <index> ...
//
// The header names the container and says whether the rows are all of
// them: "N of M rows (complete)", or "(partial: <reason> / <reason>)"
// where a reason is the --limit cap, what the page says it holds, a list
// the App shows only part of (virtualized), or the --max-chars cut. A
// backend that finds no repeated items prints "no list found" and
// nothing else, and a block wall (ticket 05) says so instead.
package extract

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The kinds of container a Result comes from.
const (
	KindList   = "list"
	KindTable  = "table"
	KindJSONLD = "jsonld"
)

// Result is what one backend found.
type Result struct {
	// Kind is KindList, KindTable or KindJSONLD; "" means no list found.
	Kind string
	// Container names it as the header prints it: `<ul>`,
	// `<div class="grid">`, a JSON-LD type, a native control.
	Container string
	// Label is its accessible name or heading, "" for none.
	Label string
	// Rows are the rows printed, at most Limit, in order; each one JSON
	// object, kept as the backend wrote it (its keys in its order).
	Rows []json.RawMessage
	// Found is how many rows the container holds (M in "N of M").
	Found int
	// Limit is the --limit the backend applied.
	Limit int
	// PageSays is what the page says it holds ("1,234 results"); a
	// partial reason only when it is more than Found.
	PageSays *Claim
	// Virtualized says the container shows only part of its rows (native
	// lists, ticket 27).
	Virtualized bool
	// AtLeast says Found is a lower bound: the backend stopped before the
	// end and could not read the total (printed "M+").
	AtLeast bool
	// Stopped is why the backend stopped reading before the end or the
	// limit ("stopped at the time limit"), a partial reason; "" for none.
	Stopped string
	// Wall names a block wall the page shows instead of its content
	// (Cloudflare, DataDome, ...); the Result then holds no rows.
	Wall string
}

// Claim is a count the page states, with its noun ("results",
// "questions").
type Claim struct {
	Total int    `json:"total"`
	Noun  string `json:"noun"`
}

// Options are the output budgets.
type Options struct {
	// MaxChars caps the printed text (0: no cap); whole rows are left out
	// from the end to fit, and the header says so.
	MaxChars int
	// Out, when set, receives the JSON array, uncut; the header, a line
	// naming the file and the hint are printed.
	Out string
	// RootHint is the command that reads the Element Indexes --root takes
	// ("winuse get-window-state 301"); "" prints no hint (a --root run).
	RootHint string
	// Write writes Out (nil: os.WriteFile).
	Write func(path string, data []byte) error
}

// NoList is the whole output when no list was found.
const NoList = "no list found"

// Render prints r within o's budgets.
func Render(r Result, o Options) (string, error) {
	if r.Wall != "" {
		return NoList + ": the Page looks like a " + r.Wall + " wall; ask the user to handle it\n", nil
	}
	if r.Kind == "" {
		return NoList + "\n", nil
	}
	hint := ""
	if o.RootHint != "" {
		hint = "hint: if this is the wrong " + noun(r.Kind) + ", pass --root <index> of the " + noun(r.Kind) + " or of one item, from " + o.RootHint + "\n"
	}
	if o.Out != "" {
		body := array(r.Rows)
		write := o.Write
		if write == nil {
			write = func(p string, d []byte) error { return os.WriteFile(p, d, 0o644) }
		}
		if err := write(o.Out, []byte(body)); err != nil {
			return "", err
		}
		return header(r, len(r.Rows), "") + "\n" +
			fmt.Sprintf("rows: %s (%d rows, %d characters)\n", o.Out, len(r.Rows), utf8.RuneCountInString(body)) + hint, nil
	}
	n := len(r.Rows)
	out := header(r, n, "") + "\n" + array(r.Rows) + hint
	if o.MaxChars <= 0 || utf8.RuneCountInString(out) <= o.MaxChars {
		return out, nil
	}
	cut := fmt.Sprintf("cut to fit %s characters; --out <file> writes every row", group(o.MaxChars))
	for n > 0 {
		n--
		out = header(r, n, cut) + "\n" + array(r.Rows[:n]) + hint
		if utf8.RuneCountInString(out) <= o.MaxChars {
			break
		}
	}
	return out, nil
}

// noun is what the hint calls the container.
func noun(kind string) string {
	if kind == KindTable {
		return "table"
	}
	return "list"
}

// header is the first line, for n rows printed; cut is the --max-chars
// reason ("" for none).
func header(r Result, n int, cut string) string {
	var b strings.Builder
	b.WriteString(r.Kind + ": " + r.Container)
	if r.Label != "" {
		b.WriteString(" " + strconv.Quote(r.Label))
	}
	var why []string
	if r.Limit > 0 && len(r.Rows) >= r.Limit && (r.Found > len(r.Rows) || r.AtLeast) {
		why = append(why, fmt.Sprintf("capped at %d", r.Limit))
	}
	if r.Virtualized {
		why = append(why, "virtualized")
	}
	if r.Stopped != "" {
		why = append(why, r.Stopped)
	}
	if r.PageSays != nil && r.PageSays.Total > r.Found {
		why = append(why, "page says "+group(r.PageSays.Total)+" "+r.PageSays.Noun)
	}
	if cut != "" {
		why = append(why, cut)
	}
	state := "(complete)"
	if len(why) > 0 {
		state = "(partial: " + strings.Join(why, " / ") + ")"
	}
	total := strconv.Itoa(r.Found)
	if r.AtLeast {
		total += "+"
	}
	fmt.Fprintf(&b, ": %d of %s rows %s", n, total, state)
	return b.String()
}

// array is the rows as a JSON array, one row per line.
func array(rows []json.RawMessage) string {
	if len(rows) == 0 {
		return "[]\n"
	}
	var b strings.Builder
	b.WriteString("[\n")
	for i, row := range rows {
		b.Write(row)
		if i < len(rows)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("]\n")
	return b.String()
}

// group writes n with thousands separators: 1,234.
func group(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
