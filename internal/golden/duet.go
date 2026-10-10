package golden

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/game"
	"github.com/FatmanUK/fuzzball_emerald/internal/importer"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// Two or three seats, which is the last class of output a one-seat
// transcript cannot reach: a line addressed to somebody else.
//
// `page`, `whisper`, `@wall`, the connect and disconnect
// announcements and the "o" half of every message property are all
// written for an audience that is not the actor — and a suite
// driving one connection can only ever see the actor's half. Four of
// the divergences this port has found were in that other half, and
// each needed a unit test standing in for a transcript.
//
// **The marker scheme is the design.** A pose is room-public, so each
// seat sees the other's; two tokens are needed, one per seat, and
// every step ends with *both* seats posing their own. A reader
// discards a line holding either, so neither stream keeps the other's
// bookkeeping — and because each seat waits for its own token
// rather than the actor's, the scheme survives the two being in
// different rooms, which is exactly where `page` is interesting.
//
// A mortal seat cannot use the `!` prefix: it requires
// `IsTrueWizard`. The second seat poses without it, which is safe in
// a fixture with no trapping exit.
//
// The quota matters at two command tokens **per seat** per step, so a
// duet turns the limiter up before it starts.
//
// A **third** seat is a second connection for a player who already
// has one, which is the only way to see three things: `@wall`'s
// per-descriptor arithmetic, the `connect` *action* firing on the
// first connection where the propqueues fire on every one, and the
// puppet wake-up that goes with it.

// Seat names one of the connections.
type Seat int

// A is the fixture's wizard; B and C are whoever the case logs in
// after it, which may be two different players or the same player
// twice.
const (
	A Seat = 0
	B Seat = 1
	C Seat = 2
)

// Step is one command, and which seat types it.
type Step struct {
	Seat Seat
	Cmd  string
}

// Duet is a two-seat script.
type Duet []Step

// Say, Hear and Also build a step for each seat, so a script reads as
// a column of who-does-what.
func Say(cmd string) Step  { return Step{Seat: A, Cmd: cmd} }
func Hear(cmd string) Step { return Step{Seat: B, Cmd: cmd} }
func Also(cmd string) Step { return Step{Seat: C, Cmd: cmd} }

// Heard is what each seat saw during one step, indexed by seat.
type Heard []string

// Login is a seat after the first: who it connects as.
type Login struct {
	Name     string
	Password string
}

// bobOnly is the common case: one extra seat, as the second player
// the setup created.
var bobOnly = []Login{{Name: "Bob", Password: "secret"}}

// duetMarkers are the tokens, one per seat.
var duetMarkers = [3]string{"EMERALDSEATA", "EMERALDSEATB",
	"EMERALDSEATC"}

// anyMarker reports whether a line carries any seat's token, so a
// reader can drop the others' bookkeeping as well as its own.
func anyMarker(line string) bool {
	for _, m := range duetMarkers {
		if strings.Contains(line, m) {
			return true
		}
	}
	return false
}

// RunOracleDuet drives the C server with two connections.
//
// setup runs on seat A alone and is not compared: it is where the
// second player is made, since `@pcreate` has to happen before
// anybody can connect as them.
func RunOracleDuet(ctx context.Context, fx *Fixture, setup Script,
	logins []Login, duet Duet) ([]Heard, error) {

	n := len(logins) + 1
	var out []Heard
	run := func(conns []net.Conn) error {
		seats := make([]*oracleSeat, n)
		for i := range conns {
			seats[i] = &oracleSeat{
				conn: conns[i],
				br:   bufio.NewReader(conns[i]),
				mark: duetMarkers[i],
				// A seat after the first may be a
				// mortal, and a mortal cannot use the
				// '!' prefix: it requires
				// IsTrueWizard. Plain "pose" is safe
				// in a fixture with no trapping exit.
				pose: "pose ",
			}
		}
		seats[0].pose = "!pose "

		if err := seats[0].login("One",
			godPassword); err != nil {
			return err
		}
		// The quota is spent four tokens a step here rather
		// than two, and a long duet will hit it.
		if err := seats[0].step("@tune " +
			"commands_per_time=1000"); err != nil {
			return err
		}
		for _, cmd := range setup {
			if err := seats[0].step(cmd); err != nil {
				return err
			}
		}
		// Each later seat connects in turn. Its own login
		// swallows its banner — which is the one thing that
		// must not be compared, since the two servers greet a
		// connection in their own words — but what the
		// seats **already open** hear is left queued, so the
		// arrival announcements land in the first step's
		// transcript on both servers and are compared there.
		// That is the only way a connect is observable at
		// all: nothing inside the duet loop can cause one.
		for i, lg := range logins {
			if err := seats[i+1].login(lg.Name,
				lg.Password); err != nil {
				return err
			}
		}

		for _, st := range duet {
			// **The acting seat closes first.** Two
			// descriptors have no ordering between them,
			// so posing both markers before reading
			// either lets the quiet seat's marker be
			// processed before the command has run -- and
			// then every line the command produced for
			// that seat lands in the *next* step. That is
			// what the first run of this case did, to
			// every step.
			//
			// Reading the actor to its own marker first
			// means the command has finished before the
			// other seat is asked anything, so whatever
			// it was told is already queued.
			act := seats[st.Seat]
			if err := act.send(st.Cmd); err != nil {
				return err
			}
			if err := act.send(act.pose +
				act.mark); err != nil {
				return err
			}
			h := make(Heard, n)
			got, err := act.readTo()
			h[st.Seat] = got
			if err != nil {
				out = append(out, h)
				return err
			}
			failed := error(nil)
			for i, quiet := range seats {
				if Seat(i) == st.Seat {
					continue
				}
				if err := quiet.send(quiet.pose +
					quiet.mark); err != nil {
					failed = err
					break
				}
				got, err := quiet.readTo()
				h[i] = got
				if err != nil {
					failed = err
					break
				}
			}
			out = append(out, h)
			if failed != nil {
				return failed
			}
		}
		_ = seats[0].send("@shutdown")
		return nil
	}
	err := withOracleConns(ctx, fx, n, run)
	return out, err
}

// oracleSeat is one connection to the C server.
type oracleSeat struct {
	conn net.Conn
	br   *bufio.Reader
	mark string
	pose string
}

func (s *oracleSeat) send(line string) error {
	_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := s.conn.Write([]byte(line + "\n"))
	return err
}

// readTo collects output until this seat's own token, dropping any
// line that carries either seat's.
func (s *oracleSeat) readTo() (string, error) {
	var got strings.Builder
	deadline := time.Now().Add(30 * time.Second)
	for {
		_ = s.conn.SetReadDeadline(deadline)
		line, err := s.br.ReadString('\n')
		if strings.Contains(line, s.mark) {
			return got.String(), nil
		}
		if !anyMarker(line) {
			got.WriteString(line)
		}
		if err != nil {
			return got.String(), err
		}
		if time.Now().After(deadline) {
			return got.String(), fmt.Errorf(
				"timed out waiting for %s", s.mark)
		}
	}
}

// login connects and swallows the banner.
func (s *oracleSeat) login(name, pass string) error {
	if err := s.send("connect " + name + " " + pass); err != nil {
		return err
	}
	if err := s.send(s.pose + s.mark); err != nil {
		return err
	}
	_, err := s.readTo()
	return err
}

// flush closes a step on this seat with no command of its own,
// discarding whatever was waiting.
func (s *oracleSeat) flush() error {
	if err := s.send(s.pose + s.mark); err != nil {
		return err
	}
	_, err := s.readTo()
	return err
}

// step runs one command on this seat alone, discarding its output.
func (s *oracleSeat) step(cmd string) error {
	if err := s.send(cmd); err != nil {
		return err
	}
	if err := s.send(s.pose + s.mark); err != nil {
		return err
	}
	_, err := s.readTo()
	return err
}

// RunEmeraldDuet is the same against this server, which needs no
// markers at all: two descriptors have their own output channels, and
// one round trip through the engine is enough to know a command has
// finished.
func RunEmeraldDuet(ctx context.Context, fx *Fixture, setup Script,
	logins []Login, duet Duet) ([]Heard, error) {

	n := len(logins) + 1

	res, err := importer.Load(importer.Source{
		DumpPath: fx.DumpPath,
		MufDir:   fx.MufDir,
	})
	if err != nil {
		return nil, fmt.Errorf(
			"importing the fixture: %w", err)
	}
	w := res.World
	for _, p := range res.Programs {
		w.SetSource(p.Ref, p.Source)
	}
	macros := make([]world.Macro, 0, len(res.Macros))
	for _, m := range res.Macros {
		macros = append(macros, world.Macro{
			Name:       m.Name,
			Definition: m.Definition,
			Owner:      m.Owner,
		})
	}
	w.SetMacros(macros)
	loadHelpData(w)

	engine := world.NewEngine(w, world.Options{
		Interval: 50 * time.Millisecond,
	})
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	gs := game.New(engine, game.Options{})
	engine.OnTick(gs.OnTick())
	engine.OnEachOp(gs.OnTick())

	done := make(chan error, 1)
	go func() { done <- engine.Run(runCtx) }()

	ds := make([]*session.Descriptor, n)
	for i := range ds {
		d, err := gs.Connect(session.TransportLine, "golden")
		if err != nil {
			return nil, err
		}
		ds[i] = d
	}
	settle := func() error {
		return engine.Do(ctx, func(*world.World) {})
	}
	drain := func(d *session.Descriptor) string {
		var b strings.Builder
		for {
			select {
			case line := <-d.Output():
				if anyMarker(line) {
					continue
				}
				b.WriteString(line)
				b.WriteString("\n")
			default:
				return b.String()
			}
		}
	}

	gs.Input(ds[0], "connect One "+godPassword)
	if err := settle(); err != nil {
		return nil, err
	}
	drain(ds[0])
	for _, cmd := range append(Script{
		"@tune commands_per_time=1000"}, setup...) {

		gs.Input(ds[0], cmd)
		if err := settle(); err != nil {
			return nil, err
		}
		drain(ds[0])
	}
	// Only the connecting seat's own stream is discarded, for the
	// reason the oracle side gives: its banner is the one thing
	// the two servers write differently. What the seats already
	// open hear is left for the first step's transcript.
	for i, lg := range logins {
		gs.Input(ds[i+1], "connect "+lg.Name+" "+
			lg.Password)
		if err := settle(); err != nil {
			return nil, err
		}
		drain(ds[i+1])
	}

	out := make([]Heard, 0, len(duet))
	for _, st := range duet {
		gs.Input(ds[st.Seat], st.Cmd)
		if err := settle(); err != nil {
			return out, err
		}
		h := make(Heard, n)
		for i := range ds {
			h[i] = drain(ds[i])
		}
		out = append(out, h)
	}

	for _, d := range ds {
		d.Close()
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		return out, fmt.Errorf(
			"the world goroutine did not stop")
	}
	return out, nil
}

// CompareDuets diffs two transcripts seat by seat, naming which seat
// a difference is in — because a line in the wrong seat's stream is
// the whole class of bug this exists to find.
func CompareDuets(t interface {
	Errorf(string, ...any)
}, duet Duet, oracle, emerald []Heard) {

	names := [3]string{"seat A", "seat B", "seat C"}
	for i, st := range duet {
		var want, got Heard
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		for seat := 0; seat < len(want) ||
			seat < len(got); seat++ {
			var w, g string
			if seat < len(want) {
				w = want[seat]
			}
			if seat < len(got) {
				g = got[seat]
			}
			diffs := Compare(w, g)
			if len(diffs) == 0 {
				continue
			}
			t.Errorf("%s heard %q differently\n%s",
				names[seat], st.Cmd, Render(diffs))
		}
	}
}
