package helper

// The Batch's upfront value checks (bladebro-ideas ticket 04): every check
// that depends only on a step's arguments runs while the Batch prepares,
// so a Batch whose last step has a bad value sends nothing at all, and the
// failure names that step. Checks that need the Window or the Page stay at
// the step's turn (the other batch tests).

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/winuse/internal/protocol"
	"github.com/hoangvu12/winuse/internal/uia"
)

func TestBatchChecksStepValuesBeforeTheFirstStep(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	neg, big := -1, 99
	text := "hi"
	cases := []struct {
		name string
		step protocol.Step
		want string // in the failure's message
	}{
		// Arguments of the wrong type.
		{"arguments of the wrong type", protocol.Step{Kind: "click", Args: json.RawMessage(`{"element_index":"two"}`)}, "invalid arguments"},

		// Element References.
		{"no element reference", stepOf("click", protocol.ClickArgs{}), "click: an element index, a name:/id: reference or @x,y coordinates are required"},
		{"two reference forms", stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2), Ref: "name:OK"}), "not several"},
		{"a negative element index", stepOf("set-value", protocol.SetValueArgs{ElementIndex: &neg, Value: "x"}), "the element index must be a non-negative integer"},
		{"negative @x,y", stepOf("click", protocol.ClickArgs{Point: &protocol.Point{X: -1, Y: 5}}), "the @x,y coordinates must be non-negative integers"},
		{"a reference with an unknown prefix", stepOf("click", protocol.ClickArgs{Ref: "what:OK"}), `invalid element reference "what:OK"`},
		{"a name: reference with no text", stepOf("set-value", protocol.SetValueArgs{Ref: "name:  ", Value: "x"}), "invalid element reference"},

		// click's options.
		{"--count out of range", stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2), ClickCount: 4}), "click_count must be 1, 2 or 3"},
		{"an unknown --button", stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2), MouseButton: "side"}), "unsupported mouse_button: side"},
		{"a right double click", stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2), MouseButton: "right", ClickCount: 2}), "right double click is not supported"},
		{"an unknown --modifiers", stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(2), Modifiers: []string{"hyper"}}), `unknown modifier "hyper"`},

		// Keys.
		{"an unknown key name", stepOf("press-key", protocol.PressKeyArgs{Key: "Control_L+Nope"}), "unsupported key: Nope"},
		{"no key", stepOf("press-key", protocol.PressKeyArgs{}), "key is required"},
		{"a Blocked Combo", stepOf("press-key", protocol.PressKeyArgs{Key: "Alt_L+F4"}), "press-key refuses Alt_L+F4"},
		{"a Blocked Combo posted, even forced", stepOf("press-key", protocol.PressKeyArgs{Key: "Alt_L+F4", Post: true, Force: true}), "press-key --post never sends Alt_L+F4"},

		// Secondary Actions.
		{"an unknown secondary action", stepOf("perform-secondary-action", protocol.SecondaryActionArgs{ElementIndex: intPtrBatch(2), Action: "Wiggle"}), "unsupported secondary action: Wiggle"},
		{"no secondary action", stepOf("perform-secondary-action", protocol.SecondaryActionArgs{ElementIndex: intPtrBatch(2)}), "an action is required"},

		// type-text.
		{"no text", stepOf("type-text", protocol.TypeTextArgs{}), "type-text: text is required"},
		{"--interval-ms out of range", stepOf("type-text", protocol.TypeTextArgs{Text: "x", IntervalMS: 5000}), "--interval-ms must be 0 to 1000"},
		{"--into with --post", stepOf("type-text", protocol.TypeTextArgs{Text: "x", IntoIndex: intPtrBatch(2), Post: true}), "--into and --post cannot be combined"},
		{"--into with a bad reference", stepOf("type-text", protocol.TypeTextArgs{Text: "x", IntoRef: "label:Name"}), "invalid element reference"},

		// scroll.
		{"an unknown scroll direction", stepOf("scroll", protocol.ScrollArgs{ElementIndex: intPtrBatch(2), Direction: "sideways"}), "the scroll direction must be up, down, left or right"},
		{"scroll steps out of range", stepOf("scroll", protocol.ScrollArgs{ElementIndex: intPtrBatch(2), Direction: "down", Steps: &big}), "steps must be between 1 and 50, got 99"},
		{"into-view with steps", stepOf("scroll", protocol.ScrollArgs{ElementIndex: intPtrBatch(2), Direction: "into-view", Steps: intPtrBatch(2)}), "into-view takes no steps"},
		{"a direction without an Element", stepOf("scroll", protocol.ScrollArgs{X: 5, Y: 5, Direction: "down"}), "a direction or steps needs an element index"},
		{"a point scroll of nothing", stepOf("scroll", protocol.ScrollArgs{X: 5, Y: 5}), "at least one scroll amount must be non-zero"},
		{"a point scroll too far", stepOf("scroll", protocol.ScrollArgs{X: 5, Y: 5, ScrollY: 2000000}), "scroll amounts must be within"},
		{"a point scroll at a negative point", stepOf("scroll", protocol.ScrollArgs{X: -5, Y: 5, ScrollY: 120}), "the @x,y coordinates must be non-negative integers"},
		{"--post on an Element scroll", stepOf("scroll", protocol.ScrollArgs{ElementIndex: intPtrBatch(2), Direction: "down", Post: true}), "--post scrolls at an @x,y point"},
		{"an unknown scroll modifier", stepOf("scroll", protocol.ScrollArgs{X: 5, Y: 5, ScrollY: 120, Modifiers: []string{"meta"}}), `unknown modifier "meta"`},

		// drag and hover.
		{"--drop-delay out of range", stepOf("drag", protocol.DragArgs{FromX: 1, FromY: 1, ToX: 2, ToY: 2, DropDelayMS: 9000}), "--drop-delay must be 0 to 5000"},
		{"a drag to a negative point", stepOf("drag", protocol.DragArgs{FromX: 1, FromY: 1, ToX: -2, ToY: 2}), "drag: the @x,y coordinates must be non-negative integers"},
		{"a drag from an Element to a negative point", stepOf("drag", protocol.DragArgs{FromIndex: intPtrBatch(2), ToX: -2, ToY: 2}), "drag: the @x,y coordinates must be non-negative integers"},
		{"a drag to a bad reference", stepOf("drag", protocol.DragArgs{FromIndex: intPtrBatch(2), ToRef: "id:"}), "invalid element reference"},
		{"a hover at a negative point", stepOf("hover", protocol.HoverArgs{X: 3, Y: -3}), "hover: the @x,y coordinates must be non-negative integers"},
		{"a hover on a bad reference", stepOf("hover", protocol.HoverArgs{Ref: "Save"}), "invalid element reference"},

		// select-text and invoke-menu.
		{"select-text all and a span", stepOf("select-text", protocol.SelectTextArgs{ElementIndex: intPtrBatch(2), All: true, Start: 1}), "pass a start and a length, or all, not both"},
		{"select-text a negative start", stepOf("select-text", protocol.SelectTextArgs{ElementIndex: intPtrBatch(2), Start: -1, Length: 2}), "got start -1 and length 2"},
		{"invoke-menu no path", stepOf("invoke-menu", protocol.InvokeMenuArgs{}), "a menu path is required"},
		{"invoke-menu an empty item", stepOf("invoke-menu", protocol.InvokeMenuArgs{Path: []string{"File", " "}}), "the menu path has an empty item name"},

		// Tabs.
		{"tab-switch no tab", stepOf("tab-switch", protocol.TabArgs{}), "a tab id from winuse tab list is required"},
		{"tab-close no tab", stepOf("tab-close", protocol.TabArgs{}), "a tab id from winuse tab list is required"},

		// Page navigation.
		{"navigate no URL", stepOf("navigate", protocol.NavigateArgs{}), "navigate: a URL is required"},
		{"reload a URL", stepOf("reload", protocol.NavigateArgs{URL: "https://a.test/"}), "reload takes no URL"},
		{"navigate an unknown load state", stepOf("navigate", protocol.NavigateArgs{URL: "https://a.test/", Wait: "soon"}), "the load state must be load, domcontentloaded or none"},

		// upload.
		{"upload no file", stepOf("upload", protocol.UploadArgs{ElementIndex: intPtrBatch(2)}), "upload: at least one file is required"},
		{"upload a relative path", stepOf("upload", protocol.UploadArgs{Files: []string{"a.txt"}}), `the path "a.txt" is not absolute`},
		{"upload a missing file", stepOf("upload", protocol.UploadArgs{Files: []string{missing}}), "does not exist or cannot be read"},
		{"upload a folder", stepOf("upload", protocol.UploadArgs{Files: []string{dir}}), "is a folder; upload takes files"},

		// dialog and eval.
		{"dialog an unknown answer", stepOf("dialog", protocol.DialogArgs{Action: "ignore"}), "the answer must be accept or dismiss"},
		{"dialog dismiss with text", stepOf("dialog", protocol.DialogArgs{Action: "dismiss", Text: &text}), "text answers a prompt, so it goes with accept"},
		{"eval no expression", stepOf("eval", protocol.EvalArgs{}), "eval: an expression is required"},
		{"eval --max out of range", stepOf("eval", protocol.EvalArgs{Expression: "1", Max: -1}), "eval: --max takes 1 to"},

		// wait: its kind and arguments, its timeout.
		{"wait an unknown kind", stepOf("wait", protocol.WaitArgs{For: "colour", Value: "red"}), `unknown wait "colour"`},
		{"wait selector no selector", stepOf("wait", protocol.WaitArgs{For: "selector"}), "wait selector needs a CSS selector"},
		{"wait text no text", stepOf("wait", protocol.WaitArgs{For: "text"}), "wait text needs the text to wait for"},
		{"wait url --gone", stepOf("wait", protocol.WaitArgs{For: "url", Value: "/done", Gone: true}), "--gone goes with wait selector and wait text"},
		{"wait js no expression", stepOf("wait", protocol.WaitArgs{For: "js"}), "wait js needs a JavaScript expression"},
		{"wait load an unknown state", stepOf("wait", protocol.WaitArgs{For: "load", Value: "soon"}), "wait load takes load or domcontentloaded"},
		{"wait idle a value", stepOf("wait", protocol.WaitArgs{For: "idle", Value: "1s"}), "wait idle takes no value"},
		{"wait idle --gone", stepOf("wait", protocol.WaitArgs{For: "idle", Gone: true}), "--gone goes with wait selector and wait text, not wait idle"},
		{"wait a negative timeout", stepOf("wait", protocol.WaitArgs{For: "selector", Value: "#a", TimeoutMS: -1}), "the timeout must be positive"},
		{"wait a timeout past 300 s", stepOf("wait", protocol.WaitArgs{For: "selector", Value: "#a", TimeoutMS: 301000}), "--timeout takes up to 300 seconds"},
		{"wait a pause of nothing", stepOf("wait", protocol.WaitArgs{For: protocol.WaitMS}), "a pause is a number of milliseconds above 0"},
		{"wait a pause past 300 s", stepOf("wait", protocol.WaitArgs{For: protocol.WaitMS, MS: 400000}), "a pause is a number of milliseconds above 0"},
		{"wait window no App or title", stepOf("wait", protocol.WaitArgs{For: protocol.WaitWindow}), "wait window: an App name or a title is required"},
		{"wait element an unknown state", stepOf("wait", protocol.WaitArgs{For: protocol.WaitElement, ElementIndex: intPtrBatch(2), State: "shiny"}), "an element waits for one of"},
		{"wait element a value for another state", stepOf("wait", protocol.WaitArgs{For: protocol.WaitElement, ElementIndex: intPtrBatch(2), State: "enabled", Value: "x"}), "only value takes a value"},
		{"wait element no element", stepOf("wait", protocol.WaitArgs{For: protocol.WaitElement, State: "enabled"}), "wait: an element index, a name:/id: reference or @x,y coordinates are required"},

		// The reads' flags.
		{"get-window-state --screenshot with --root", stepOf("get-window-state", windowRef{Screenshot: true, Root: "5"}), "--screenshot goes with a full read"},
		{"get-window-state --diff with --skeleton", stepOf("get-window-state", windowRef{Diff: true, Skeleton: true}), "--diff compares a full read"},
		{"get-window-state a negative --depth", stepOf("get-window-state", windowRef{Depth: -1}), "--depth takes a positive number"},
		{"get-window-state --skeleton with --root", stepOf("get-window-state", windowRef{Skeleton: true, Root: "5"}), "--skeleton reads the whole Window and --root one Element"},
		{"find --first and --count", stepOf("find", protocol.FindArgs{First: true, Count: true}), "pass one of --first, --limit and --count"},
		{"find an unknown state", stepOf("find", protocol.FindArgs{State: "shiny"}), `find: unknown state "shiny"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Two good acting steps, then the bad one last: the Batch must
			// fail before step 1 sends anything. (Not headed: a headed
			// Batch refuses --post steps for that alone.)
			spy := newBatchSpy()
			spy.point = uia.Point{X: 100, Y: 200}
			_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
				Window: "101",
				Steps: []protocol.Step{
					stepOf("click", protocol.ClickArgs{ElementIndex: intPtrBatch(1)}),
					stepOf("type-text", protocol.TypeTextArgs{Text: "hello"}),
					tc.step,
				},
			})
			if cmdErr == nil {
				t.Fatal("a Batch with a bad step value must fail")
			}
			prefix := "batch: Action 3 (" + tc.step.Kind + "): "
			if !strings.HasPrefix(cmdErr.Msg, prefix) || !strings.Contains(cmdErr.Msg, tc.want) || !strings.HasSuffix(cmdErr.Msg, "; nothing was sent") {
				t.Errorf("message = %q, want %q naming the step, with %q, ending with nothing was sent", cmdErr.Msg, prefix, tc.want)
			}
			if cmdErr.Delivery != protocol.DeliveryNotDelivered {
				t.Errorf("delivery = %q, want %q", cmdErr.Delivery, protocol.DeliveryNotDelivered)
			}
			if cmdErr.Out != "" {
				t.Errorf("out = %q, want no step lines: no step ran", cmdErr.Out)
			}
			if got := spy.recorded(); len(got) != 0 {
				t.Errorf("pipeline steps = %v, want none: a Batch with a bad value never acts", got)
			}
			if typed := spy.typedTexts(); len(typed) != 0 {
				t.Errorf("typed %q, want nothing", typed)
			}
		})
	}
}

func TestBatchStepChecksStillRunAtTheStepsTurnWhenSentAlone(t *testing.T) {
	// The same checks are the standalone commands' own: one sent alone
	// fails with the message the Batch names.
	spy := newBatchSpy()
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "wait", protocol.WaitArgs{Window: "101", For: "selector", Value: "#a", TimeoutMS: 301000})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, "--timeout takes up to 300 seconds") {
		t.Fatalf("wait = %v, want the timeout bound", cmdErr)
	}
	_, cmdErr = runCommand(t, CommandsWith(spy.svc), "click", protocol.ClickArgs{Window: "101", Ref: "what:OK", Headed: true})
	if cmdErr == nil || !strings.Contains(cmdErr.Msg, `invalid element reference "what:OK"`) {
		t.Fatalf("click = %v, want the malformed reference refused", cmdErr)
	}
	if got := spy.recorded(); len(got) != 0 {
		t.Errorf("pipeline steps = %v, want none: a malformed reference is refused before the pipeline", got)
	}
}

func TestBatchLiveChecksStayAtTheStepsTurn(t *testing.T) {
	// Whether an Element is still there depends on the Window: the Batch
	// runs the steps before it, and that step fails at its turn.
	spy := newBatchSpy()
	spy.setValueErr = uia.ErrStale
	_, cmdErr := runCommand(t, CommandsWith(spy.svc), "batch", protocol.BatchArgs{
		Window: "101",
		Headed: true,
		Steps: []protocol.Step{
			stepOf("type-text", protocol.TypeTextArgs{Text: "hello"}),
			stepOf("set-value", protocol.SetValueArgs{ElementIndex: intPtrBatch(2), Value: "x"}),
		},
	})
	if cmdErr == nil {
		t.Fatal("a step whose Element is gone must fail the Batch")
	}
	if !strings.HasPrefix(cmdErr.Out, "Action 1 (type-text): ok\n") {
		t.Errorf("out = %q, want step 1 to have run", cmdErr.Out)
	}
	if _, _, ok := failedStep(cmdErr, 2, "set-value"); !ok {
		t.Errorf("out = %q, want step 2 failed at its turn", cmdErr.Out)
	}
	if typed := spy.typedTexts(); len(typed) != 1 {
		t.Errorf("typed %q, want step 1's text", typed)
	}
}

func TestEveryBatchStepKindHasItsStaticChecks(t *testing.T) {
	// A new batchable Action must say what the Batch checks up front (nil:
	// nothing but its Window), so a bad value never waits for its turn.
	for _, kind := range protocol.BatchStepKinds {
		if _, ok := stepChecks[kind]; !ok {
			t.Errorf("batch step kind %q has no entry in stepChecks", kind)
		}
	}
	for kind := range stepChecks {
		if !batchableStep(kind) {
			t.Errorf("stepChecks names %q, which is no batchable Action", kind)
		}
	}
}
