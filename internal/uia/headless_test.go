package uia

// The Headless Actions' offline tests (ADR 0010): the guards and messages
// that run before any COM call, and the Invoke fallback's decision and
// driver over fakes. The pattern calls and read-backs run on real Windows
// only; the opt-in e2e suite covers them.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPerformSecondaryActionHeadlessRefusesRaise(t *testing.T) {
	// Raise calls SetFocus, which moves the desktop foreground: the
	// Headless path refuses it before touching the Element (a zero Client
	// proves nothing was looked up).
	err := (&Client{}).PerformSecondaryActionHeadless(context.Background(), 101, 0, "Raise")
	if !errors.Is(err, ErrRaiseHeadless) {
		t.Fatalf("error = %v, want ErrRaiseHeadless", err)
	}
}

func TestInvokeFallbackDecision(t *testing.T) {
	write := &WriteError{What: "toggling the Element", HR: -0x7fffbffb}
	cases := []struct {
		name          string
		action        string
		before, after int32
		err           error
		want          bool
	}{
		{"toggle delivered, state unchanged", "Toggle", toggleOff, toggleOff, nil, true},
		{"toggle from indeterminate, unchanged", "Toggle", toggleIndeterminate, toggleIndeterminate, nil, true},
		{"toggle delivered, state changed", "Toggle", toggleOff, toggleOn, nil, false},
		{"toggle write error, state unchanged", "Toggle", toggleOn, toggleOn, write, true},
		{"toggle write error, state changed (it took effect)", "Toggle", toggleOn, toggleOff, write, false},
		{"toggle write error, state unreadable after", "Toggle", toggleOn, -1, write, false},
		{"toggle with an unreadable state before", "Toggle", -1, -1, nil, false},
		{"toggle with an unreadable state before, write error", "Toggle", -1, -1, write, false},
		{"stale is a refusal, not a write", "Toggle", toggleOff, toggleOff, fmt.Errorf("%w: gone", ErrStale), false},
		{"disabled is a refusal", "Toggle", toggleOff, toggleOff, ErrDisabled, false},
		{"an action the Element does not offer", "Toggle", toggleOff, toggleOff, &UnavailableActionError{Action: "Toggle"}, false},
		{"expand from collapsed, still collapsed", "Expand", expandCollapsed, expandCollapsed, nil, true},
		{"expand write error from collapsed", "Expand", expandCollapsed, expandCollapsed, write, true},
		{"expand from partially expanded is not the known opposite", "Expand", expandPartiallyExpanded, expandPartiallyExpanded, nil, false},
		{"expand from an unreadable state", "Expand", -1, -1, nil, false},
		{"expand that moved to partially expanded", "Expand", expandCollapsed, expandPartiallyExpanded, nil, false},
		{"collapse from expanded, still expanded", "Collapse", expandExpanded, expandExpanded, nil, true},
		{"collapse from collapsed is not the known opposite", "Collapse", expandCollapsed, expandCollapsed, nil, false},
		{"select has no Invoke fallback", "Select", 0, 0, nil, false},
		{"scroll has no Invoke fallback", "Scroll Down", 0, 0, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := invokeFallback(tc.action, tc.before, tc.after, tc.err); got != tc.want {
				t.Errorf("invokeFallback(%q, %d, %d, %v) = %v, want %v", tc.action, tc.before, tc.after, tc.err, got, tc.want)
			}
		})
	}
}

// fakeFallback records the live operations runWithInvokeFallback drives.
type fakeFallback struct {
	calls      []string
	performErr error
	verifyErrs []error // one per verify call; nil once exhausted
	state      int32   // what the state read returns
	noInvoke   bool    // the Element offers no Invoke
	invokeErr  error
}

func (f *fakeFallback) steps() fallbackSteps {
	return fallbackSteps{
		perform: func() error { f.calls = append(f.calls, "perform"); return f.performErr },
		verify: func() error {
			f.calls = append(f.calls, "verify")
			if len(f.verifyErrs) == 0 {
				return nil
			}
			err := f.verifyErrs[0]
			f.verifyErrs = f.verifyErrs[1:]
			return err
		},
		state: func() int32 { f.calls = append(f.calls, "state"); return f.state },
		invoke: func() (bool, error) {
			if f.noInvoke {
				return false, nil
			}
			f.calls = append(f.calls, "invoke")
			return true, f.invokeErr
		},
	}
}

func TestRunWithInvokeFallback(t *testing.T) {
	unchanged := &VerifyError{Action: "Toggle", Expected: "a changed toggle state", Observed: "off"}
	write := &WriteError{What: "toggling the Element", HR: -0x7fffbffb}
	cases := []struct {
		name      string
		action    string
		before    int32
		fake      fakeFallback
		wantErr   error // nil: success; otherwise errors.Is against it
		wantCalls string
	}{
		{
			name: "verified on the pattern call: no fallback", action: "Toggle", before: toggleOff,
			wantCalls: "perform verify",
		},
		{
			name: "ignored toggle falls back to Invoke and verifies again", action: "Toggle", before: toggleOff,
			fake:      fakeFallback{verifyErrs: []error{unchanged}, state: toggleOff},
			wantCalls: "perform verify state invoke verify",
		},
		{
			name: "no Invoke offered: the read-back failure stands", action: "Toggle", before: toggleOff,
			fake:    fakeFallback{verifyErrs: []error{unchanged}, state: toggleOff, noInvoke: true},
			wantErr: unchanged, wantCalls: "perform verify state",
		},
		{
			name: "a failed write with the state unchanged falls back", action: "Toggle", before: toggleOn,
			fake:      fakeFallback{performErr: write, state: toggleOn},
			wantCalls: "perform state invoke verify",
		},
		{
			name: "a failed write that took effect does not invoke", action: "Toggle", before: toggleOn,
			fake:    fakeFallback{performErr: write, state: toggleOff},
			wantErr: write, wantCalls: "perform state",
		},
		{
			name: "an unreadable state before never invokes", action: "Toggle", before: -1,
			fake:    fakeFallback{verifyErrs: []error{unchanged}, state: -1},
			wantErr: unchanged, wantCalls: "perform verify state",
		},
		{
			name: "a refusal before the call never invokes", action: "Toggle", before: toggleOff,
			fake:    fakeFallback{performErr: ErrDisabled, state: toggleOff},
			wantErr: ErrDisabled, wantCalls: "perform state",
		},
		{
			name: "the fallback fires at most once", action: "Toggle", before: toggleOff,
			fake:    fakeFallback{verifyErrs: []error{unchanged, unchanged}, state: toggleOff},
			wantErr: unchanged, wantCalls: "perform verify state invoke verify",
		},
		{
			name: "a failed Invoke leaves the first outcome", action: "Toggle", before: toggleOff,
			fake:    fakeFallback{verifyErrs: []error{unchanged}, state: toggleOff, invokeErr: write},
			wantErr: unchanged, wantCalls: "perform verify state invoke",
		},
		{
			name: "expand still collapsed falls back", action: "Expand", before: expandCollapsed,
			fake:      fakeFallback{verifyErrs: []error{&VerifyError{Action: "Expand"}}, state: expandCollapsed},
			wantCalls: "perform verify state invoke verify",
		},
		{
			name: "collapse still expanded falls back", action: "Collapse", before: expandExpanded,
			fake:      fakeFallback{verifyErrs: []error{&VerifyError{Action: "Collapse"}}, state: expandExpanded},
			wantCalls: "perform verify state invoke verify",
		},
		{
			name: "expand from partially expanded does not invoke", action: "Expand", before: expandPartiallyExpanded,
			fake:    fakeFallback{verifyErrs: []error{&VerifyError{Action: "Expand"}}, state: expandPartiallyExpanded},
			wantErr: &VerifyError{}, wantCalls: "perform verify state",
		},
		{
			name: "select has no fallback", action: "Select", before: -1,
			fake:    fakeFallback{verifyErrs: []error{&VerifyError{Action: "Select"}}, state: -1},
			wantErr: &VerifyError{}, wantCalls: "perform verify state",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.fake
			err := runWithInvokeFallback(tc.action, tc.before, f.steps())
			switch want := tc.wantErr.(type) {
			case nil:
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
			case *VerifyError:
				var verify *VerifyError
				if !errors.As(err, &verify) {
					t.Fatalf("error = %v, want a VerifyError", err)
				}
			default:
				if !errors.Is(err, want) {
					t.Fatalf("error = %v, want %v", err, want)
				}
			}
			if got := strings.Join(f.calls, " "); got != tc.wantCalls {
				t.Errorf("calls = %q, want %q", got, tc.wantCalls)
			}
		})
	}
}

func TestVerifyErrorWithheldObservedNamesNoValue(t *testing.T) {
	// A password read-back withholds both sides; the message still says
	// the Action was delivered.
	e := &VerifyError{Action: "set-value", Expected: "the value that was set"}
	if got := e.Error(); !strings.Contains(got, "was delivered") || strings.Contains(got, `"`) {
		t.Errorf("message = %q, want the delivered wording without a quoted value", got)
	}
}
