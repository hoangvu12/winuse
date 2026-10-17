// Package keys parses Codex Computer Use's key names for press-key: X
// Window System keysym-style names such as `a`, `space`, `Return`, `Tab`
// or `Control_L+a`, with Codex's documented aliases (Control, Ctrl, Alt,
// period, greater, Numpad_0, ...). Parsing is pure — no Win32 — so the
// offline tests cover the whole accepted surface.
//
// The name set is Codex's recovered alias table (codex-surface.md §3):
// matching is case-insensitive, `-` is treated as `_`, a single letter or
// digit maps to its own key, and shifted punctuation names (greater,
// question, colon, ...) map to the *unshifted* virtual key — Codex's rule
// that shifted punctuation needs an explicit Shift
// (Control_L+Shift_L+period). Raw single-character punctuation (".", ",")
// is accepted too and maps the same way; raw shifted digits ("!") map to
// their unshifted digit. Whitespace around a chord's "+" separators is
// ignored.
//
// The Windows/Super/Meta family (Win, Windows, Meta, Command, Cmd, Super,
// OS, each with _L/_R) is recognized only to be refused: winuse never
// sends the Windows key (Codex's documented rule).
package keys

import (
	"fmt"
	"strings"
)

// Windows virtual-key codes the name table needs (Win32 VK_* values).
const (
	vkCancel     = 0x03
	vkBack       = 0x08
	vkTab        = 0x09
	vkClear      = 0x0C
	vkReturn     = 0x0D
	vkModechange = 0x1F
	vkEscape     = 0x1B
	vkSpace      = 0x20
	vkPrior      = 0x21
	vkNext       = 0x22
	vkEnd        = 0x23
	vkHome       = 0x24
	vkLeft       = 0x25
	vkUp         = 0x26
	vkRight      = 0x27
	vkDown       = 0x28
	vkSelect     = 0x29
	vkPrint      = 0x2A
	vkExecute    = 0x2B
	vkSnapshot   = 0x2C
	vkInsert     = 0x2D
	vkDelete     = 0x2E
	vkHelp       = 0x2F
	vkApps       = 0x5D
	vkNumpad0    = 0x60
	vkMultiply   = 0x6A
	vkAdd        = 0x6B
	vkSeparator  = 0x6C
	vkSubtract   = 0x6D
	vkDecimal    = 0x6E
	vkDivide     = 0x6F
	vkF1         = 0x70
	vkNumlock    = 0x90
	vkOemNEC     = 0x92 // VK_OEM_NEC_EQUAL: the numpad "=" key
	vkScroll     = 0x91
	vkLshift     = 0xA0
	vkRshift     = 0xA1
	vkLcontrol   = 0xA2
	vkRcontrol   = 0xA3
	vkLmenu      = 0xA4
	vkRmenu      = 0xA5
	vkOem1       = 0xBA // ;:
	vkOemPlus    = 0xBB // =+
	vkOemComma   = 0xBC // ,<
	vkOemMinus   = 0xBD // -_
	vkOemPeriod  = 0xBE // .>
	vkOem2       = 0xBF // /?
	vkOem3       = 0xC0 // `~
	vkOem4       = 0xDB // [{
	vkOem5       = 0xDC // \|
	vkOem6       = 0xDD // ]}
	vkOem7       = 0xDE // '"
)

// Key is one key of a chord, parsed from a Codex/X keysym name. VK is the
// Windows virtual key to inject; Extended marks the keys that need
// KEYEVENTF_EXTENDEDKEY (arrows, Insert/Delete/Home/End/PgUp/PgDn, right
// Ctrl/Alt, numpad Divide/Enter, NumLock, PrintScreen, Apps, Menu) so
// arrows arrive as arrows, not numpad keys. Name is the canonical X
// keysym-style name (for messages and tests).
type Key struct {
	Name     string
	VK       uint16
	Extended bool
}

// Chord is a parsed key chord: one or more keys. Delivery order is part of
// the chord: every key goes down in the given order, then up in reverse
// (Codex's chord delivery, codex-surface.md §3).
type Chord []Key

// String renders the chord back in press-key syntax.
func (c Chord) String() string {
	names := make([]string, len(c))
	for i, k := range c {
		names[i] = k.Name
	}
	return strings.Join(names, "+")
}

// Kind classifies a parse failure.
type Kind int

const (
	// ErrEmpty: the key is missing or blank ("" or whitespace).
	ErrEmpty Kind = iota
	// ErrUnknown: not an accepted key name.
	ErrUnknown
	// ErrWindowsKey: a Windows/Super/Meta key name — recognized, and
	// always refused.
	ErrWindowsKey
)

// Error is a key-name parse failure. Key holds the offending name as
// typed ("" when the key was empty).
type Error struct {
	Key  string
	Kind Kind
}

func (e *Error) Error() string {
	switch e.Kind {
	case ErrEmpty:
		return "key is required"
	case ErrWindowsKey:
		return "unsupported key: " + e.Key + " (the Windows key is never sent)"
	default:
		return "unsupported key: " + e.Key
	}
}

// keySpec is one accepted name's mapping.
type keySpec struct {
	name     string
	vk       uint16
	extended bool
}

// aliases is Codex's alias table: every accepted multi-character name
// (uppercased, with - replaced by _) to its key. A group of aliases
// (CONTROL/CTRL/CONTROL_L/CTRL_L) shares one key, exactly like Codex's
// recovered alias blob.
var aliases = map[string]keySpec{
	// Modifiers. CONTROL_L/CTRL_L are Control; CONTROL_R/CTRL_R are the
	// right Control (an extended key).
	"CONTROL":   {name: "Control_L", vk: vkLcontrol},
	"CTRL":      {name: "Control_L", vk: vkLcontrol},
	"CONTROL_L": {name: "Control_L", vk: vkLcontrol},
	"CTRL_L":    {name: "Control_L", vk: vkLcontrol},
	"CONTROL_R": {name: "Control_R", vk: vkRcontrol, extended: true},
	"CTRL_R":    {name: "Control_R", vk: vkRcontrol, extended: true},
	"SHIFT":     {name: "Shift_L", vk: vkLshift},
	"SHIFT_L":   {name: "Shift_L", vk: vkLshift},
	"SHIFT_R":   {name: "Shift_R", vk: vkRshift},
	"ALT":       {name: "Alt_L", vk: vkLmenu},
	"OPTION":    {name: "Alt_L", vk: vkLmenu},
	"ALT_L":     {name: "Alt_L", vk: vkLmenu},
	"OPTION_L":  {name: "Alt_L", vk: vkLmenu},
	"ALT_R":     {name: "Alt_R", vk: vkRmenu, extended: true},
	"OPTION_R":  {name: "Alt_R", vk: vkRmenu, extended: true},

	// Editing and navigation. LINEFEED has no Windows key of its own; it
	// shares Return. KP_ENTER/NUMPAD_ENTER are the numpad Enter (the
	// extended Enter). BEGIN is Home (X's Begin).
	"ENTER":        {name: "Return", vk: vkReturn},
	"RETURN":       {name: "Return", vk: vkReturn},
	"LINEFEED":     {name: "Return", vk: vkReturn},
	"KP_ENTER":     {name: "KP_Enter", vk: vkReturn, extended: true},
	"NUMPAD_ENTER": {name: "KP_Enter", vk: vkReturn, extended: true},
	"NUM_ENTER":    {name: "KP_Enter", vk: vkReturn, extended: true},
	"TAB":          {name: "Tab", vk: vkTab},
	"CLEAR":        {name: "Clear", vk: vkClear},
	"SPACE":        {name: "space", vk: vkSpace},
	"ESC":          {name: "Escape", vk: vkEscape},
	"ESCAPE":       {name: "Escape", vk: vkEscape},
	"BACKSPACE":    {name: "BackSpace", vk: vkBack},
	"DELETE":       {name: "Delete", vk: vkDelete, extended: true},
	"DEL":          {name: "Delete", vk: vkDelete, extended: true},
	"INSERT":       {name: "Insert", vk: vkInsert, extended: true},
	"HOME":         {name: "Home", vk: vkHome, extended: true},
	"BEGIN":        {name: "Home", vk: vkHome, extended: true},
	"END":          {name: "End", vk: vkEnd, extended: true},
	"PRIOR":        {name: "Prior", vk: vkPrior, extended: true},
	"PAGEUP":       {name: "Prior", vk: vkPrior, extended: true},
	"PAGE_UP":      {name: "Prior", vk: vkPrior, extended: true},
	"NEXT":         {name: "Next", vk: vkNext, extended: true},
	"PAGEDOWN":     {name: "Next", vk: vkNext, extended: true},
	"PAGE_DOWN":    {name: "Next", vk: vkNext, extended: true},
	"UP":           {name: "Up", vk: vkUp, extended: true},
	"DOWN":         {name: "Down", vk: vkDown, extended: true},
	"LEFT":         {name: "Left", vk: vkLeft, extended: true},
	"RIGHT":        {name: "Right", vk: vkRight, extended: true},

	// System keys. MENU is the Application/Menu key. SYS_REQ is the
	// PrintScreen key's X name. CANCEL and BREAK share VK_CANCEL (the
	// Break key). MODE_SWITCH and SCRIPT_SWITCH share VK_MODECHANGE.
	// SHIFT_LOCK shares Caps_Lock.
	"MENU":          {name: "Menu", vk: vkApps, extended: true},
	"HELP":          {name: "Help", vk: vkHelp, extended: true},
	"SELECT":        {name: "Select", vk: vkSelect, extended: true},
	"PRINT":         {name: "Print", vk: vkPrint, extended: true},
	"SYS_REQ":       {name: "Sys_Req", vk: vkSnapshot, extended: true},
	"EXECUTE":       {name: "Execute", vk: vkExecute, extended: true},
	"CANCEL":        {name: "Cancel", vk: vkCancel},
	"BREAK":         {name: "Cancel", vk: vkCancel},
	"MODE_SWITCH":   {name: "Mode_switch", vk: vkModechange},
	"SCRIPT_SWITCH": {name: "Mode_switch", vk: vkModechange},
	"PAUSE":         {name: "Pause", vk: 0x13},
	"SCROLL_LOCK":   {name: "Scroll_Lock", vk: vkScroll},
	"NUM_LOCK":      {name: "Num_Lock", vk: vkNumlock, extended: true},
	"CAPS_LOCK":     {name: "Caps_Lock", vk: 0x14},
	"SHIFT_LOCK":    {name: "Caps_Lock", vk: 0x14},

	// Punctuation. Shifted names (greater, question, colon, ...) map to
	// the *unshifted* virtual key: add an explicit Shift to type the
	// shifted character (Codex's documented rule).
	"PLUS":          {name: "plus", vk: vkOemPlus},
	"EQUAL":         {name: "equal", vk: vkOemPlus},
	"EQUALS":        {name: "equal", vk: vkOemPlus},
	"MINUS":         {name: "minus", vk: vkOemMinus},
	"HYPHEN":        {name: "minus", vk: vkOemMinus},
	"UNDERSCORE":    {name: "underscore", vk: vkOemMinus},
	"COMMA":         {name: "comma", vk: vkOemComma},
	"LESS":          {name: "less", vk: vkOemComma},
	"PERIOD":        {name: "period", vk: vkOemPeriod},
	"DOT":           {name: "period", vk: vkOemPeriod},
	"FULL_STOP":     {name: "period", vk: vkOemPeriod},
	"GREATER":       {name: "greater", vk: vkOemPeriod},
	"SLASH":         {name: "slash", vk: vkOem2},
	"FORWARD_SLASH": {name: "slash", vk: vkOem2},
	"QUESTION":      {name: "question", vk: vkOem2},
	"SEMICOLON":     {name: "semicolon", vk: vkOem1},
	"COLON":         {name: "colon", vk: vkOem1},
	"APOSTROPHE":    {name: "apostrophe", vk: vkOem7},
	"QUOTE":         {name: "apostrophe", vk: vkOem7},
	"SINGLE_QUOTE":  {name: "apostrophe", vk: vkOem7},
	"DOUBLE_QUOTE":  {name: "quotedbl", vk: vkOem7},
	"QUOTEDBL":      {name: "quotedbl", vk: vkOem7},
	"GRAVE":         {name: "grave", vk: vkOem3},
	"BACKTICK":      {name: "grave", vk: vkOem3},
	"TILDE":         {name: "asciitilde", vk: vkOem3},
	"ASCIITILDE":    {name: "asciitilde", vk: vkOem3},
	"BRACKETLEFT":   {name: "bracketleft", vk: vkOem4},
	"BRACKETRIGHT":  {name: "bracketright", vk: vkOem6},
	"BRACELEFT":     {name: "braceleft", vk: vkOem4},
	"BRACERIGHT":    {name: "braceright", vk: vkOem6},
	"BACKSLASH":     {name: "backslash", vk: vkOem5},
	"BAR":           {name: "bar", vk: vkOem5},
	"PIPE":          {name: "bar", vk: vkOem5},

	// Raw shifted digits and their X keysym names map to the unshifted
	// digit key (the same rule as the shifted punctuation names).
	"EXCLAM":      {name: "exclam", vk: 0x31},
	"AT":          {name: "at", vk: 0x32},
	"NUMBERSIGN":  {name: "numbersign", vk: 0x33},
	"DOLLAR":      {name: "dollar", vk: 0x34},
	"PERCENT":     {name: "percent", vk: 0x35},
	"ASCIICIRCUM": {name: "asciicircum", vk: 0x36},
	"AMPERSAND":   {name: "ampersand", vk: 0x37},
	"ASTERISK":    {name: "asterisk", vk: 0x38},
	"PARENLEFT":   {name: "parenleft", vk: 0x39},
	"PARENRIGHT":  {name: "parenright", vk: 0x30},

	// Numpad keys, the three alias spellings (KP_*, NUMPAD_*, NUM_*).
	// Digits are generated below. KP_Divide, like the numpad Enter, is an
	// extended key. KP_EQUAL is VK_OEM_NEC_EQUAL (0x92), the only Windows
	// virtual key for a numpad "=". KP_DELETE is the numpad Del (the
	// Delete key with NumLock off).
	"KP_MULTIPLY":      {name: "KP_Multiply", vk: vkMultiply},
	"NUMPAD_MULTIPLY":  {name: "KP_Multiply", vk: vkMultiply},
	"NUM_MULTIPLY":     {name: "KP_Multiply", vk: vkMultiply},
	"KP_ADD":           {name: "KP_Add", vk: vkAdd},
	"NUMPAD_ADD":       {name: "KP_Add", vk: vkAdd},
	"NUM_ADD":          {name: "KP_Add", vk: vkAdd},
	"KP_SUBTRACT":      {name: "KP_Subtract", vk: vkSubtract},
	"NUMPAD_SUBTRACT":  {name: "KP_Subtract", vk: vkSubtract},
	"NUM_SUBTRACT":     {name: "KP_Subtract", vk: vkSubtract},
	"KP_DIVIDE":        {name: "KP_Divide", vk: vkDivide, extended: true},
	"NUMPAD_DIVIDE":    {name: "KP_Divide", vk: vkDivide, extended: true},
	"NUM_DIVIDE":       {name: "KP_Divide", vk: vkDivide, extended: true},
	"KP_DECIMAL":       {name: "KP_Decimal", vk: vkDecimal},
	"NUMPAD_DECIMAL":   {name: "KP_Decimal", vk: vkDecimal},
	"NUM_DECIMAL":      {name: "KP_Decimal", vk: vkDecimal},
	"KP_SEPARATOR":     {name: "KP_Separator", vk: vkSeparator},
	"NUMPAD_SEPARATOR": {name: "KP_Separator", vk: vkSeparator},
	"NUM_SEPARATOR":    {name: "KP_Separator", vk: vkSeparator},
	"KP_EQUAL":         {name: "KP_Equal", vk: vkOemNEC},
	"NUMPAD_EQUAL":     {name: "KP_Equal", vk: vkOemNEC},
	"NUM_EQUAL":        {name: "KP_Equal", vk: vkOemNEC},
	"KP_DELETE":        {name: "KP_Delete", vk: vkDelete, extended: true},
	"NUMPAD_DELETE":    {name: "KP_Delete", vk: vkDelete, extended: true},
	"NUM_DELETE":       {name: "KP_Delete", vk: vkDelete, extended: true},
}

// charKeys maps raw single-character punctuation to its unshifted key —
// the raw-character aliases of the names above (Codex's alias blob ends
// with the raw ";", "`" and "~"). Shifted digits map to their digit key,
// like the shifted punctuation names map to unshifted keys.
var charKeys = map[byte]keySpec{
	'=':  {name: "equal", vk: vkOemPlus},
	'+':  {name: "plus", vk: vkOemPlus},
	'-':  {name: "minus", vk: vkOemMinus},
	'_':  {name: "underscore", vk: vkOemMinus},
	',':  {name: "comma", vk: vkOemComma},
	'<':  {name: "less", vk: vkOemComma},
	'.':  {name: "period", vk: vkOemPeriod},
	'>':  {name: "greater", vk: vkOemPeriod},
	'/':  {name: "slash", vk: vkOem2},
	'?':  {name: "question", vk: vkOem2},
	';':  {name: "semicolon", vk: vkOem1},
	':':  {name: "colon", vk: vkOem1},
	'\'': {name: "apostrophe", vk: vkOem7},
	'"':  {name: "quotedbl", vk: vkOem7},
	'`':  {name: "grave", vk: vkOem3},
	'~':  {name: "asciitilde", vk: vkOem3},
	'[':  {name: "bracketleft", vk: vkOem4},
	']':  {name: "bracketright", vk: vkOem6},
	'{':  {name: "braceleft", vk: vkOem4},
	'}':  {name: "braceright", vk: vkOem6},
	'\\': {name: "backslash", vk: vkOem5},
	'|':  {name: "bar", vk: vkOem5},
	'!':  {name: "exclam", vk: 0x31},
	'@':  {name: "at", vk: 0x32},
	'#':  {name: "numbersign", vk: 0x33},
	'$':  {name: "dollar", vk: 0x34},
	'%':  {name: "percent", vk: 0x35},
	'^':  {name: "asciicircum", vk: 0x36},
	'&':  {name: "ampersand", vk: 0x37},
	'*':  {name: "asterisk", vk: 0x38},
	'(':  {name: "parenleft", vk: 0x39},
	')':  {name: "parenright", vk: 0x30},
}

// windowsKeyNames is the Windows/Super/Meta family: recognized only to be
// refused. winuse never sends the Windows key (Codex's rule: never press
// Meta, Windows, Win, Super or OS).
var windowsKeyNames = map[string]bool{}

func init() {
	// Numpad digits: KP_0..KP_9, Numpad_0.., Num_0.. all name the numpad
	// digit keys (distinct from the main-row digits, which is the point
	// of the KP_ names).
	for i := 0; i <= 9; i++ {
		d := fmt.Sprintf("%d", i)
		spec := keySpec{name: "KP_" + d, vk: vkNumpad0 + uint16(i)}
		aliases["KP_"+d] = spec
		aliases["NUMPAD_"+d] = spec
		aliases["NUM_"+d] = spec
	}
	// Function keys F1..F20 (Codex's range; F21+ is unsupported).
	for i := 1; i <= 20; i++ {
		f := fmt.Sprintf("F%d", i)
		aliases[f] = keySpec{name: f, vk: vkF1 + uint16(i-1)}
	}
	// The Windows-key family, bare and with _L/_R.
	for _, base := range []string{"WIN", "WINDOWS", "META", "COMMAND", "CMD", "SUPER", "OS"} {
		windowsKeyNames[base] = true
		windowsKeyNames[base+"_L"] = true
		windowsKeyNames[base+"_R"] = true
	}
}

// Parse parses a key or `+`-separated chord in Codex's key naming: one or
// more names, in press order (whitespace around "+" is ignored). A single
// key returns a one-key chord. Unknown names, the Windows-key family and
// empty names fail with *Error; the caller turns those into the
// user-facing messages.
func Parse(s string) (Chord, error) {
	if strings.TrimSpace(s) == "" {
		return nil, &Error{Kind: ErrEmpty}
	}
	parts := strings.Split(s, "+")
	chord := make(Chord, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, &Error{Kind: ErrEmpty}
		}
		key, err := parseKey(name)
		if err != nil {
			return nil, err
		}
		chord = append(chord, key)
	}
	return chord, nil
}

// parseKey resolves one key name. A single letter or digit maps to its own
// key; everything else is canonicalized (case-insensitive, `-` as `_`)
// and looked up.
func parseKey(name string) (Key, error) {
	if len(name) == 1 {
		c := name[0]
		switch {
		case c >= 'a' && c <= 'z':
			return Key{Name: name, VK: uint16(c - 'a' + 'A')}, nil
		case c >= 'A' && c <= 'Z':
			return Key{Name: strings.ToLower(name), VK: uint16(c)}, nil
		case c >= '0' && c <= '9':
			return Key{Name: name, VK: uint16(c)}, nil // VK 0x30..0x39
		default:
			if spec, ok := charKeys[c]; ok {
				return Key{Name: spec.name, VK: spec.vk, Extended: spec.extended}, nil
			}
		}
	}
	canonical := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	if windowsKeyNames[canonical] {
		return Key{}, &Error{Key: name, Kind: ErrWindowsKey}
	}
	if spec, ok := aliases[canonical]; ok {
		return Key{Name: spec.name, VK: spec.vk, Extended: spec.extended}, nil
	}
	return Key{}, &Error{Key: name, Kind: ErrUnknown}
}
