package cli

// Element Reference token parsing (ticket 11): the shared forms of every
// Element-taking Action command — an Element Index, name:<text> or
// id:<AutomationId> (@x,y is click's own, in parsePointRef). Syntax only:
// the token either carries a known form or fails locally (a usage error,
// exit 2, never dialing the helper); the reference resolves against the
// Window's latest Window State in the helper, where ambiguity and misses
// fail listing the candidates (the CLI cannot know them).

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/wstate"
)

// parseElementRefToken parses one Element Reference token into its wire
// form: an Element Index (a non-negative integer) or a name:/id:
// reference (passed through verbatim — the helper matches it against the
// Window State's names and AutomationIds, exactly as printed).
func parseElementRefToken(token string) (index *int, ref string, err error) {
	if strings.HasPrefix(token, nameRefPrefix) || strings.HasPrefix(token, idRefPrefix) {
		if strings.TrimSpace(token[strings.IndexByte(token, ':')+1:]) == "" {
			return nil, "", fmt.Errorf("a name: or id: reference needs text after the prefix, got %q", token)
		}
		return nil, token, nil
	}
	n, convErr := strconv.Atoi(token)
	if convErr != nil || n < 0 {
		return nil, "", fmt.Errorf("the element reference must be a non-negative element index, name:<text> or id:<AutomationId>, got %q", token)
	}
	return &n, "", nil
}

// isRefToken reports whether a token carries the name:/id: prefix — the
// unquoted-name hint below turns on it.
func isRefToken(token string) bool {
	return strings.HasPrefix(token, nameRefPrefix) || strings.HasPrefix(token, idRefPrefix)
}

// refPrefixes are the Element Reference forms' prefixes.
const (
	nameRefPrefix = wstate.NameRefPrefix
	idRefPrefix   = wstate.IDRefPrefix
)

// unquotedRefHint explains the common harness mistake of an unquoted
// multi-word name or ID: the shell split it, so the command saw extra
// positional arguments.
func unquotedRefHint(extra string) string {
	return fmt.Sprintf("a name: or id: reference with spaces must be one argument; quote it, e.g. %q", extra)
}
