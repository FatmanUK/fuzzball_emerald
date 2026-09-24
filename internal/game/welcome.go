package game

import (
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/mpi"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// welcomeProplist is game.h's WELCOME_PROPLIST: the property list on
// #0 that a world uses to write its own login banner.
const welcomeProplist = "welcome"

// welcomeLines builds the banner a connection is shown, which is
// welcome_user.
//
// There are three sources, tried in upstream's order: the "welcome"
// property list on #0, then the welcome corpus — which is where
// upstream reads file_welcome_screen — and then the compiled-in
// default. A world therefore has two ways to change it without
// touching the server, and one of them is reachable from MUF.
//
// MPI runs over whichever won, when do_mpi_parsing and
// do_welcome_parsing both allow it. An evaluation that yields nothing
// falls back to the default rather than showing a blank screen, which
// is upstream's own handling of a broken banner.
func (s *Server) welcomeLines(w *world.World, d *session.Descriptor) []string {
	text, fromProplist := proplistText(w, ref.Ref(0), welcomeProplist)
	if !fromProplist {
		text, _ = w.HelpText(world.CorpusWelcome)
	}

	if text != "" && w.Tune.Bool("do_mpi_parsing") &&
		w.Tune.Bool("do_welcome_parsing") {
		text = s.parseWelcome(w, d, text)
	}
	if strings.TrimSpace(text) == "" {
		return defaultWelcome()
	}
	return splitBannerLines(text)
}

// parseWelcome evaluates the banner as MPI, as God and blessed, since
// nobody is logged in to own the evaluation.
//
// Upstream puts the descriptor number in match_cmdname and the
// hostname in match_args, so {&cmd} and {&arg} are what a banner uses
// to greet a connection. That is an odd pair of names for it, and it
// is reproduced because banners in the wild read them.
func (s *Server) parseWelcome(w *world.World, d *session.Descriptor,
	text string) string {

	env := &mpi.Env{
		Who:     mpi.Ref(w.Tune.Ref("welcome_mpi_who")),
		What:    mpi.Ref(w.Tune.Ref("welcome_mpi_what")),
		Perms:   mpi.Ref(w.Tune.Ref("welcome_mpi_what")),
		Blessed: true,
		Descr:   d.ID,
		Host:    &mpiHost{s: s, w: w},
	}
	_ = env.SetVar("how", welcomeProplist)
	_ = env.SetVar("cmd", strconv.Itoa(d.ID))
	_ = env.SetVar("arg", d.Hostname)
	return mpi.Eval(env, text)
}

// proplistText reads a property list and joins it with carriage
// returns, which is get_concat_list in its line mode. It reports
// whether there was a list at all, because an absent one is what
// makes upstream fall through to the file.
func proplistText(w *world.World, obj ref.Ref, name string) (string, bool) {
	o := w.Get(obj)
	if o == nil || o.Props == nil {
		return "", false
	}
	// Upstream's safegetprop renders whatever it finds as text,
	// so a count stored as an integer reads the same as one
	// stored as a string.
	get := func(path string) string {
		v, ok := o.Props.Get(path)
		if !ok {
			return ""
		}
		return v.StringValue()
	}

	// The count lives under "name#" or "name/#"; without either,
	// the list is measured by walking it until an item is
	// missing.
	count := 0
	for _, path := range [2]string{name + "#", name + "/#"} {
		if v := strings.TrimSpace(get(path)); v != "" {
			count, _ = strconv.Atoi(v)
			break
		}
	}
	if count <= 0 {
		for i := 1; ; i++ {
			if get(name+"#/"+strconv.Itoa(i)) == "" {
				count = i - 1
				break
			}
		}
	}
	if count <= 0 {
		return "", false
	}

	lines := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		lines = append(lines, get(name+"#/"+strconv.Itoa(i)))
	}
	return strings.Join(lines, "\r"), true
}

// splitBannerLines breaks text on either line ending, as welcome_user
// does walking the buffer a character at a time.
func splitBannerLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

// connectHelp is what "help" answers before login: upstream's
// file_connection_help, which is a different text from the banner.
//
// Emerald used to re-send the banner here, which is the one thing
// somebody typing "help" at a login screen has already read.
func (s *Server) connectHelp(w *world.World, d *session.Descriptor) {
	body, ok := w.HelpText(world.CorpusConnect)
	if !ok || strings.TrimSpace(body) == "" {
		d.Send("This content is missing - management has been notified.")
		return
	}
	for _, line := range splitBannerLines(body) {
		d.Send(line)
	}
}

// showMOTD sends the message of the day, which upstream does on every
// successful connect rather than only when asked.
func (s *Server) showMOTD(w *world.World, d *session.Descriptor) {
	body, ok := w.HelpText(world.CorpusMOTD)
	if !ok || body == "" {
		return
	}
	for _, line := range splitBannerLines(body) {
		if line == "" {
			d.Send("  ")
			continue
		}
		d.Send(line)
	}
}
