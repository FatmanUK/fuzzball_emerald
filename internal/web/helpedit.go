package web

import (
	"net/http"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/help"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// The manual viewer and editor.
//
// A corpus is edited as one text in upstream's index format rather
// than a topic at a time. That is the format a wizard already knows,
// it is what the seed files are written in, and it means moving a
// topic or adding an alias is an edit rather than a sequence of
// operations against a form.

// helpData is what the manual page shows.
type helpData struct {
	Corpora []string
	// Corpus is the one being looked at.
	Corpus string
	// Text is its whole content in index format, or the bare body
	// for a corpus that holds a single text.
	Text string
	// Index reports that the corpus is an index-format one, so
	// the page can explain the "~" and "|" it is showing.
	Index bool
	// Topics is the topic list, for the summary beside the
	// editor.
	Topics []helpTopicRow
}

type helpTopicRow struct {
	Names  string
	Bytes  int
	Edited bool
}

// getHelp shows one corpus.
func (s *Server) getHelp(w http.ResponseWriter, r *http.Request) {
	p := s.newPage(r, "Manual")
	data, err := s.helpData(r)
	if err != nil {
		s.log.Error("reading help", "error", err)
		p.Error = "the manual could not be read"
	}
	p.Data = data
	p.Notice = r.URL.Query().Get("notice")
	s.render(w, "help.html", p)
}

// helpData loads the corpus named in the query, defaulting to help.
func (s *Server) helpData(r *http.Request) (helpData, error) {
	corpus := r.URL.Query().Get("corpus")
	if corpus == "" {
		corpus = r.PostFormValue("corpus")
	}
	if !world.KnownCorpus(corpus) {
		corpus = world.CorpusHelp
	}

	rows, err := s.store.HelpCorpus(r.Context(), corpus)
	if err != nil {
		return helpData{Corpora: world.Corpora(),
			Corpus: corpus}, err
	}

	topics := make([]world.HelpTopic, 0, len(rows))
	summary := make([]helpTopicRow, 0, len(rows))
	for _, row := range rows {
		t := world.HelpTopic{
			Corpus:   row.Corpus,
			Name:     row.Name,
			Aliases:  splitAliases(row.Aliases),
			Ord:      int(row.Ord),
			Body:     row.Body,
			Modified: row.Modified,
		}
		topics = append(topics, t)

		names := strings.Join(t.AllNames(), "|")
		if names == "" {
			names = "(header)"
		}
		summary = append(summary, helpTopicRow{
			Names:  names,
			Bytes:  len(t.Body),
			Edited: t.Modified != 0,
		})
	}

	d := helpData{
		Corpora: world.Corpora(),
		Corpus:  corpus,
		Index:   !world.SingletonCorpus(corpus),
		Topics:  summary,
	}
	if d.Index {
		d.Text = help.RenderIndex(topics)
	} else if len(topics) > 0 {
		d.Text = topics[0].Body
	}
	return d, nil
}

// postHelp saves a corpus.
//
// The whole text is reparsed and the corpus replaced, which is how
// the editor can delete a topic and reorder the rest without needing
// an operation for each. Every topic saved here is stamped as edited,
// so a later release's seeding leaves it alone.
func (s *Server) postHelp(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "malformed form", http.StatusBadRequest)
		return
	}
	corpus := r.PostFormValue("corpus")
	if !world.KnownCorpus(corpus) {
		s.fail(w, r, "help.html", "there is no corpus called "+
			corpus)
		return
	}
	text := normaliseNewlines(r.PostFormValue("text"))

	var topics []world.HelpTopic
	if world.SingletonCorpus(corpus) {
		topics = []world.HelpTopic{{Corpus: corpus, Body: text}}
	} else {
		// The info corpus has no header, so the text before
		// the first separator is not a topic there.
		header := corpus != world.CorpusInfo
		topics = help.ParseIndex(corpus, text, header)
		if len(topics) == 0 && strings.TrimSpace(text) != "" {
			s.fail(w, r, "help.html", "nothing in that text "+
				"parsed as a topic; each block after a "+
				"\"~\" line needs a line of names")
			return
		}
	}

	now := s.now().Unix()
	for i := range topics {
		topics[i].Ord = i
		topics[i].Modified = now
	}

	err := s.store.WriteHelpCorpus(r.Context(), corpus, topics)
	if err != nil {
		s.log.Error("writing help", "corpus", corpus, "error", err)
		s.fail(w, r, "help.html", "the manual could not be saved")
		return
	}
	s.audit(r, "help saved", "corpus", corpus,
		"topics", len(topics))
	s.redirectNotice(w, r, "/help?corpus="+corpus, "",
		corpus+" saved.")
}

// normaliseNewlines turns a browser's CRLF form submission into the
// line endings everything else here uses.
func normaliseNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// splitAliases reads the "|"-separated list back.
func splitAliases(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "|")
}
