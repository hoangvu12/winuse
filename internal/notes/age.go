package notes

import (
	"fmt"
	"strings"
	"time"

	"github.com/hoangvu12/winuse/internal/agent"
)

// Age is how long ago a Note was added, for its line: "just now",
// "5 min ago", "3 h ago", "1 day ago", "2 months ago", "2 years ago".
func Age(added, now time.Time) string {
	d := now.Sub(added)
	day := 24 * time.Hour
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < day:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	case d < 60*day:
		return plural(int(d/day), "day")
	case d < 365*day:
		return plural(int(d/(30*day)), "month")
	}
	return plural(int(d/(365*day)), "year")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit + " ago"
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}

// Author is the name a Note records for the Agent that added it: the
// WINUSE_AGENT name, or the Harness the request came from ("claude"),
// without its process id, which means nothing later.
func Author(id agent.Identity) string {
	switch {
	case strings.HasPrefix(id.ID, "name:"):
		return id.Label
	case strings.HasPrefix(id.ID, "proc:"):
		if i := strings.LastIndexByte(id.Label, ' '); i > 0 {
			return id.Label[:i]
		}
		return id.Label
	}
	return "an unnamed agent"
}
