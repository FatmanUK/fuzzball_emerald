package web

import (
	"net/http"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/store"
)

// The object inspector: one object's row, its properties, where its
// references point, and a program's source.
//
// It is read-only, deliberately, even when the world is free. Editing
// an object here would mean reproducing the rules that decide what a
// change means — a location has to be threaded into a containment
// chain, an owner has to be an existing player, a flag's meaning
// depends on the type — and those rules live in the game. What this
// is for is looking at a world the server will not boot on.

// objectLink is a reference this object makes, resolved to a name.
type objectLink struct {
	Label string
	Ref   string
	Name  string
	// Missing reports a reference to an object that is not there,
	// which is exactly what somebody looking at a broken world
	// wants pointed out.
	Missing bool
}

type propRow struct {
	Path    string
	Type    string
	Value   string
	Blessed bool
}

type objectData struct {
	Found bool
	// Query is what was asked for, so the form keeps it.
	Query string

	Ref      string
	Name     string
	Type     string
	Flags    string
	Links    []objectLink
	Props    []propRow
	Created  int64
	Modified int64
	LastUsed int64
	UseCount int32

	// Dests are an exit's destinations, in order.
	Dests []objectLink
	// Source is a program's text.
	Source    string
	HasSource bool
}

// getObject shows one object.
func (s *Server) getObject(w http.ResponseWriter, r *http.Request) {
	p := s.newPage(r, "Objects")
	query := strings.TrimSpace(r.URL.Query().Get("ref"))
	data := objectData{Query: query}

	if query == "" {
		p.Data = data
		s.render(w, "objects.html", p)
		return
	}
	target, err := parseRef(query)
	if err != nil {
		p.Error = "that is not a dbref; write it as #123 or 123"
		p.Data = data
		s.render(w, "objects.html", p)
		return
	}

	full, err := s.objectData(r, target)
	if err != nil {
		s.log.Error("reading an object", "ref", target.String(),
			"error", err)
		p.Error = "that object could not be read"
	} else if !full.Found {
		p.Error = "there is no " + target.String()
	}
	full.Query = query
	p.Data = full
	s.render(w, "objects.html", p)
}

// objectData gathers everything the page shows.
func (s *Server) objectData(r *http.Request, target ref.Ref) (
	objectData, error) {

	ctx := r.Context()
	o, ok, err := s.store.ObjectByRef(ctx, target)
	if err != nil || !ok {
		return objectData{Found: false}, err
	}

	d := objectData{
		Found:    true,
		Ref:      target.String(),
		Name:     o.Name,
		Type:     ref.ObjType(o.Type).String(),
		Flags:    ref.Flags(o.Flags).Unparse(),
		Created:  o.Created,
		Modified: o.Modified,
		LastUsed: o.LastUsed,
		UseCount: o.UseCount,
	}

	// The chain fields are shown as they are stored, because that
	// is what a world being inspected for damage needs: a chain
	// that disagrees with the locations is the symptom.
	linked := []struct {
		label string
		r     int32
	}{
		{"Owner", o.Owner},
		{"Location", o.Location},
		{"Contents", o.Contents},
		{"Exits", o.Exits},
		{"Next", o.Next},
		{"Home", o.Home},
		{"Drop-to", o.Dropto},
	}
	dests, err := s.store.ExitDestsOf(ctx, target)
	if err != nil {
		return d, err
	}

	wanted := make([]ref.Ref, 0, len(linked)+len(dests))
	for _, l := range linked {
		wanted = append(wanted, ref.Ref(l.r))
	}
	for _, x := range dests {
		wanted = append(wanted, ref.Ref(x.Dest))
	}
	names, err := s.store.NamesOf(ctx, wanted)
	if err != nil {
		return d, err
	}

	link := func(label string, r int32) objectLink {
		rr := ref.Ref(r)
		name, found := names[rr]
		return objectLink{
			Label: label,
			Ref:   rr.String(),
			Name:  name,
			// The four negative dbrefs are not objects
			// and are not missing either.
			Missing: !found && rr >= 0,
		}
	}
	for _, l := range linked {
		d.Links = append(d.Links, link(l.label, l.r))
	}
	for _, x := range dests {
		d.Dests = append(d.Dests, link("", x.Dest))
	}

	rows, err := s.store.PropertiesOf(ctx, target)
	if err != nil {
		return d, err
	}
	for _, pr := range rows {
		d.Props = append(d.Props, propRow{
			Path:    pr.Path,
			Type:    props.Type(pr.Type).String(),
			Value:   propValue(pr),
			Blessed: pr.Blessed,
		})
	}

	if ref.ObjType(o.Type) == ref.TypeProgram {
		src, has, err := s.store.ProgramSource(ctx, target)
		if err != nil {
			return d, err
		}
		d.Source, d.HasSource = src, has
	}
	return d, nil
}

// propValue renders a stored property the way the property tree
// would.
func propValue(p store.Property) string {
	return props.Value{
		Type:    props.Type(p.Type),
		Str:     p.Str,
		Num:     p.Num,
		Float:   p.Float,
		Ref:     ref.Ref(p.RefVal),
		Blessed: p.Blessed,
	}.StringValue()
}
