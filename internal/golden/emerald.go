package golden

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/game"
	"github.com/FatmanUK/fuzzball_emerald/internal/importer"
	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// RunEmerald drives this server through the same script.
//
// It runs in-process rather than over a socket: the transports are covered by
// their own tests, and what is being compared here is what the game says, not
// how it is delivered.
func RunEmerald(ctx context.Context, fx *Fixture, script Script) (string, error) {
	steps, err := RunEmeraldSteps(ctx, fx, script, nil)
	return strings.Join(steps, ""), err
}

// RunEmeraldSteps is the same, returning each command's output separately.
func RunEmeraldSteps(ctx context.Context, fx *Fixture, script Script,
	pauses map[int]time.Duration) ([]string, error) {
	res, err := importer.Load(importer.Source{
		DumpPath: fx.DumpPath,
		MufDir:   fx.MufDir,
	})
	if err != nil {
		return nil, fmt.Errorf("importing the fixture: %w", err)
	}
	w := res.World
	for _, p := range res.Programs {
		w.SetSource(p.Ref, p.Source)
	}
	macros := make([]world.Macro, 0, len(res.Macros))
	for _, m := range res.Macros {
		macros = append(macros, world.Macro{
			Name: m.Name, Definition: m.Definition, Owner: m.Owner,
		})
	}
	w.SetMacros(macros)

	// A short interval, because the tick is what wakes a sleeping program.
	engine := world.NewEngine(w, world.Options{Interval: 50 * time.Millisecond})
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- engine.Run(runCtx) }()

	gs := game.New(engine, game.Options{})
	engine.OnTick(gs.OnTick())

	d, err := gs.Connect(session.TransportLine, "golden")
	if err != nil {
		return nil, err
	}

	// Output is drained after each command rather than in the background: a
	// descriptor's Send happens on the world goroutine, so once a round trip
	// through the engine completes, everything the command produced is
	// already queued.
	drain := func() string {
		var b strings.Builder
		for {
			select {
			case line := <-d.Output():
				b.WriteString(line)
				b.WriteString("\n")
			default:
				return b.String()
			}
		}
	}
	settle := func() error {
		return engine.Do(ctx, func(*world.World) {})
	}

	gs.Input(d, "connect One "+godPassword)
	if err := settle(); err != nil {
		return nil, err
	}
	drain() // the banner and login output are not compared

	out := make([]string, 0, len(script))
	for i, cmd := range script {
		gs.Input(d, cmd)
		if err := settle(); err != nil {
			return out, err
		}
		// A program that suspends itself needs the ticks that resume it
		// to run before its output is collected.
		if pause, ok := pauses[i]; ok {
			time.Sleep(pause)
			if err := settle(); err != nil {
				return out, err
			}
		}
		out = append(out, drain())
	}

	d.Close()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		return out, fmt.Errorf("the world goroutine did not stop")
	}
	return out, nil
}
