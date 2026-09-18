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
	steps, err := RunEmeraldSteps(ctx, fx, script)
	return strings.Join(steps, ""), err
}

// RunEmeraldSteps is the same, returning each command's output separately.
func RunEmeraldSteps(ctx context.Context, fx *Fixture, script Script) ([]string, error) {
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

	engine := world.NewEngine(w, world.Options{Interval: time.Hour})
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- engine.Run(runCtx) }()

	gs := game.New(engine, game.Options{})
	macros := map[string]string{}
	for _, m := range res.Macros {
		macros[strings.ToLower(m.Name)] = m.Definition
	}
	gs.SetMacros(macros)

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
	for _, cmd := range script {
		gs.Input(d, cmd)
		if err := settle(); err != nil {
			return out, err
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
