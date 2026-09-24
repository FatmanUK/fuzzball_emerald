package web

import (
	"net/http"
	"sort"

	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
)

// The @tune editor.
//
// A parameter's name is a runtime API — MUF reads them by string
// with SYSPARM — so this shows every one that exists rather than
// only the ones somebody has set, and it never invents a name.
// Validation goes through the same tune.Param.Parse the server uses,
// so a value that would not load is refused here rather than at the
// next boot.

// tuneParam is one row of the editor.
type tuneParam struct {
	Name    string
	Label   string
	Type    string
	Value   string
	Default string
	// Overridden reports that the value is stored rather than
	// left at its default.
	Overridden bool
	// Inert explains why a parameter no longer does anything, and
	// is empty for the ones that do.
	Inert string
	// GodOnly marks the parameters upstream gated behind God.
	GodOnly bool
}

// tuneGroup is the parameters under one heading.
type tuneGroup struct {
	Name   string
	Params []tuneParam
}

type tuneData struct {
	Groups []tuneGroup
	// Changed names the parameter a save just wrote, so the page
	// can point at it.
	Changed string
}

// getTune lists every parameter, grouped as tune.Groups has them.
func (s *Server) getTune(w http.ResponseWriter, r *http.Request) {
	p := s.newPage(r, "Parameters")
	data, err := s.tuneData(r)
	if err != nil {
		s.log.Error("reading tune parameters", "error", err)
		p.Error = "the parameters could not be read"
	}
	p.Data = data
	p.Notice = r.URL.Query().Get("notice")
	s.render(w, "tune.html", p)
}

// tuneData builds the grouped listing.
func (s *Server) tuneData(r *http.Request) (tuneData, error) {
	stored, err := s.store.TuneValues(r.Context())
	if err != nil {
		return tuneData{}, err
	}

	byGroup := map[string][]tuneParam{}
	for _, def := range tune.Params() {
		row := tuneParam{
			Name:    def.Name,
			Label:   def.Label,
			Type:    def.Type.String(),
			Default: def.Format(def.Default),
			Inert:   def.Inert,
			GodOnly: def.GodOnly,
		}
		if v, ok := stored[def.Name]; ok {
			row.Value = v
			row.Overridden = true
		} else {
			row.Value = row.Default
		}
		byGroup[def.Group] = append(byGroup[def.Group], row)
	}

	groups := make([]tuneGroup, 0, len(byGroup))
	for _, name := range tune.Groups() {
		rows := byGroup[name]
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].Name < rows[j].Name
		})
		groups = append(groups, tuneGroup{Name: name, Params: rows})
	}
	return tuneData{Groups: groups,
		Changed: r.URL.Query().Get("changed")}, nil
}

// postTune saves or resets one parameter.
//
// The value is parsed through the parameter's own Parse and written
// back through its Format, so what lands in the database is the
// canonical spelling of what was meant — the same round trip the
// server's own loader does, which is what stops a value that reads
// back differently from the one that was typed.
func (s *Server) postTune(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "malformed form", http.StatusBadRequest)
		return
	}
	name := r.PostFormValue("name")
	def, ok := tune.Lookup(name)
	if !ok {
		s.fail(w, r, "tune.html", "there is no parameter called "+
			name)
		return
	}

	if r.PostFormValue("reset") != "" {
		if err := s.store.ResetTune(r.Context(), def.Name); err != nil {
			s.log.Error("resetting a parameter",
				"name", def.Name, "error", err)
			s.fail(w, r, "tune.html", "the parameter could not "+
				"be reset")
			return
		}
		s.audit(r, "tune reset", "name", def.Name)
		s.redirectNotice(w, r, "/tune", def.Name,
			def.Name+" is back to its default.")
		return
	}

	raw := r.PostFormValue("value")
	v, err := def.Parse(raw)
	if err != nil {
		s.fail(w, r, "tune.html", def.Name+": "+err.Error())
		return
	}
	if err := s.store.SetTune(r.Context(), def.Name,
		def.Format(v)); err != nil {
		s.log.Error("writing a parameter", "name", def.Name,
			"error", err)
		s.fail(w, r, "tune.html", "the parameter could not be saved")
		return
	}
	s.audit(r, "tune set", "name", def.Name, "value", def.Format(v))
	s.redirectNotice(w, r, "/tune", def.Name, def.Name+" saved.")
}
