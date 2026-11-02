package helper

// Select by name's helper-side tests (desktop-parity 12): set-value on a
// UIA combo or list rides the Headless set-value pipeline unchanged, and
// its refusals say "nothing was sent" only when the list was not opened
// for the search. The element side (selectByName) is tested in
// internal/uia; everything here is faked.

import (
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/uia"
)

func TestHeadlessSelectByNameRefusals(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantMsg  string
		wantHint string
	}{
		{
			name:     "no match in a list sent nothing",
			err:      &uia.SelectItemError{Label: "Purple", Items: []string{"Red", "Blue"}},
			wantMsg:  `no item named "Purple"; its items are "Red", "Blue"; nothing was sent`,
			wantHint: "item names exactly",
		},
		{
			name:     "no match in a combo opened and closed it",
			err:      &uia.SelectItemError{Label: "Purple", Items: []string{"Red"}, Opened: true},
			wantMsg:  `the list was opened and closed; no item named "Purple"; its items are "Red"`,
			wantHint: "item names exactly",
		},
		{
			name:     "an ambiguous name in a list sent nothing",
			err:      &uia.SelectItemError{Label: "Blue", Matches: []string{"Blue", "Blue"}},
			wantMsg:  `2 items match "Blue": "Blue", "Blue"; nothing was sent`,
			wantHint: "exact name wins",
		},
		{
			name:     "an ambiguous name in a combo opened and closed it",
			err:      &uia.SelectItemError{Label: "blue", Matches: []string{"Blue", "BLUE"}, Opened: true},
			wantMsg:  `the list was opened and closed; 2 items match "blue": "Blue", "BLUE"`,
			wantHint: "exact name wins",
		},
		{
			name:     "a read-back that does not show the item was delivered",
			err:      &uia.VerifyError{Action: "set-value", Msg: `set-value selected "Red" but the list shows "Blue" selected afterwards`},
			wantMsg:  `set-value selected "Red" but the list shows "Blue" selected afterwards`,
			wantHint: "do not repeat it",
		},
		{
			name:     "a failed notification after the selection was delivered",
			err:      &uia.VerifyError{Action: "set-value", Msg: `set-value selected "Red", but telling the app about the change failed (hung); the app may not have reacted to it`},
			wantMsg:  `set-value selected "Red", but telling the app about the change failed (hung); the app may not have reacted to it`,
			wantHint: "do not repeat it",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spy := newHeadlessSpy()
			spy.hValueErr = tc.err
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "set-value", setValueArgs("101", 3, "Purple"))
			if cmdErr == nil || cmdErr.Msg != tc.wantMsg {
				t.Fatalf("message = %v, want %q", cmdErr, tc.wantMsg)
			}
			if !strings.Contains(cmdErr.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to mention %q", cmdErr.Hint, tc.wantHint)
			}
			assertNoV1(t, spy.recorded())
		})
	}
}
