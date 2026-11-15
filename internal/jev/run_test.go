package jev

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestClassifyKnowsWhatAStepCanDoWithAnElement(t *testing.T) {
	cases := []struct {
		line       string
		click, typ bool
	}{
		{"button Search", true, false},
		{"combo box (collapsed, settable, string) Search with DuckDuckGo", true, true},
		{"edit (settable, string) Search Wikipedia", false, true},
		{"edit (settable, string, password) Password", false, false},
		{"button (disabled) Next", false, false},
		{"edit Read-only note", false, false},
		{"text Kurt Godel", false, false},
		{"list item (selectable) openjev github", true, false},
		{"split button Save", true, false},
		{"document (settable, string) Text editor Secondary Actions: Scroll Up, Scroll Down", false, true},
		{"document Kurt Godel - Wikipedia", false, false},
	}
	for _, c := range cases {
		if click, typ := Classify(Element{1, c.line, 2, false}); click != c.click || typ != c.typ {
			t.Errorf("%q: click %v typ %v, want %v %v", c.line, click, typ, c.click, c.typ)
		}
	}
}

func TestScrollContainerIsTheDesktopPaneHoldingMostContent(t *testing.T) {
	// Settings, as its Window State reads: a navigation pane and a page pane.
	els := []Element{
		{1, "window Settings", 1, false},
		{17, "pane Secondary Actions: Scroll Up, Scroll Down ID: MenuItemsScrollViewer", 2, false},
		{19, "list item (selectable, selected) System Secondary Actions: Select", 3, false},
		{21, "list item (selectable) Bluetooth & devices Secondary Actions: Select", 3, false},
		{6, "list Secondary Actions: Scroll Left, Scroll Right ID: TabListView", 2, false},
		{7, "tab (selectable) a", 3, false},
		{8, "tab (selectable) b", 3, false},
		{9, "tab (selectable) c", 3, false},
		{46, "pane Secondary Actions: Scroll Up, Scroll Down", 2, false},
		{59, "list item Display", 3, false},
		{60, "group Display", 4, false},
		{63, "list item Sound", 3, false},
		{131, "list item About", 3, false},
		{135, "scroll bar (settable, float) Vertical", 2, false},
	}
	if box, ok := ScrollContainer(els); !ok || box.Index != 46 {
		t.Errorf("got %+v %v, want the page pane 46 (the tab list scrolls only sideways)", box, ok)
	}
	if _, ok := ScrollContainer(els[:4]); !ok {
		t.Error("a lone scrolling pane is not found")
	}
	if _, ok := ScrollContainer(runEls()); ok {
		t.Error("a Window with nothing that scrolls offers a ScrollContainer")
	}
	in := RunInput{Goal: "open About", Els: els, Scroll: true}
	if _, ops := RunRequest("m", in); ops[OpScrollDown] == "" || ops[OpScrollUp] == "" {
		t.Errorf("a scrolling desktop App is not offered SCROLL: %v", ops)
	}
}

func runEls() []Element {
	return []Element{
		{1, "link Main page", 2, false},
		{2, "button Search", 2, false},
		{3, "edit (settable, string) Search Wikipedia", 2, false},
		{4, "text Welcome", 2, false},
		{5, "link Kurt Godel", 2, false},
	}
}

func TestRunRequestOffersOnlyOperationsTheWindowAllows(t *testing.T) {
	in := RunInput{Goal: "open the Kurt Godel article", Head: `Window: "Wiki"`, Els: runEls()}
	req, ops := RunRequest("m", in)
	if _, ok := ops[OpType]; ok {
		t.Error("TYPE_TEXT offered without a supplied value")
	}
	if _, ok := ops[OpScrollDown]; ok {
		t.Error("scrolling offered without a scrollable root")
	}
	for _, q := range []string{"operation", "click_target0"} {
		if _, ok := req.Questions[q]; !ok {
			t.Errorf("no %s question", q)
		}
	}
	// The guards go in requests of their own (DangerRequest, AchievedRequest).
	for _, q := range []string{"danger", "achieved", "type_target0", "value_next"} {
		if _, ok := req.Questions[q]; ok {
			t.Errorf("unexpected %s question", q)
		}
	}

	in.Values = []Value{{ID: "v1", Text: "Kurt Godel"}}
	in.Scroll = true
	req, ops = RunRequest("m", in)
	for _, op := range []string{OpClick, OpType, OpScrollDown, OpScrollUp, OpWait, OpDone, OpBlocked} {
		if _, ok := ops[op]; !ok {
			t.Errorf("%s not offered", op)
		}
	}
	tt := req.Questions["type_target0"].Criteria.(Options)
	if len(tt) != 2 || tt[0].Name != "3" || tt[1].Name != "none" {
		t.Errorf("type targets = %v", tt)
	}
	if v := req.Questions["value_next"].Criteria.(map[string]any); v["v1"] != "Kurt Godel" || v["none"] == nil {
		t.Errorf("value_next = %v", v)
	}

	in.Values[0].Used = true
	if _, ops = RunRequest("m", in); ops[OpType] != "" {
		t.Error("TYPE_TEXT offered with every value used")
	}
}

func TestRunRequestPutsFreshAndGoalElementsFirstAndSkipsFailedOnes(t *testing.T) {
	in := RunInput{Goal: "open the Kurt Godel article", Els: runEls(),
		Prev: map[string]bool{"link Main page": true, "button Search": true, "link Kurt Godel": true}}
	in.Els = append(in.Els, Element{6, "link Suggestion", 2, false})
	req, _ := RunRequest("m", in)
	ct := req.Questions["click_target0"].Criteria.(Options)
	var order []string
	for _, o := range ct {
		order = append(order, o.Name)
	}
	// 6 is new since the last read; 5 shares words with the goal; then Window order.
	if got := strings.Join(order, ","); got != "6,5,1,2,none" {
		t.Errorf("click target order = %s", got)
	}
	raw, _ := json.Marshal(req.Questions["click_target0"])
	if !strings.Contains(string(raw), `"criteria":{"6":"link Suggestion","5":"link Kurt Godel"`) {
		t.Errorf("order lost on the wire: %s", raw)
	}

	in.Failed = map[int]bool{1: true, 2: true, 5: true, 6: true}
	if req, ops := RunRequest("m", in); ops[OpClick] != "" || req.Questions["click_target0"].Criteria != nil {
		t.Errorf("CLICK offered with every clickable Element failed: %v", ops)
	}
}

// dropdownState is the-internet's dropdown page as get-window-state reads it,
// plus DuckDuckGo's editable search combo box with a suggestion.
const dropdownState = "Window: \"The Internet\", App: brave.exe.\n" +
	"\t0 document The Internet Value: https://the-internet.herokuapp.com/dropdown\n" +
	"\t\t5 heading Dropdown List\n" +
	"\t\t6 combo box (collapsed) Value: Please select an option Secondary Actions: Expand\n" +
	"\t\t\t7 list\n" +
	"\t\t\t\t8 list item (selectable, disabled) Please select an option\n" +
	"\t\t\t\t9 list item (selectable) Option 1 Secondary Actions: Select\n" +
	"\t\t\t\t10 list item (selectable) Option 2 Secondary Actions: Select\n" +
	"\t\t11 combo box (collapsed, settable, string) Search Secondary Actions: Expand\n" +
	"\t\t\t12 list item (selectable) openjev github Secondary Actions: Select\n" +
	"\t\t13 list item (selectable) Outside any list\n"

func TestSelectOptionsBelongToTheirDropDownListOnly(t *testing.T) {
	_, els := ParseState(dropdownState)
	got := SelectOptions(els)
	if len(got) != 2 || got[9].Index != 6 || got[10].Index != 6 {
		t.Errorf("options = %v; want 9 and 10 of list 6 (not the disabled 8, a typed combo box's 12, or 13)", got)
	}
	if l := OptionLabel("list item (selectable) Option 2 Secondary Actions: Select"); l != "Option 2" {
		t.Errorf("label = %q", l)
	}
	if l := OptionLabel("option Large ID: size-l"); l != "Large" {
		t.Errorf("label = %q", l)
	}
}

func TestSelectIsOfferedForADropDownAndSetOnTheList(t *testing.T) {
	head, els := ParseState(dropdownState)
	in := RunInput{Goal: "choose Option 2", Head: head, Els: els, Scroll: true}
	req, ops := RunRequest("m", in)
	if ops[OpSelect] == "" {
		t.Fatal("SELECT not offered")
	}
	var sel, click []string
	for _, o := range req.Questions["select_target0"].Criteria.(Options) {
		sel = append(sel, o.Name)
	}
	for _, o := range req.Questions["click_target0"].Criteria.(Options) {
		click = append(click, o.Name)
	}
	if strings.Join(sel, ",") != "9,10,none" {
		t.Errorf("select targets = %v", sel)
	}
	for _, n := range click {
		if n == "6" || n == "9" || n == "10" {
			t.Errorf("the drop-down list or its option %s is a click target: %v", n, click)
		}
	}
	d, err := Decide(&Response{Answers: map[string]Answer{
		"operation":      choice(OpSelect, nil),
		"select_target0": choice("10", map[string]float64{"10": 0.97, "9": 0.03}),
	}}, in, ops)
	if err != nil || d.Target == nil || d.Target.Index != 10 || d.Box == nil || d.Box.Index != 6 {
		t.Errorf("SELECT: %+v %v", d, err)
	}
}

func TestDecideWeighsAnOperationByItsTarget(t *testing.T) {
	head, els := ParseState(dropdownState)
	in := RunInput{Goal: "choose Option 2", Head: head, Els: els, Scroll: true}
	_, ops := RunRequest("m", in)
	// TypeSafe's answer on the-internet's dropdown page.
	d, err := Decide(&Response{Answers: map[string]Answer{
		"operation":      choice(OpClick, map[string]float64{OpClick: 0.70, OpSelect: 0.29, OpWait: 0.01}),
		"click_target0":  choice("none", map[string]float64{"13": 0, "none": 1}),
		"select_target0": choice("10", map[string]float64{"10": 1, "9": 0, "none": 0}),
	}}, in, ops)
	if err != nil || d.Op != OpSelect || d.Target == nil || d.Target.Index != 10 || d.OpP != 0.29 {
		t.Errorf("decision = %+v %v; want SELECT 10", d, err)
	}
}

func TestTheStateAndTargetsStayWithinTheirBudgets(t *testing.T) {
	var els []Element
	for i := 1; i <= 3000; i++ {
		els = append(els, Element{i, "link Some article title about a topic Description: more words here", 2, false})
	}
	els[2500].Line = "link Kurt Godel"
	in := RunInput{Goal: "open Kurt Godel", Els: els}
	req, _ := RunRequest("m", in)
	s := runState(in)
	lines := s["elements"].([]string)
	if n := len(strings.Join(lines, "\n")); n > stateBudget || s["elements_left_out"] != 3000-len(lines) {
		t.Errorf("state: %d characters, %d lines, left out %v", n, len(lines), s["elements_left_out"])
	}
	if !strings.Contains(strings.Join(lines, "\n"), "2501 link Kurt Godel") {
		t.Error("the goal's Element was left out of the state")
	}
	prev := 0
	for _, l := range lines {
		n, _ := strconv.Atoi(strings.Fields(l)[0])
		if n <= prev {
			t.Fatalf("state out of Window order: %d after %d", n, prev)
		}
		prev = n
	}
	raw, _ := json.Marshal(req.Questions["click_target0"].Criteria)
	if len(raw) > targetBudget+2000 || !strings.HasPrefix(string(raw), `{"2501":"link Kurt Godel"`) {
		t.Errorf("click targets: %d bytes, start %.40s", len(raw), raw)
	}
}

func TestAStepSendsWhatIsOnScreenAndTheOffscreenElementsTheGoalNames(t *testing.T) {
	out := "Window: \"Kurt Gödel - Wikipedia\", App: brave.exe.\n" +
		"\t0 document Kurt Gödel - Wikipedia\n" +
		"\t\t1 heading Kurt Gödel\n" +
		"\t\t2 link Edit\n" +
		"\t\t3 [off screen] heading Death\n" +
		"\t\t4 [off screen] link Princeton Hospital\n" +
		"\t\t5 [off screen] link Unrelated\n"
	head, els := ParseState(out)
	if !els[2].Offscreen || els[2].Line != "heading Death" || els[0].Offscreen {
		t.Fatalf("parsed %+v", els)
	}
	in := RunInput{Goal: "open the link to the hospital where he died", Head: head, Els: els, Scroll: true}
	s := runState(in)
	got := strings.Join(s["elements"].([]string), "|")
	if got != "1 heading Kurt Gödel|2 link Edit|4 link Princeton Hospital (off screen)" || s["elements_scrolled_out_of_view"] != 2 {
		t.Errorf("state elements %q, scrolled out %v", got, s["elements_scrolled_out_of_view"])
	}
	req, _ := RunRequest("m", in)
	var names []string
	for _, o := range req.Questions["click_target0"].Criteria.(Options) {
		names = append(names, o.Name)
	}
	if strings.Join(names, ",") != "4,2,none" {
		t.Errorf("click targets %v: the goal's off-screen link first, the unrelated one left out", names)
	}
}

func TestRunStateCarriesTheLastEightStepsAndUnusedValues(t *testing.T) {
	in := RunInput{Goal: "g", Head: "Window: \"W\"", Els: runEls(),
		Values: []Value{{ID: "v1", Text: "a", Used: true}, {ID: "v2", Text: "b"}}}
	for i := 0; i < 10; i++ {
		in.History = append(in.History, RunStep{Action: strings.Repeat("x", i+1)})
	}
	s := runState(in)
	if h := s["recent_actions"].([]RunStep); len(h) != 8 || h[0].Action != "xxx" {
		t.Errorf("history = %v", h)
	}
	if v := s["supplied_values"].(map[string]string); len(v) != 1 || v["v2"] != "b" {
		t.Errorf("values = %v", v)
	}
	if e := s["elements"].([]string); len(e) != 5 || e[1] != "2 button Search" {
		t.Errorf("elements = %v", e)
	}
}

func choice(c string, p map[string]float64) Answer {
	return Answer{Type: "choice", Choice: c, Probabilities: p}
}

func TestDecideActsOnlyOnAnOfferedOperationAndASureTarget(t *testing.T) {
	in := RunInput{Goal: "g", Els: runEls(), Values: []Value{{ID: "v1", Text: "Kurt Godel"}}}
	_, ops := RunRequest("m", in)

	_, err := Decide(&Response{Answers: map[string]Answer{"operation": choice("SCROLL_DOWN", nil)}}, in, ops)
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("an operation that was not offered: err %v", err)
	}

	d, err := Decide(&Response{Answers: map[string]Answer{
		"operation":     choice(OpClick, map[string]float64{OpClick: 0.9}),
		"click_target0": choice("5", map[string]float64{"5": 0.8, "2": 0.15, "none": 0.05}),
	}}, in, ops)
	if err != nil || d.Stall != "" || d.Target == nil || d.Target.Index != 5 || d.OpP != 0.9 {
		t.Errorf("click: %+v %v", d, err)
	}

	d, _ = Decide(&Response{Answers: map[string]Answer{
		"operation":     choice(OpClick, nil),
		"click_target0": choice("5", map[string]float64{"5": 0.4, "none": 0.6}),
	}}, in, ops)
	if d.Target != nil || !strings.Contains(d.Stall, "best 5 link Kurt Godel p=0.40") {
		t.Errorf("an unsure target: %+v", d)
	}

	d, _ = Decide(&Response{Answers: map[string]Answer{
		"operation":    choice(OpType, nil),
		"type_target0": choice("3", map[string]float64{"3": 0.95}),
		"value_next":   choice("none", nil),
	}}, in, ops)
	if d.Value != nil || d.Stall == "" {
		t.Errorf("TYPE_TEXT without a value: %+v", d)
	}
	d, _ = Decide(&Response{Answers: map[string]Answer{
		"operation":    choice(OpType, nil),
		"type_target0": choice("3", map[string]float64{"3": 0.95}),
		"value_next":   choice("v1", nil),
	}}, in, ops)
	if d.Stall != "" || d.Value == nil || d.Value.Text != "Kurt Godel" || d.Target.Index != 3 {
		t.Errorf("TYPE_TEXT: %+v", d)
	}
}

func TestDangerIsAskedAlone(t *testing.T) {
	req := DangerRequest("m", RunInput{Goal: "g", Els: runEls()})
	if len(req.Questions) != 1 || req.Questions["danger"].Type != "noul" || req.State.(map[string]any)["goal"] != "g" {
		t.Errorf("request = %+v", req)
	}
	if p, err := ReadDanger(&Response{Answers: map[string]Answer{"danger": {Type: "noul", Noul: 0.99}}}); err != nil || p != 0.99 {
		t.Errorf("ReadDanger = %v %v", p, err)
	}
	if _, err := ReadDanger(&Response{Answers: map[string]Answer{"danger": {Type: "choice"}}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a choice for a noul: %v", err)
	}
}

func TestConfirmDoneNeedsTheAchievedNoulOnItsOwn(t *testing.T) {
	in := RunInput{Goal: "open the repository", Els: runEls()}
	req := AchievedRequest("m", in)
	if len(req.Questions) != 1 || req.Questions["achieved"].Type != "noul" ||
		!strings.Contains(req.Questions["achieved"].Instructions, "open the repository") {
		t.Errorf("request = %+v", req)
	}
	d := Decision{Op: OpDone}
	if err := ConfirmDone(&d, &Response{Answers: map[string]Answer{"achieved": {Type: "noul", Noul: 0.37}}}); err != nil || !strings.Contains(d.Stall, "achieved=0.37") {
		t.Errorf("a doubtful DONE: %+v %v", d, err)
	}
	d = Decision{Op: OpDone}
	if err := ConfirmDone(&d, &Response{Answers: map[string]Answer{"achieved": {Type: "noul", Noul: 0.99}}}); err != nil || d.Stall != "" || d.Achieved != 0.99 {
		t.Errorf("a sure DONE: %+v %v", d, err)
	}
	if err := ConfirmDone(&d, &Response{Answers: map[string]Answer{}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("no answer: %v", err)
	}
}
