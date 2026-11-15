package helper

// set-value on a Page's custom ARIA list (learn-from 05; page/aria.go):
// a Radix or Headless UI select, a typeahead combobox, a listbox. Before,
// set-value refused such a widget ("takes no typed text"), and picking an
// option took three turns: click it open, read, click the option.
//
//	widget                                  set-value <widget> <option name>
//	combobox or aria-haspopup=listbox       a trusted click opens it unless aria-expanded=true,
//	button (a trigger)                      then the option's trusted click, read back
//	combobox text field (typeahead)         the text typed (focus, select all, insertText), then
//	                                        the option's trusted click, read back; with no list
//	                                        shown it is a plain text field, read back as one
//	listbox                                 the option's trusted click, read back
//
// Every click is the Headless Page input a click uses (page.Input), so the
// page sees a person's pointer events. The option is named by its name
// exactly, else ignoring case (page.PickARIAOption); several matches or
// none are an error that names the options, with "nothing was sent" when
// winuse had not yet opened or typed into the widget. A list winuse opened
// and then found no option in stays open (the error says so), so a retry
// with the right name needs no second click. An option already selected
// in a multi-select list, or in a listbox named itself, is left alone: a
// click would turn it off.

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hoangvu12/winuse/internal/page"
	"github.com/hoangvu12/winuse/internal/win"
)

// pageSetARIA runs set-value on a custom list (Choice Kind "aria").
func (s Services) pageSetARIA(ctx context.Context, w win.WindowInfo, index int, o *page.Origin, target elementTarget, ch page.Choice, value string) (string, *Error) {
	id := strconv.FormatUint(w.ID, 10)
	if page.Collapse(value) == "" {
		return "", nothingSent(Fail(fmt.Sprintf("cannot set element %d: it is a list, and set-value picks one of its options by name", index),
			"winuse set-value "+id+" "+target.token()+" <option name>"))
	}
	opens := !ch.Editable && !ch.List && !ch.Expanded
	aim := page.AimOptions{Scroll: true}
	if opens {
		// The trigger takes a click. An open one is not hit-tested: its
		// popup may cover it, or keep the pointer off the rest of the page.
		aim = page.AimOptions{Scroll: true, Enabled: true, Visible: true, HitTest: true}
	}
	var notes []string
	pick := ariaPick{s: s, w: w, index: index, o: o, target: target, ch: ch, notes: &notes}
	out, rerr := s.runPage(ctx, pageRun{
		noun: "choice", done: "set", verb: "select", w: w, index: index, origin: o, target: target,
		check: func(ctx context.Context) error {
			if ch.Disabled {
				return &page.NotActionableError{Why: page.WhyDisabled}
			}
			return nil
		},
		aim: aim,
		act: func(ctx context.Context, in *page.Input, aim page.Aim) error {
			before, err := o.ReadARIAList(ctx, "before")
			if err != nil {
				return err
			}
			pick.before = before
			if ch.Editable {
				return pick.typeAndPick(ctx, in, value)
			}
			if opens {
				if err := in.Click(ctx, aim.Point, page.ButtonLeft, 1); err != nil {
					return err
				}
			}
			list, i, err := o.WaitARIAOption(ctx, value, false)
			if err != nil {
				return pick.missing(err, in.Sent, "the click opened its list, which is still open")
			}
			return pick.click(ctx, in, list, i)
		},
	})
	if rerr != nil || out != okOutput || len(notes) == 0 {
		return out, rerr
	}
	return okWith(notes...), nil
}

// ariaPick is one set-value on a custom list, past the read before it.
type ariaPick struct {
	s      Services
	w      win.WindowInfo
	index  int
	o      *page.Origin
	target elementTarget
	ch     page.Choice
	before page.ARIAList
	notes  *[]string
}

// typeAndPick types the text into a typeahead combobox, then clicks the
// option it names. A field that shows no list is a plain text field, read
// back as one; one whose list has no such option keeps the text, with a
// note naming the options.
func (p ariaPick) typeAndPick(ctx context.Context, in *page.Input, value string) error {
	f := page.Field{Kind: p.ch.Field}
	if err := p.o.ReplaceText(ctx, in, value); err != nil {
		return err
	}
	list, i, err := p.o.WaitARIAOption(ctx, value, true)
	var oe *page.OptionError
	var nl *page.NoListError
	switch {
	case err == nil && i < 0:
		return p.o.CheckValue(ctx, f, value) // no list: free text
	case errors.As(err, &oe) && !oe.Several, errors.As(err, &nl):
		if verr := p.o.CheckValue(ctx, f, value); verr != nil {
			return verr
		}
		*p.notes = append(*p.notes, fmt.Sprintf("element %d holds the text typed, but no option was picked: %s", p.index, err.Error()))
		return nil
	case err != nil:
		return p.missing(err, in.Sent, "the text was typed into it, and its list is open")
	}
	return p.click(ctx, in, list, i)
}

// missing maps a list without the option asked for: a refusal while
// nothing was sent, else a delivered failure saying what was done.
func (p ariaPick) missing(err error, sent int, done string) error {
	var oe *page.OptionError
	var nl *page.NoListError
	hint := "name one option exactly as the list shows it"
	switch {
	case errors.As(err, &oe):
	case errors.As(err, &nl):
		hint = "click it, read the Window State, and click its option"
		if nl.Several == 0 {
			done = "the click was delivered, and no list showed"
		}
	default:
		return err
	}
	msg := fmt.Sprintf("cannot set element %d: %s", p.index, err.Error())
	if sent == 0 {
		return &actRefusal{msg, hint}
	}
	return &page.VerifyError{Msg: msg + "; " + done}
}

// click clicks option i of the list and reads the choice back.
func (p ariaPick) click(ctx context.Context, in *page.Input, list page.ARIAList, i int) error {
	opt := list.Options[i]
	id := strconv.FormatUint(p.w.ID, 10)
	refuse := func(msg, hint string) error {
		if in.Sent == 0 {
			return &actRefusal{msg, hint}
		}
		return &page.VerifyError{Msg: msg + "; its list was opened or typed into, and is still open"}
	}
	if opt.Disabled {
		return refuse(fmt.Sprintf("cannot set element %d: its option %q is disabled", p.index, opt.Name), "pick another option")
	}
	if opt.Selected && (list.Multi || p.ch.List) {
		// A click would turn it off (a multi-select list) or change nothing.
		if in.Sent > 0 {
			*p.notes = append(*p.notes, fmt.Sprintf("the option %q was already selected, so winuse left it; its list is still open", opt.Name))
		}
		return nil
	}
	oo, err := p.o.ARIAOptionOrigin(ctx, i, opt.Name)
	if err != nil {
		return err
	}
	oaim, err := oo.Aim(ctx, page.AimOptions{Scroll: true, Enabled: true, Visible: true, HitTest: true})
	var na *page.NotActionableError
	if errors.As(err, &na) {
		return refuse(fmt.Sprintf("cannot set element %d: its option %q cannot be clicked: %s", p.index, opt.Name, na.Error()), notActionableHint(na, id, p.target))
	}
	if err != nil {
		return err
	}
	// After the click that opened the list, the Agent Cursor goes on to the
	// option; otherwise it stays on the widget, which ripples on this click.
	var rect win.Rect
	shown := false
	if in.Sent > 0 {
		if r, ok := p.s.pageScreenRect(ctx, p.w, &oo, oaim.Rect); ok {
			rect, shown = p.s.cursorTravelRect(ctx, p.w, r, cursorLabel("select", opt.Name))
		}
	}
	if err := in.Click(ctx, oaim.Point, page.ButtonLeft, 1); err != nil {
		return err
	}
	if shown {
		p.s.cursorClick(ctx, p.w, rect)
	}
	if !p.o.WaitARIAChosen(ctx, oo, p.ch.Editable, p.before, opt, opt.Name) {
		return &page.VerifyError{Msg: fmt.Sprintf("the option %q was clicked, but element %d does not show it chosen afterwards (the page may take its choice another way, such as by keys)", opt.Name, p.index)}
	}
	return nil
}
