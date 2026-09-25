package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/mpi"
	"github.com/FatmanUK/fuzzball_emerald/internal/muf"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// exec_or_notify and its two companions, property.c:2454 onwards.
//
// A message property is not always text. When its value begins with
// '@' it *names a MUF program* — "@123 some args" or "@$lib-desc
// some args" — and the property is a call rather than a
// description. Emerald evaluated MPI over such a value and printed
// the result, so a world that used the idiom showed "@$lib-desc" to
// whoever looked. That was a live divergence in look, in movement and
// in the fail/succeed predicates, not a gap opened up by adding the
// setters.
//
// Only the non-"o" messages can do this. The "o" messages — @osucc,
// @ofail, @odrop — go through parseOProp instead, which is MPI,
// pronoun substitution and a name prefix, and runs nothing.

// mesgValue reads a message property's stored value.
//
// Upstream's callers test GETSUCC/GETFAIL — whether the *property*
// is set — before deciding whether to print a default instead.
// Emerald used to test whether the evaluated text came out non-empty,
// which is a different question: an @fail that deliberately evaluates
// to nothing would still have drawn "You can't go that way." after
// it.
func mesgValue(w *world.World, obj ref.Ref, path string) (props.Value, bool) {
	o := w.Get(obj)
	if o == nil {
		return props.Value{}, false
	}
	v, ok := o.Props.Get(path)
	if !ok || v.Type != props.String || v.Str == "" {
		return props.Value{}, false
	}
	return v, true
}

// hasMesg reports whether a message property is set.
func hasMesg(w *world.World, obj ref.Ref, path string) bool {
	_, ok := mesgValue(w, obj, path)
	return ok
}

// execOrNotifyProp is exec_or_notify_prop: read a message property
// and act on it, doing nothing at all when it is not set.
//
// whatcalled is the caller context — "(@Desc)", "(@Succ)" — which
// becomes MPI's {&how} and the program's COMMAND variable.
func (s *Server) execOrNotifyProp(w *world.World, descr int,
	player, thing ref.Ref, path, whatcalled string) {

	v, ok := mesgValue(w, thing, path)
	if !ok {
		return
	}
	s.execOrNotify(w, descr, player, thing, v.Str, whatcalled, v.Blessed)
}

// execOrNotify shows one message to a player, running it as a program
// when it names one.
func (s *Server) execOrNotify(w *world.World, descr int,
	player, thing ref.Ref, message, whatcalled string, blessed bool) {

	if !strings.HasPrefix(message, "@") {
		s.send(w, player, s.evalMPI(w, descr, player, thing,
			message, blessed, mpi.Private))
		return
	}

	word, args := splitMesgWord(message[1:])
	var prog ref.Ref
	if strings.HasPrefix(word, "$") {
		// find_registered_obj, which searches _reg/ on thing
		// and then outwards through its environment — not
		// the looking player's.
		prog = match.New(w, thing, word).Registered().Result()
	} else {
		prog = ref.Ref(leadingInt(word))
	}

	o := w.Get(prog)
	if o == nil || o.Type() != ref.TypeProgram {
		// It named nothing runnable after all. Upstream
		// prints whatever followed the word *unparsed* — no
		// MPI, which its own comment calls a crazy edge case
		// and leaves alone — and the nothing-special
		// message when nothing followed it.
		if args != "" {
			s.send(w, player, args)
			return
		}
		s.send(w, player, w.Tune.String("description_default"))
		return
	}

	// The arguments are MPI-evaluated before the program sees
	// them. The program does every notification itself: this adds
	// no text of its own, not even when it produces none.
	s.runMesgProgram(w, descr, player, thing, prog, whatcalled,
		s.evalMPI(w, descr, player, thing, args, blessed, mpi.Private))
}

// runMesgProgram runs the program a message property named.
//
// It is RunLock's and INTERP's shape — a nested frame the caller
// waits on — because upstream runs it PREEMPT: the description has
// to be finished before the rest of the look prints. The instruction
// budget still applies, since Frame.Instructions accumulates across
// Run calls, so a description that loops for ever aborts rather than
// wedging the world goroutine.
//
// It is not filed as a process, for the same reason RunLock's and
// INTERP's frames are not: nothing could resume one. A program that
// blocks on READ or SLEEP here is therefore treated as finished,
// which is where this stops short of upstream.
func (s *Server) runMesgProgram(w *world.World, descr int,
	player, thing, prog ref.Ref, whatcalled, args string) {

	p, err := s.compileProgram(w, prog)
	if err != nil {
		s.send(w, player, "That program does not compile: "+
			err.Error())
		s.mufLog().Warn("compile failed",
			"program", prog.String(), "error", err.Error())
		return
	}

	host := &mufHost{s: s, w: w, caller: player}
	f := muf.NewFrame(p, host)
	// A property that runs a program runs it HARDUID, so it acts
	// as the owner of the object carrying the message rather than
	// as whoever triggered it. property.c:2528.
	f.Perms = muf.HardUID
	loc := ref.Nothing
	if me := w.Get(player); me != nil {
		loc = me.Location
	}
	// COMMAND is the caller context and the pushed argument is
	// the evaluated text — upstream's match_cmdname and
	// match_args, two different strings, which SetReserved's own
	// convention makes one. QUEUE overwrites the pushed value the
	// same way.
	f.SetReserved(player, loc, thing, whatcalled)
	f.Stack[len(f.Stack)-1] = muf.Str(args)
	f.Descr = descr
	f.Mode = muf.ModePreempt

	w.Used(prog)

	for {
		res, err := f.Run(muf.Limits{})
		if err != nil {
			s.reportMUFErrorTo(w, player, f, prog, err)
			return
		}
		if res == muf.Done || res == muf.Blocked {
			return
		}
	}
}

// canDoit is predicates.c's can_doit: could_doit, followed by
// whichever of the four messages the outcome calls for.
//
// The default failure message is only used when the caller supplies
// one. look_room passes none, so a room locked against whoever is
// looking says nothing rather than borrowing an exit's wording.
func (s *Server) canDoit(w *world.World, descr int,
	who, thing ref.Ref, defaultFail string) bool {

	me, o := w.Get(who), w.Get(thing)
	if me == nil || o == nil || me.Location == ref.Nothing {
		return false
	}
	zombie := me.Type() == ref.TypeThing &&
		o.Flags&ref.Zombie != 0
	if zombie && w.Tune.Bool("allow_zombies") &&
		!isWizard(w, ownerOf(w, who)) {
		s.notify(w, who, "Sorry, but zombies can't do that.")
		return false
	}

	// A DARK player announces nothing to the room, so the "o"
	// halves are suppressed for one.
	quiet := me.Flags&ref.Dark != 0

	if !couldDoit(s, w, descr, 1, who, thing) {
		if hasMesg(w, thing, propFail) {
			s.execOrNotifyProp(w, descr, who, thing,
				propFail, "(@Fail)")
		} else if defaultFail != "" {
			s.notify(w, who, "%s", defaultFail)
		}
		if !quiet {
			s.parseOProp(w, descr, who, me.Location,
				thing, propOFail, me.Name, "(@Ofail)")
		}
		return false
	}
	s.execOrNotifyProp(w, descr, who, thing, propSucc, "(@Succ)")
	if !quiet {
		s.parseOProp(w, descr, who, me.Location, thing,
			propOSucc, me.Name, "(@Osucc)")
	}
	return true
}

// parseOProp is parse_oprop: an "o" message, which is broadcast to
// the room prefixed with the actor's name.
//
// It differs from execOrNotify in three ways, all of them upstream's:
// it never runs a program, it is public rather than private MPI, and
// it goes through pronoun substitution before the prefix is applied.
// An empty result sends nothing at all.
func (s *Server) parseOProp(w *world.World, descr int,
	player, dest, thing ref.Ref, path, prefix, whatcalled string) {

	v, ok := mesgValue(w, thing, path)
	if !ok {
		return
	}
	// MPI_ISPUBLIC is zero upstream: public is simply the absence
	// of Private, which is what MesgType.Public reads back.
	text := s.evalMPI(w, descr, player, thing, v.Str, v.Blessed, 0)
	text = muf.PronounSub(&mufHost{s: s, w: w, caller: player},
		player, text)
	if text == "" {
		return
	}
	s.notifyRoom(w, dest, []ref.Ref{player}, "%s",
		prefixMessage(text, prefix))
}

// prefixMessage is fbstrings.c's prefix_message with its
// SuppressIfPresent argument set, which is the only way parse_oprop
// calls it.
//
// Two details are load-bearing. A line that already begins with the
// prefix followed by a pose separator is left alone, so an @osucc
// written "Name smiles." does not come out "Name Name smiles.". And
// no space is inserted before a pose separator, which is what makes
// an @osucc of "'s hat glows." read "Name's hat glows.".
func prefixMessage(text, prefix string) string {
	var b strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		rest := strings.TrimSuffix(line, "\r")
		if !hasPrefixPose(rest, prefix) {
			b.WriteString(prefix)
			if !isPoseSeparator(rest) {
				b.WriteByte(' ')
			}
		}
		b.WriteString(rest)
	}
	return b.String()
}

// hasPrefixPose reports whether a line already carries the prefix,
// which upstream tests by requiring a pose separator or the end of
// the line straight after it.
func hasPrefixPose(line, prefix string) bool {
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	return isPoseSeparator(line[len(prefix):])
}

// isPoseSeparator is is_valid_pose_separator on the first byte of s,
// treating the end of the string as one — upstream's test admits a
// trailing NUL beside the separators themselves.
func isPoseSeparator(s string) bool {
	if s == "" {
		return true
	}
	switch s[0] {
	case ' ', '\'', ',', '-':
		return true
	}
	return false
}

// splitMesgWord is exec_or_notify's own scan: everything up to the
// first space, then exactly one character skipped.
//
// It deliberately does not trim. "@ foo" has an empty first word
// upstream, so it names no program and prints "foo"; trimming would
// make it look for a program called "foo" instead.
func splitMesgWord(s string) (word, rest string) {
	i := strings.IndexAny(s, " \t\r\n\v\f")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}
