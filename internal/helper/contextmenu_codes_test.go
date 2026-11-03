package helper

// The ContextMenu route's failures carry the codes dp-07's --json output
// reports (protocol.CodeFor): a refusal with no route, a delivered open or
// choice whose read-back failed, and a name the menu does not have.

import (
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
)

func TestContextMenuFailureCodes(t *testing.T) {
	for msg, want := range map[string]string{
		"element 3 is a WPF Element: " + errMenuWPF.Error() + nothingSentNote:                                   protocol.CodeNoHeadlessRoute,
		(&menuNotOpenedError{route: menuRoutePost}).Error():                                                     protocol.CodeVerifyFailed,
		(&menuNotOpenedError{route: menuRouteUIA}).Error():                                                      protocol.CodeVerifyFailed,
		"Escape to menu 778 was delivered, but the menu is still open":                                          protocol.CodeVerifyFailed,
		`the choice of "Show Unicode" in menu 778 was delivered, but the menu stayed open, so winuse closed it`: protocol.CodeVerifyFailed,
		`the menu item "Insert" opens a submenu, which has no background route` + nothingSentNote:               protocol.CodeNoHeadlessRoute,
		`classic menu 778 has no item named "Fly"; its items are "Undo"` + nothingSentNote:                      protocol.CodeNotFound,
		"perform-secondary-action ContextMenu at a point has no background route" + nothingSentNote:             protocol.CodeNoHeadlessRoute,
	} {
		if got, _ := protocol.CodeFor(msg, "", 1); got != want {
			t.Errorf("CodeFor(%q) = %s, want %s", msg, got, want)
		}
	}
}
