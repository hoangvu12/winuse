package fixture

import (
	"strings"
	"testing"
)

// The e2e toast's document: the title and text escaped, one
// foreground-activated button per name, its arguments the name in lower
// case (what Toast.Activated receives).
func TestToastXML(t *testing.T) {
	got := toastXML("t1", `Build <done> & "ok"`, "Pick one.", []string{"Alpha", "Beta"})
	want := `<toast launch="t1"><visual><binding template="ToastGeneric">` +
		`<text>Build &lt;done&gt; &amp; &#34;ok&#34;</text><text>Pick one.</text></binding></visual>` +
		`<actions><action content="Alpha" arguments="alpha" activationType="foreground"/>` +
		`<action content="Beta" arguments="beta" activationType="foreground"/></actions></toast>`
	if got != want {
		t.Errorf("toastXML:\n got %s\nwant %s", got, want)
	}
	if got := toastXML("t2", "T", "x", nil); got != `<toast launch="t2"><visual><binding template="ToastGeneric"><text>T</text><text>x</text></binding></visual></toast>` {
		t.Errorf("no buttons: %s", got)
	}
}

// Setting's answers (measured 2026-11-04): not found is ToastEnabled for an
// App that has shown no toast yet; other failures stay errors.
func TestSettingOf(t *testing.T) {
	if s, err := settingOf(ToastDisabledForUser, nil); err != nil || s != ToastDisabledForUser {
		t.Errorf("a read setting: %d, %v", s, err)
	}
	if s, err := settingOf(0, hrNotFound); err != nil || s != ToastEnabled {
		t.Errorf("HRESULT 0x80070490: %d, %v; want ToastEnabled", s, err)
	}
	if s, err := settingOf(0, hresultError(0x80004005)); err == nil || s != -1 || err.Error() != "ToastNotifier.Setting: HRESULT 0x80004005" {
		t.Errorf("E_FAIL: %d, %v; want the error", s, err)
	}
}

// The first toast adds ToastAUMID's key under both roots; Close deletes
// the levels that were missing, deepest first.
func TestMissingLevels(t *testing.T) {
	exists := func(have ...string) func(string) bool {
		return func(key string) bool {
			for _, h := range have {
				if key == h {
					return true
				}
			}
			return false
		}
	}
	root := `R`
	all := missingLevels(root, exists())
	want := []string{
		`R\{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`,
		`R\{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0`,
		`R\{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell`,
		`R\{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}`,
	}
	if strings.Join(all, "|") != strings.Join(want, "|") {
		t.Errorf("no level present: %q, want %q", all, want)
	}
	some := missingLevels(root, exists(want[3], want[2]))
	if strings.Join(some, "|") != strings.Join(want[:2], "|") {
		t.Errorf("the top two present: %q, want %q", some, want[:2])
	}
}
