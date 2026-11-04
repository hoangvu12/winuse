package protocol

// Failure codes and delivery (desktop-parity ticket 07): every failed
// Response carries a code, and a delivery and retry when its text says
// whether the Action reached the app. The CLI prints them only under the
// opt-in --json flag; the plain text output is unchanged (ADR 0004).
//
// The helper's failure sites keep building their messages as before: a
// code comes from the message (and its hint) through codeRules, in one
// place, unless the site set one on its helper.Error. The helper's
// coverage test (helper/codes_test.go) fails when a literal message at a
// failure site matches no rule, so a new failure site needs either a
// message that says what kind of failure it is, or a rule here.

import (
	"regexp"
	"strings"
)

// The failure codes, agent-desktop's names where they match.
const (
	CodeStaleRef        = "STALE_REF"         // the Element Index or Window State is out of date
	CodeAmbiguousTarget = "AMBIGUOUS_TARGET"  // a reference matches several Elements or Apps
	CodeNotFound        = "NOT_FOUND"         // no Window, Element, App, Tab, display or file matches
	CodePermDenied      = "PERM_DENIED"       // an administrator Window
	CodeDesktopLocked   = "DESKTOP_LOCKED"    // the input desktop is locked
	CodeBlockedByModal  = "BLOCKED_BY_MODAL"  // a modal or JavaScript dialog is open
	CodeMinimized       = "MINIMIZED"         // the Window is minimized
	CodeNoHeadlessRoute = "NO_HEADLESS_ROUTE" // the Action needs --headed
	CodeVerifyFailed    = "VERIFY_FAILED"     // the Action was delivered; its read-back did not match
	CodeTimeout         = "TIMEOUT"           // the request's budget ran out, or another Agent held the lane
	CodeStopped         = "STOPPED"           // the user pressed Esc
	CodeUsage           = "USAGE"             // the command line or its arguments are wrong
	CodeInternal        = "INTERNAL"          // anything else: a call that failed
)

// The deliveries a failure reports, from its text (deliveryOf), and the
// retry each one allows.
const (
	DeliveryNotDelivered        = "not_delivered"        // "nothing was sent": a retry is safe
	DeliveryDeliveredUnverified = "delivered_unverified" // "was delivered; do not repeat it"
	DeliveryUncertain           = "delivery_uncertain"   // "may have taken effect"
	RetrySafe                   = "safe"
	RetryUnsafe                 = "unsafe"
)

// codeRule maps failure text to a code. Rules are tried in order on the
// message, then (hint rules) on the hint; the first match wins.
type codeRule struct {
	code string
	re   *regexp.Regexp
}

func rules(code string, patterns ...string) []codeRule {
	out := make([]codeRule, len(patterns))
	for i, p := range patterns {
		out[i] = codeRule{code, regexp.MustCompile("(?i)" + p)}
	}
	return out
}

func join(groups ...[]codeRule) []codeRule {
	var out []codeRule
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// codeRules classify a message. The specific kinds come first: a modal
// dialog, a minimized Window or a stale index outranks the generic
// "failed" a message may also carry.
var codeRules = join(
	rules(CodeStopped, `stopped by the user`, `^interrupted$`),
	rules(CodeDesktopLocked, `desktop is locked`),
	rules(CodePermDenied, `as administrator`),
	rules(CodeNotFound, `shows no `, `is gone`, `of window \d+ closed$`),
	rules(CodeBlockedByModal, `modal dialog`, `JavaScript dialog`, `shows a dialog`, `answers nothing until it is answered`),
	rules(CodeMinimized, `is minimized`),
	rules(CodeTimeout, `timed out`, `took too long`, `did not answer`, `did not respond`, `budget (ended|expired)`,
		`in time\b`, ` is using .*; nothing was sent`, `not responding`, `^waiting (for|in) `,
		`after it was brought to the front`),
	rules(CodeAmbiguousTarget, `ambiguous`, `matches \d+ elements`, `overlapping Elements`, `several `, `items named`, `has \d+ open Windows`),
	rules(CodeStaleRef, `stale`, `cached Window State`, `no cached window state`, `has not been read by this agent`,
		`reused its ID`, `of window \d+ changed`, `read the Window State again`, `no longer within`),
	rules(CodeVerifyFailed, `delivered,? but`, `could not be read back`, `after the (restore|minimize|maximize|switch|move|resize)`,
		`still running after`, `afterwards`, `does not show it`, `was delivered, and`, `close was delivered`),
	rules(CodeNoHeadlessRoute, `no headless route`, `no background route`, `cannot be [a-z ]*headless`,
		`does not reach element`, `cannot reach .* headless`, `needs --headed`, `without the pointer`,
		`only with --headed`, `is headed[- ]only`),
	rules(CodeNotFound, `not found`, `no such`, `does not exist`, `has no open Windows`, `no Element of`,
		`there is no `, `has no item`, `matches no`, `no longer exists`, `is not open`, `no \S+ is available`,
		`has no (file chooser|classic menu bar|JavaScript dialog|screenshot|Tab|page|area)`,
		`no Tab of`, `is not attached`, `has no Browser Connection`, `^no `, `unknown (window|element|tab|app|display)`),
	rules(CodeUsage, `^usage:`, `^[a-z-]+: --`, `must be`, `is required`, `unknown `, `unsupported `, `expected `,
		`cannot be combined`, `not both`, `goes? with`, `pass one`, `empty `, `is outside`, `refuses `,
		`not an? (absolute path|folder|file|Element Index|element index|command)`, `is not absolute`, `takes `, `invalid`,
		`pass an element index`, `an element index`, `the @x`, `is not a batchable`, `has no action`,
		`is a folder`, `needs `, `only (in|with|through) `, `only [a-z]+ can`, `one of `, `, not by `, `is larger than`, `at most`,
		`is the Window ID of`, `cannot carry`, `Blocked Combo`, `click is not supported`, `cannot follow`, `need an`,
		`Tab target`, `would open its popup`, `is the document of`), // background Tabs (ADR 0017)
	rules(CodeInternal, `failed`, `fail`, `could not`, `cannot`, `can't`, `unavailable`, `refused`, `error`, `threw`,
		`not supported`, `is not`, `has no`, `is still`, `does not`, `did not`, `nothing was sent`, `nothing was started`,
		`greyed out`, `is maximized`, `canceled`, `opened`, `is a `, `is the last`, `turned`, `runs `, `ran, and`, `belongs to`,
		`different winuse build`, `^(reading|writing|encoding|enumerating|saving|listing|attaching|launching) `),
)

// firstHintRules classify by the hint before the message: an
// administrator Window's refusal says why only there, its message
// ("... failed") would read as INTERNAL. lastHintRules classify by the
// hint a message did not settle.
var (
	firstHintRules = rules(CodePermDenied, `elevated terminal`)
	lastHintRules  = join(
		rules(CodeAmbiguousTarget, `candidates`),
		rules(CodeUsage, `^usage:`, `^run winuse [a-z-]+ <`),
	)
)

// CodeFor returns the code of a failure from its message, hint and exit
// code; ok is false when no rule matched (the code is then INTERNAL).
// Exit 130 is an Esc stop and exit 2 a usage error, whatever the text.
func CodeFor(msg, hint string, exit int) (code string, ok bool) {
	switch exit {
	case ExitEsc:
		return CodeStopped, true
	case 2:
		return CodeUsage, true
	}
	for _, r := range firstHintRules {
		if r.re.MatchString(hint) {
			return r.code, true
		}
	}
	for _, r := range codeRules {
		if r.re.MatchString(msg) {
			return r.code, true
		}
	}
	for _, r := range lastHintRules {
		if r.re.MatchString(hint) {
			return r.code, true
		}
	}
	return CodeInternal, false
}

// DeliveryOf returns what a failure's text says about its Action's
// delivery, and whether a retry is safe; both are empty when the text
// says nothing. "may have (partly) taken effect" outranks "nothing was
// sent", which outranks "delivered": a hint that names a later step's
// uncertainty is the stronger claim.
func DeliveryOf(msg, hint string) (delivery, retry string) {
	text := msg + "\n" + hint
	switch {
	case uncertain.MatchString(text):
		return DeliveryUncertain, RetryUnsafe
	case notDelivered.MatchString(text):
		return DeliveryNotDelivered, RetrySafe
	case delivered.MatchString(text):
		return DeliveryDeliveredUnverified, RetryUnsafe
	case strings.HasPrefix(msg, "winuse request timed out"):
		// The budget ran out with the command still running: whatever it
		// did by then is unknown.
		return DeliveryUncertain, RetryUnsafe
	}
	return "", ""
}

var (
	uncertain    = regexp.MustCompile(`may have (partly )?taken effect`)
	notDelivered = regexp.MustCompile(`nothing was (sent|started|changed|added)`)
	delivered    = regexp.MustCompile(`was delivered|do not repeat`)
)

// Stamp fills a failed Response's code, delivery and retry: code is the
// failure site's own (empty for CodeFor's), and a usage error (exit 2)
// was never sent. A successful Response is left alone.
func (r *Response) Stamp(code string) {
	if r.OK {
		return
	}
	if code == "" {
		code, _ = CodeFor(r.Err, r.Hint, r.Exit)
	}
	r.Code = code
	r.Delivery, r.Retry = DeliveryOf(r.Err, r.Hint)
	if r.Delivery == "" && code == CodeUsage {
		r.Delivery, r.Retry = DeliveryNotDelivered, RetrySafe
	}
}
