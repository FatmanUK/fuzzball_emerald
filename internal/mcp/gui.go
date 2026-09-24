package mcp

import (
	"fmt"
	"sync"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
)

// GUIPackage is the MCP package that draws dialogs on a client, from
// include/mcpgui.h.
const GUIPackage = "org-fuzzball-gui"

// Dialog is one open dialog on one connection.
//
// A dialog's controls hold values the client edits, and a program
// reads them back when the user presses a button. The values live
// here rather than in the program because the client may change them
// at any time, including while the program that opened the dialog is
// suspended.
type Dialog struct {
	ID string
	// Descr is the connection the dialog is shown on.
	Descr int
	// Dismissed records that the client has closed the dialog.
	Dismissed bool

	// values maps a control id to its lines, in the order the
	// client sent them.
	values map[string][]string
	// order keeps the control ids in the order they were first
	// set, so a program reading the whole dialog gets a stable
	// answer.
	order []string

	// OnEvent is called when the client reports a control event,
	// and OnError when it reports a failure. Either may be nil.
	OnEvent func(d *Dialog, ctrl, event string, dismissed bool)
	OnError func(d *Dialog, ctrl, code, text string)
}

// Value returns one line of a control's value.
func (d *Dialog) Value(ctrl string, line int) (string, bool) {
	lines, ok := d.values[ascii.Fold(ctrl)]
	if !ok || line < 0 || line >= len(lines) {
		return "", false
	}
	return lines[line], true
}

// Lines returns every line of a control's value.
func (d *Dialog) Lines(ctrl string) ([]string, bool) {
	lines, ok := d.values[ascii.Fold(ctrl)]
	return lines, ok
}

// Controls names the controls that have a value, in the order they
// first got one.
func (d *Dialog) Controls() []string { return d.order }

// SetValue records a control's value.
func (d *Dialog) SetValue(ctrl string, lines []string) {
	k := ascii.Fold(ctrl)
	if _, known := d.values[k]; !known {
		d.order = append(d.order, ctrl)
	}
	d.values[k] = lines
}

// Dialogs holds every open dialog on a server.
//
// Dialog ids are handed out here rather than per connection because a
// program names a dialog by id alone: it does not say which
// connection it meant, and the id has to be enough to find it.
type Dialogs struct {
	mu   sync.Mutex
	next int
	byID map[string]*Dialog
}

// NewDialogs returns an empty registry.
func NewDialogs() *Dialogs {
	return &Dialogs{byID: map[string]*Dialog{}}
}

// New opens a dialog on a connection and returns it.
func (r *Dialogs) New(descr int) *Dialog {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	d := &Dialog{
		ID:     fmt.Sprintf("%08X-%d", r.next, descr),
		Descr:  descr,
		values: map[string][]string{},
	}
	r.byID[d.ID] = d
	return d
}

// Find returns a dialog by id.
func (r *Dialogs) Find(id string) (*Dialog, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.byID[id]
	return d, ok
}

// Close forgets a dialog.
func (r *Dialogs) Close(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id]; !ok {
		return false
	}
	delete(r.byID, id)
	return true
}

// CloseDescr forgets every dialog on a connection, which is what a
// disconnection has to do: nothing is going to close them from the
// far end.
func (r *Dialogs) CloseDescr(descr int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var closed []string
	for id, d := range r.byID {
		if d.Descr == descr {
			closed = append(closed, id)
			delete(r.byID, id)
		}
	}
	return closed
}

// GUIHandler returns the handler for the GUI package, reading from
// and writing to the given registry.
func GUIHandler(dialogs *Dialogs) func(*Frame, *Message, Version) {
	return func(f *Frame, msg *Message, _ Version) {
		id, _ := msg.Arg("dlogid")
		if id == "" {
			f.sendGUIError(msg.Name, "Missing dialog ID.")
			return
		}
		dlg, ok := dialogs.Find(id)
		if !ok {
			f.sendGUIError(msg.Name, "Invalid dialog ID.")
			return
		}
		ctrl, _ := msg.Arg("id")

		switch {
		case ascii.EqualFold(msg.Name, "ctrl-value"):
			if ctrl == "" {
				f.sendGUIError(msg.Name, "Missing control ID.")
				return
			}
			lines, _ := msg.Lines("value")
			dlg.SetValue(ctrl, lines)

		case ascii.EqualFold(msg.Name, "ctrl-event"):
			if ctrl == "" {
				f.sendGUIError(msg.Name, "Missing control ID.")
				return
			}
			event, _ := msg.Arg("event")
			if event == "" {
				event = "buttonpress"
			}
			// An event dismisses the dialog unless the
			// client says otherwise, because most
			// controls are buttons and a button press
			// closes what it is on.
			dismissed := true
			if v, ok := msg.Arg("dismissed"); ok &&
				(ascii.EqualFold(v, "false") || v == "0") {
				dismissed = false
			}
			if dismissed {
				dlg.Dismissed = true
			}
			if dlg.OnEvent != nil {
				dlg.OnEvent(dlg, ctrl, event, dismissed)
			}

		case ascii.EqualFold(msg.Name, "error"):
			code, _ := msg.Arg("errcode")
			text, _ := msg.Arg("errtext")
			if dlg.OnError != nil {
				dlg.OnError(dlg, ctrl, code, text)
			}
		}
	}
}

// sendGUIError tells the client its message could not be acted on.
func (f *Frame) sendGUIError(name, text string) {
	_ = f.SendMessage(NewMessage(GUIPackage, "error").
		AddArg("errcode", name).
		AddArg("errtext", text))
}
