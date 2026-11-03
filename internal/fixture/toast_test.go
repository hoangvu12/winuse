package fixture

import "testing"

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
