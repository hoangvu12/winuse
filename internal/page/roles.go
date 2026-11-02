package page

import "strings"

// How a CDP accessibility role renders in the Window State. The role
// strings are the ones UI Automation Elements use (wstate.RoleName), so a
// Page Element and a frame Element read alike. CDP has a few roles UIA
// has no word for; those get a plain English word of their own: heading,
// dialog, and clickable for an element the cursor-pointer pass found.

// kind says what the renderer does with a node of a role.
type kind int

const (
	wrapper   kind = iota // never a line; its children take its place
	keep                  // always a line
	keepNamed             // a line when it has a name, else a wrapper
	drop                  // never a line, and neither are its children
)

// roleRule is one row of the role table.
type roleRule struct {
	role string // the Window State role
	kind kind
}

// roleTable maps CDP role values (lower case) to their rendering. A role
// not listed is a wrapper when unnamed and renders as text or group when
// named (unknownRule).
var roleTable = map[string]roleRule{
	// Interactive.
	"button":                {"button", keep},
	"popupbutton":           {"button", keep},
	"togglebutton":          {"button", keep},
	"disclosuretriangle":    {"button", keep},
	"link":                  {"link", keep},
	"textbox":               {"edit", keep},
	"searchbox":             {"edit", keep},
	"textfield":             {"edit", keep},
	"checkbox":              {"check box", keep},
	"switch":                {"check box", keep},
	"radio":                 {"radio button", keep},
	"combobox":              {"combo box", keep},
	"textfieldwithcombobox": {"combo box", keep},
	"menulistpopup":         {"list", keep},
	"listbox":               {"list", keep},
	"option":                {"list item", keep},
	"menulistoption":        {"list item", keep},
	"listboxoption":         {"list item", keep},
	"menu":                  {"menu", keep},
	"menubar":               {"menu bar", keep},
	"menuitem":              {"menu item", keep},
	"menuitemcheckbox":      {"menu item", keep},
	"menuitemradio":         {"menu item", keep},
	"slider":                {"slider", keep},
	"spinbutton":            {"spinner", keep},
	"scrollbar":             {"scroll bar", keep},
	"tab":                   {"tab", keep},
	"tablist":               {"tab list", keep},
	"tree":                  {"tree", keep},
	"treeitem":              {"tree item", keep},

	// Structure.
	"list":                 {"list", keep},
	"listitem":             {"list item", keep},
	"heading":              {"heading", keep},
	"table":                {"table", keep},
	"grid":                 {"data grid", keep},
	"treegrid":             {"data grid", keep},
	"cell":                 {"data item", keep},
	"gridcell":             {"data item", keep},
	"columnheader":         {"header item", keep},
	"rowheader":            {"header item", keep},
	"toolbar":              {"toolbar", keep},
	"status":               {"status bar", keep},
	"progressbar":          {"progress bar", keep},
	"meter":                {"progress bar", keep},
	"tooltip":              {"tooltip", keep},
	"dialog":               {"dialog", keep},
	"alertdialog":          {"dialog", keep},
	"alert":                {"group", keep},
	"iframe":               {"pane", keep},
	"iframepresentational": {"pane", keep},
	"rootwebarea":          {"document", keep},
	"webarea":              {"document", keep},
	"statictext":           {"text", keepNamed},
	"image":                {"image", keepNamed},
	"img":                  {"image", keepNamed},
	"svgroot":              {"image", keepNamed},
	"canvas":               {"image", keepNamed},
	"video":                {"group", keepNamed},
	"audio":                {"group", keepNamed},

	// Landmarks and groups: a line only when named.
	"group":         {"group", keepNamed},
	"radiogroup":    {"group", keepNamed},
	"region":        {"group", keepNamed},
	"section":       {"group", keepNamed},
	"article":       {"group", keepNamed},
	"form":          {"group", keepNamed},
	"search":        {"group", keepNamed},
	"navigation":    {"group", keepNamed},
	"main":          {"group", keepNamed},
	"banner":        {"group", keepNamed},
	"contentinfo":   {"group", keepNamed},
	"complementary": {"group", keepNamed},
	"figure":        {"group", keepNamed},
	"tabpanel":      {"group", keepNamed},
	"details":       {"group", keepNamed},
	"application":   {"group", keepNamed},
	"document":      {"group", keepNamed},
	"feed":          {"group", keepNamed},
	"log":           {"group", keepNamed},
	"note":          {"group", keepNamed},

	// Never lines.
	"generic":          {"", wrapper},
	"none":             {"", wrapper},
	"presentation":     {"", wrapper},
	"paragraph":        {"", wrapper},
	"row":              {"", wrapper},
	"rowgroup":         {"", wrapper},
	"layouttable":      {"", wrapper},
	"layouttablerow":   {"", wrapper},
	"layouttablecell":  {"", wrapper},
	"labeltext":        {"", wrapper},
	"legend":           {"", wrapper},
	"caption":          {"", wrapper},
	"figcaption":       {"", wrapper},
	"blockquote":       {"", wrapper},
	"strong":           {"", wrapper},
	"emphasis":         {"", wrapper},
	"code":             {"", wrapper},
	"mark":             {"", wrapper},
	"time":             {"", wrapper},
	"abbr":             {"", wrapper},
	"pre":              {"", wrapper},
	"ruby":             {"", wrapper},
	"subscript":        {"", wrapper},
	"superscript":      {"", wrapper},
	"contentdeletion":  {"", wrapper},
	"contentinsertion": {"", wrapper},
	"unknown":          {"", wrapper},
	"inlinetextbox":    {"", drop},
	"linebreak":        {"", drop},
	"listmarker":       {"", drop},
	"separator":        {"", drop},
	"splitter":         {"", drop},
}

// interactiveRoles are the Window State roles of Elements an agent acts
// on. The cursor-pointer pass never adds a clickable inside one, and their
// own text never repeats under them.
var interactiveRoles = map[string]bool{
	"button": true, "link": true, "edit": true, "check box": true, "radio button": true,
	"combo box": true, "list item": true, "menu item": true, "slider": true, "spinner": true,
	"tab": true, "tree item": true, "data item": true, "header item": true, "heading": true,
}

// fieldRoles are the Window State roles whose value the Window State shows.
var fieldRoles = map[string]bool{"edit": true, "combo box": true, "spinner": true, "slider": true}

// selectableRoles report selection the way UIA's SelectionItem does.
var selectableRoles = map[string]bool{"option": true, "menulistoption": true, "listboxoption": true, "tab": true, "treeitem": true, "radio": true}

// rule looks up a CDP role.
func rule(cdpRole string) roleRule {
	if r, ok := roleTable[strings.ToLower(cdpRole)]; ok {
		return r
	}
	return roleRule{"", wrapper}
}
