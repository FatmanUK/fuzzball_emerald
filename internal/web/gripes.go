package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// The gripe log.
//
// Upstream keeps complaints in the file named by file_log_gripes, and
// a wizard reads them with a bare "gripe" — which spits the whole
// file, however long it has grown. Emerald keeps them as rows, and
// the game holds only the most recent world.GripeLimit in memory, so
// this is where the rest of the history is. It is read-only for the
// same reason the object inspector is: nothing here should be able to
// rewrite what somebody reported.

// gripesPerPage is how many are shown at once. It is a page rather
// than the lot because this is the one table that only ever grows.
const gripesPerPage = 50

type gripeRow struct {
	When     string
	Who      string
	WhoRef   string
	Where    string
	WhereRef string
	Message  string
}

type gripesData struct {
	Rows  []gripeRow
	Total int64
	// Older and Newer are offsets, or -1 when there is no such
	// page, so the template can hide the link rather than render
	// a dead one.
	Older int
	Newer int
}

// getGripes lists the complaints, newest first.
func (s *Server) getGripes(w http.ResponseWriter, r *http.Request) {
	p := s.newPage(r, "Gripes")

	offset, _ := strconv.Atoi(r.URL.Query().Get("from"))
	if offset < 0 {
		offset = 0
	}

	ctx := r.Context()
	total, err := s.store.CountGripes(ctx)
	if err != nil {
		s.log.Error("counting gripes", "error", err)
		p.Error = "the gripe log could not be read"
		p.Data = gripesData{Older: -1, Newer: -1}
		s.render(w, "gripes.html", p)
		return
	}
	rows, err := s.store.Gripes(ctx, offset, gripesPerPage)
	if err != nil {
		s.log.Error("reading gripes", "error", err)
		p.Error = "the gripe log could not be read"
		p.Data = gripesData{Older: -1, Newer: -1}
		s.render(w, "gripes.html", p)
		return
	}

	data := gripesData{Total: total, Older: -1, Newer: -1}
	for _, g := range rows {
		data.Rows = append(data.Rows, gripeRow{
			When: time.Unix(g.At, 0).
				Format("2006-01-02 15:04:05"),
			Who:      g.WhoName,
			WhoRef:   ref.Ref(g.Who).String(),
			Where:    g.WhereName,
			WhereRef: ref.Ref(g.Where).String(),
			Message:  g.Message,
		})
	}
	if offset > 0 {
		data.Newer = max(0, offset-gripesPerPage)
	}
	if int64(offset+gripesPerPage) < total {
		data.Older = offset + gripesPerPage
	}

	p.Data = data
	s.render(w, "gripes.html", p)
}
