package page

import (
	"strings"
	"testing"
)

// TestPickARIAOption: a custom list's option is named by its name exactly
// (spaces collapsed), else ignoring case; never by a part of its name;
// several matches at a rule are an error naming them; no match lists the
// options.
func TestPickARIAOption(t *testing.T) {
	opts := []ARIAOption{{Name: "Apple"}, {Name: "apple"}, {Name: "Apple pie"}, {Name: "Banana"}, {Name: "Blood orange"}}
	cases := []struct {
		want string
		idx  int
		err  string
	}{
		{"Apple", 0, ""},
		{"apple", 1, ""},
		{"  Blood   orange ", 4, ""},
		{"banana", 3, ""},
		{"BLOOD ORANGE", 4, ""},
		{"APPLE", -1, `several options match "APPLE": "Apple (option 1 of 5)", "apple (option 2 of 5)"`},
		{"Ban", -1, `the list has no option "Ban"; its options are "Apple", "apple", "Apple pie", "Banana", "Blood orange"`},
		{"pie", -1, `the list has no option "pie"`},
	}
	for _, c := range cases {
		i, err := PickARIAOption(opts, c.want)
		if c.err == "" {
			if err != nil || i != c.idx {
				t.Errorf("%q: %d %v, want %d", c.want, i, err, c.idx)
			}
			continue
		}
		if err == nil || !strings.HasPrefix(err.Error(), c.err) {
			t.Errorf("%q: %d %v, want %q", c.want, i, err, c.err)
		}
	}
	if _, err := PickARIAOption(nil, "Apple"); err == nil || err.Error() != `the list has no option "Apple", and no options at all` {
		t.Errorf("empty list: %v", err)
	}
}

// TestARIAChosen: the read-back after the option's click.
func TestARIAChosen(t *testing.T) {
	sel := ARIAOption{Name: "Banana", Selected: true}
	none := ARIAOption{Name: "Banana"}
	list := func(state, value, around string) ARIAList {
		return ARIAList{State: state, Value: value, Around: around}
	}
	cases := []struct {
		name                string
		editable            bool
		before, after       ARIAList
		optBefore, optAfter ARIAOption
		want                bool
	}{
		{"the option reads selected", false, list("ok", "Pick", ""), list("ok", "Pick", ""), none, sel, true},
		{"the trigger shows it", false, list("none", "Pick a fruit", ""), list("none", "  banana ", ""), none, none, true},
		{"the trigger shows another", false, list("none", "Pick a fruit", ""), list("none", "Apple", ""), none, none, false},
		{"a placeholder that holds the name proves nothing", false, list("none", "Banana?", ""), list("none", "Banana?", ""), none, none, false},
		{"already chosen, and still shown", false, list("none", "Banana", ""), list("none", "Banana", ""), sel, none, true},
		{"a typeahead whose list closed on its value", true, list("none", "", ""), list("none", "Banana", ""), none, none, true},
		{"a typeahead whose list closed, the choice beside it", true, list("none", "", ""), list("none", "", "Banana"), none, none, true},
		{"a typeahead whose list is still open", true, list("none", "", ""), list("ok", "Banana", ""), none, none, false},
		{"a typeahead whose list closed empty", true, list("none", "", ""), list("none", "", ""), none, none, false},
	}
	for _, c := range cases {
		if got := ARIAChosen(c.editable, c.before, c.after, c.optBefore, c.optAfter, "Banana"); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

// TestARIAJSShape: the option lookup reads the list as the list read
// does (the same finder, in "after" mode), and the choice read gives a
// custom list its own kind after the native forms.
func TestARIAJSShape(t *testing.T) {
	if !strings.Contains(ariaOptionJS, "const mode = 'after';"+ariaFindJS) || !strings.Contains(ariaListJS, ariaFindJS) {
		t.Error("the option lookup and the list read do not share the finder")
	}
	sel, aria := strings.Index(choiceJS, "el.tagName === 'SELECT'"), strings.Index(choiceJS, "kind: 'aria'")
	if sel < 0 || aria < 0 || aria < sel {
		t.Error("a <select> must be read as a select before any role")
	}
	for _, s := range []string{"role === 'combobox'", "role === 'listbox'", "=== 'listbox')"} {
		if !strings.Contains(choiceJS, s) {
			t.Errorf("choiceJS lacks %s", s)
		}
	}
}
