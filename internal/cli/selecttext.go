package cli

// select-text's CLI side (headless-max ticket 10): `select-text <window>
// <element> <start> <length>` or `select-text <window> <element> all`. It
// is always Headless and has no --headed form. Argument parsing is local
// (usage errors exit 2 and never dial the helper); the helper checks the
// same limits for any other client.

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
)

// selectTextUsage is the select-text command's usage line.
const selectTextUsage = "usage: winuse select-text <window> <element index | name:<text> | id:<id>> <start> <length> | all"

// allWord is the span word that selects the whole text.
const allWord = "all"

// parseSelectTextArgs parses select-text's arguments: a Window reference,
// then parseSelectTextTokens.
func parseSelectTextArgs(args []string) (protocol.SelectTextArgs, error) {
	if len(args) < 3 {
		return protocol.SelectTextArgs{}, errors.New("select-text takes a window reference, an element index or name:/id: reference, and a start and a length, or all")
	}
	a, err := parseSelectTextTokens(args[1:])
	a.Window = args[0]
	return a, err
}

// parseSelectTextTokens parses an Element Reference (an Element Index,
// name:<text> or id:<AutomationId>; not @x,y) and the span: two
// non-negative integers, a start and a length in UTF-16 units, or the
// word all. The Batch step parser shares it.
func parseSelectTextTokens(tokens []string) (protocol.SelectTextArgs, error) {
	var a protocol.SelectTextArgs
	if len(tokens) < 2 {
		return a, errors.New("select-text takes an element index or name:/id: reference, and a start and a length, or all")
	}
	if strings.HasPrefix(tokens[0], "@") {
		return a, errors.New("select-text takes an element index or name:/id: reference, not @x,y coordinates")
	}
	index, ref, err := parseElementRefToken(tokens[0])
	if err != nil {
		return a, err
	}
	a.ElementIndex, a.Ref = index, ref
	span := tokens[1:]
	switch {
	case len(span) == 1 && strings.EqualFold(span[0], allWord):
		a.All = true
		return a, nil
	case len(span) == 2:
		start, serr := parseTextOffset("start", span[0])
		if serr != nil {
			return a, serr
		}
		length, lerr := parseTextOffset("length", span[1])
		if lerr != nil {
			return a, lerr
		}
		if start+length > uia.MaxTextOffset {
			return a, fmt.Errorf("start+length must be at most %d, got %d", uia.MaxTextOffset, start+length)
		}
		a.Start, a.Length = start, length
		return a, nil
	case isRefToken(tokens[0]) && len(span) > 2:
		return a, errors.New(unquotedRefHint(tokens[0] + " " + span[0]))
	}
	return a, fmt.Errorf("select-text takes a start and a length, or all, after the element; got %q", strings.Join(span, " "))
}

// parseTextOffset parses a start or a length: an integer from 0 to
// uia.MaxTextOffset.
func parseTextOffset(what, token string) (int, error) {
	n, err := strconv.Atoi(token)
	if err != nil || n < 0 || n > uia.MaxTextOffset {
		return 0, fmt.Errorf("the %s must be an integer from 0 to %d, got %q", what, uia.MaxTextOffset, token)
	}
	return n, nil
}

// selectTextCmd implements `winuse select-text <window> <element> <start>
// <length> | all`: select text in a field, Headless.
func selectTextCmd(args []string) int {
	a, err := parseSelectTextArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "winuse: %v\n%s\n", err, selectTextUsage)
		return 2
	}
	if err := runWithArgs("select-text", a); err != nil {
		reportError(os.Stderr, err)
		return ExitCode(err)
	}
	return 0
}
