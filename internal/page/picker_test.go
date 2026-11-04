package page

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPickerPartsStandForTheirInput: the JS that reads, sets and checks a
// picker maps a part of an input's shadow tree (a spinner, the picker
// button) to the input, and the hit test does not call the input a cover
// of its own part. (The live check found "covered by <input id=d>".)
func TestPickerPartsStandForTheirInput(t *testing.T) {
	const remap = "if (r && r.host && r.host.tagName === 'INPUT') el = r.host;"
	for name, js := range map[string]string{"choice": choiceJS, "picker": pickerJS, "nativepicker": nativePickerJS} {
		if !strings.Contains(js, remap) {
			t.Errorf("%s does not map a shadow part to its input", name)
		}
	}
	if !strings.Contains(aimJS, "hit === root.host") {
		t.Error("the hit test calls an input the cover of its own shadow part")
	}
}

// TestPickerInputsAreOneSettableField (background-tabs live check 2): a
// date, time or colour input renders as one settable edit, not as the
// month, day and year spinners and the picker button of its shadow tree,
// which set-value could not write.
func TestPickerInputsAreOneSettableField(t *testing.T) {
	val := func(s string) *AXValue { b, _ := json.Marshal(s); return &AXValue{Type: "string", Value: b} }
	n := func(id, role, name string, backend int64, kids ...string) AXNode {
		return AXNode{NodeID: id, Role: val(role), Name: val(name), BackendDOMNodeID: backend, ChildIDs: kids}
	}
	for _, role := range []string{"date", "DateTime", "InputTime", "ColorWell"} {
		raw := &Raw{Main: Doc{SessionID: "S", FrameID: "F", Marks: &Marks{}, Nodes: []AXNode{
			n("1", "RootWebArea", "Form", 1, "2"),
			n("2", role, "When", 110, "3", "4", "5", "6"),
			n("3", "spinButton", "Month", 111),
			n("4", "spinButton", "Day", 112),
			n("5", "spinButton", "Year", 113),
			n("6", "button", "Show date picker", 114),
		}}}
		res := Build(raw, Limits{MaxNodes: 100, MaxDepth: 10, MaxChildren: 100, NodeLabel: 100, DepthLabel: 10})
		if len(res.Elements) != 1 || res.Elements[0].Role != "edit" || res.Elements[0].Name != "When" || res.Elements[0].Settable != "string" {
			var lines []string
			for _, e := range res.Elements {
				lines = append(lines, e.Role+" "+e.Name)
			}
			t.Errorf("%s renders as %s", role, strings.Join(lines, ", "))
			continue
		}
		if res.Origins[0].BackendNodeID != 110 {
			t.Errorf("%s: the Element is node %d, not the input", role, res.Origins[0].BackendNodeID)
		}
	}
}
