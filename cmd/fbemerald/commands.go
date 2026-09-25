package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/admit"
	"github.com/FatmanUK/fuzzball_emerald/internal/game"
	"github.com/FatmanUK/fuzzball_emerald/internal/help"
	"github.com/FatmanUK/fuzzball_emerald/internal/importer"
	"github.com/FatmanUK/fuzzball_emerald/internal/logging"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/store"
	"github.com/FatmanUK/fuzzball_emerald/internal/transport/tlsline"
	"github.com/FatmanUK/fuzzball_emerald/internal/transport/wss"
	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	c, err := loadConfig(fs, args)
	if err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	base, err := newLogger(c)
	if err != nil {
		return err
	}
	log := logging.On(base, logging.Status)

	ctx, stop := notifyContext()
	defer stop()

	log.Info("starting",
		"version", version,
		"line_addr", c.LineAddr,
		"wss_addr", c.WSSAddr,
		"flush_interval", c.FlushInterval.String(),
		"cipher_policy", c.TLS.Policy,
	)

	st, err := store.Open(ctx, c.DatabaseURL, base)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return err
	}

	// Nothing else may be using this world. Two servers on one
	// database would each hold an authoritative in-memory graph
	// and write over each other every flush interval, so this is
	// a refusal rather than a warning. It is a Postgres advisory
	// lock, which means a crashed server leaves nothing behind to
	// clear up.
	lease, err := st.AcquireLease(ctx)
	if err != nil {
		if errors.Is(err, store.ErrLeaseHeld) {
			return fmt.Errorf("%w: refusing to start a "+
				"second server against it", err)
		}
		return err
	}
	defer lease.Release()

	w := world.New()
	loadStart := time.Now()
	rep, err := st.Load(ctx, w)
	if err != nil {
		return fmt.Errorf("loading the world: %w", err)
	}
	progs, err := st.LoadPrograms(ctx, func(r ref.Ref, src string) error {
		w.SetSource(r, src)
		return nil
	})
	if err != nil {
		return fmt.Errorf("loading program source: %w", err)
	}

	macros, err := st.LoadMacros(ctx)
	if err != nil {
		return fmt.Errorf("loading macros: %w", err)
	}
	table := make([]world.Macro, 0, len(macros))
	for _, m := range macros {
		table = append(table, world.Macro{
			Name:       m.Name,
			Definition: m.Definition,
			Owner:      ref.Ref(m.Owner),
		})
	}
	w.SetMacros(table)

	topics, err := st.LoadHelp(ctx, w)
	if err != nil {
		return fmt.Errorf("loading help: %w", err)
	}
	// A world that has never been seeded, or that predates this
	// release's content, is brought up to date before anyone can
	// connect: the help system is useless empty, and there is no
	// game directory to drop files into.
	seeded, err := seedHelp(ctx, st, w, false)
	if err != nil {
		return err
	}
	if len(seeded) > 0 {
		log.Info("help seeded",
			"corpora", strings.Join(seeded, " "),
			"version", help.Version)
		topics, err = st.LoadHelp(ctx, w)
		if err != nil {
			return fmt.Errorf("loading help: %w", err)
		}
	}

	gripes, err := st.LoadGripes(ctx, w, world.GripeLimit)
	if err != nil {
		return fmt.Errorf("loading gripes: %w", err)
	}

	log.Info("world loaded",
		"help_topics", topics,
		"gripes", gripes,
		"programs", progs,
		"macros", len(macros),
		"objects", rep.Objects,
		"properties", rep.Properties,
		"tune_params", rep.Tune,
		"chains_repaired", rep.ChainsRepaired,
		"took", time.Since(loadStart).String(),
	)
	if rep.Objects == 0 {
		log.Warn("the database is empty; import a world with \"fbemerald import\"")
	}

	engine := world.NewEngine(w, world.Options{
		Persister: st,
		Interval:  c.FlushInterval,
		Logger:    base,
	})

	tlsConfig, err := c.BuildTLS(base)
	if err != nil {
		return err
	}

	if c.PprofAddr != "" {
		servePprof(ctx, c.PprofAddr, log)
	}

	gate := admit.New(admit.Limits{
		Total:   c.Limits.MaxConnections,
		PerHost: c.Limits.MaxPerHost,
		Rate:    c.Limits.ConnectRate,
		Window:  c.Limits.ConnectWindow,
	})
	log.Info("connection limits",
		"max_connections", c.Limits.MaxConnections,
		"max_per_host", c.Limits.MaxPerHost,
		"connect_rate", c.Limits.ConnectRate,
		"connect_window", c.Limits.ConnectWindow.String(),
	)

	game.Version = version
	gs := game.New(engine, game.Options{Logger: base})

	engine.OnTick(gs.OnTick())
	engine.OnEachOp(gs.OnTick())

	// Run the world first: the listeners enqueue work onto it
	// from their own goroutines, so it has to be draining before
	// they accept anyone.
	//
	// The world's context is deliberately *not* derived from the
	// signal context. A signal has to reach the world in two
	// steps — say goodbye, then stop — and deriving it would
	// cancel both at once, racing the farewell against the drain
	// that makes sending impossible.
	runCtx, stopWorld := context.WithCancel(context.Background())
	defer stopWorld()
	gs.OnShutdown(stopWorld)

	worldDone := make(chan error, 1)
	go func() { worldDone <- engine.Run(runCtx) }()

	var listeners []listener
	if c.LineAddr != "" {
		ls, err := tlsline.New(c.LineAddr, tlsConfig, gs, base, gate)
		if err != nil {
			stopWorld()
			<-worldDone
			return fmt.Errorf("listening on %s: %w", c.LineAddr, err)
		}
		log.Info("listening", "transport", "tls", "addr", ls.Addr().String())
		listeners = append(listeners, ls)
	}
	if c.WSSAddr != "" {
		ls, err := wss.New(c.WSSAddr, tlsConfig, gs, wss.Options{
			Path:   c.WSSPath,
			Logger: base,
			Gate:   gate,
		})
		if err != nil {
			stopWorld()
			<-worldDone
			return fmt.Errorf("listening on %s: %w", c.WSSAddr, err)
		}
		log.Info("listening", "transport", "wss",
			"addr", ls.Addr().String(), "path", c.WSSPath)
		listeners = append(listeners, ls)
	}

	for _, ls := range listeners {
		go func(ls listener) {
			if err := ls.Serve(runCtx); err != nil {
				log.Error("listener stopped", "error", err)
			}
		}(ls)
	}

	// On a signal, tell everyone still connected before stopping
	// the world: AnnounceShutdown waits for the message to be
	// queued, and only then is the drain allowed to begin.
	// @shutdown reaches the same two steps from the other
	// direction, having already announced before calling
	// stopWorld itself.
	go func() {
		<-ctx.Done()
		log.Info("signal received, shutting down")
		gs.AnnounceShutdown()
		stopWorld()
	}()

	// The world goroutine returning is what ends the server: it
	// happens on a signal, or when a wizard types @shutdown.
	err = <-worldDone
	for _, ls := range listeners {
		_ = ls.Close()
	}
	if err != nil {
		return err
	}
	log.Info("stopped cleanly")
	return nil
}

// servePprof runs the profiling endpoints on a loopback address.
//
// Config.Validate has already refused anything that is not loopback.
// There is no authentication beyond that: the handlers are a
// debugging aid for someone who is already on the host, reached
// through an SSH tunnel rather than exposed.
func servePprof(ctx context.Context, addr string, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	go func() {
		log.Info("pprof listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			log.Error("pprof listener stopped", "error", err)
		}
	}()
}

// listener is what both transports provide.
type listener interface {
	Serve(context.Context) error
	Close() error
	Addr() net.Addr
}

func cmdMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	c, err := loadConfig(fs, args)
	if err != nil {
		return err
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("no database URL (set FBE_DATABASE_URL)")
	}
	base, err := newLogger(c)
	if err != nil {
		return err
	}

	ctx, stop := notifyContext()
	defer stop()

	st, err := store.Open(ctx, c.DatabaseURL, base)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return err
	}
	logging.On(base, logging.Status).Info("schema is up to date")
	return nil
}

// cmdHelpSeed writes the built-in help texts into an existing
// database, which is how a world picks up a release's new content
// without restarting the server -- and, with -force, how a wizard who
// has made a mess of the manual gets it back.
func cmdHelpSeed(args []string) error {
	fs := flag.NewFlagSet("help-seed", flag.ContinueOnError)
	force := fs.Bool("force", false,
		"replace every topic, including ones edited here")
	c, err := loadConfig(fs, args)
	if err != nil {
		return err
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf(
			"no database URL (set FBE_DATABASE_URL)")
	}
	base, err := newLogger(c)
	if err != nil {
		return err
	}
	log := logging.On(base, logging.Status)

	ctx, stop := notifyContext()
	defer stop()

	st, err := store.Open(ctx, c.DatabaseURL, base)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return err
	}

	w := world.New()
	if _, err := st.LoadHelp(ctx, w); err != nil {
		return err
	}
	changed, err := seedHelp(ctx, st, w, *force)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		log.Info("help is already up to date")
		return nil
	}
	log.Info("help seeded", "corpora", strings.Join(changed, " "),
		"version", help.Version)
	return nil
}

// seedHelp applies the built-in content and writes what changed.
//
// It is shared by the serve path and the help-seed subcommand, and
// writes through the store directly rather than through the engine:
// at boot there is no engine yet, and the subcommand has no server at
// all.
func seedHelp(ctx context.Context, st *store.Store, w *world.World,
	force bool) ([]string, error) {

	seeded, err := st.HelpSeedVersion(ctx)
	if err != nil {
		return nil, err
	}
	changed := help.Apply(w, seeded, force)
	if len(changed) == 0 {
		// The version still has to be recorded, or every boot
		// re-runs the merge to reach the same answer.
		if seeded == help.Version {
			return nil, nil
		}
		return nil, st.SetHelpSeedVersion(ctx, help.Version)
	}

	snap := w.TakeSnapshot()
	if err := st.Flush(ctx, snap); err != nil {
		return nil, fmt.Errorf("writing seeded help: %w", err)
	}
	err = st.SetHelpSeedVersion(ctx, help.Version)
	if err != nil {
		return nil, err
	}
	return changed, nil
}

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: fbemerald import [flags] <dump.db>

Loads a legacy Fuzzball database into Postgres. Program sources and the macro
table are read from a muf/ directory beside the dump.

`)
		fs.PrintDefaults()
	}
	mufDir := fs.String("muf-dir", "", "directory of <dbref>.m sources (default: found beside the dump)")
	force := fs.Bool("force", false, "replace an existing world instead of refusing")
	dryRun := fs.Bool("dry-run", false, "read and report, but write nothing")

	c, err := loadConfig(fs, args)
	if err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one dump file")
	}
	dumpPath := fs.Arg(0)

	base, err := newLogger(c)
	if err != nil {
		return err
	}
	log := logging.On(base, logging.Status)

	ctx, stop := notifyContext()
	defer stop()

	// Read the whole world before touching the database, so a
	// dump that turns out to be unreadable cannot leave a
	// half-replaced world behind.
	started := time.Now()
	res, err := importer.Load(importer.Source{DumpPath: dumpPath, MufDir: *mufDir})
	if err != nil {
		return err
	}
	rep := res.Report

	for _, warn := range rep.Warnings {
		log.Warn("import", "detail", warn)
	}
	log.Info("dump read",
		"objects", rep.Objects,
		"properties", rep.Properties,
		"programs", rep.Programs,
		"macros", rep.Macros,
		"params_set", rep.ParamsSet,
		"params_reset", rep.ParamsReset,
		"params_dropped", rep.ParamsDropped,
		"warnings", len(rep.Warnings),
		"took", time.Since(started).String(),
	)

	// Fuzzball lets a player with no password log in with any
	// password. Emerald refuses, so these accounts are
	// unreachable until someone sets a password on them. That is
	// a change in behaviour and needs saying plainly rather than
	// hiding in a count.
	if locked := res.PlayersWithoutPasswords(); len(locked) > 0 {
		names := make([]string, 0, len(locked))
		for _, r := range locked {
			names = append(names, fmt.Sprintf("%s (%v)", res.World.Get(r).Name, r))
		}
		log.Warn("players in this dump have no password and cannot log in; "+
			"Fuzzball would have accepted any password for them",
			"players", strings.Join(names, ", "))
	}

	if *dryRun {
		log.Info("dry run: nothing was written")
		return nil
	}

	st, err := store.Open(ctx, c.DatabaseURL, base)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return err
	}

	empty, err := st.IsEmpty(ctx)
	if err != nil {
		return err
	}
	if !empty && !*force {
		return fmt.Errorf("the database already holds a world; pass -force to replace it")
	}
	if !empty {
		log.Warn("replacing the existing world")
		if err := st.Reset(ctx); err != nil {
			return err
		}
	}

	if err := st.Flush(ctx, res.World.TakeSnapshot()); err != nil {
		return fmt.Errorf("writing the world: %w", err)
	}

	progs := make(map[ref.Ref]string, len(res.Programs))
	for _, p := range res.Programs {
		progs[p.Ref] = p.Source
	}
	if err := st.SavePrograms(ctx, progs); err != nil {
		return err
	}

	macros := make([]store.Macro, 0, len(res.Macros))
	for _, m := range res.Macros {
		macros = append(macros, store.Macro{
			Name:       m.Name,
			Definition: m.Definition,
			Owner:      int32(m.Owner),
		})
	}
	if err := st.SaveMacros(ctx, macros); err != nil {
		return err
	}

	log.Info("import complete",
		"objects", rep.Objects,
		"programs", len(progs),
		"macros", len(macros),
		"took", time.Since(started).String(),
	)
	return nil
}

func cmdTune(args []string) error {
	fs := flag.NewFlagSet("tune", flag.ContinueOnError)
	group := fs.String("group", "", "show only one group")
	groups := fs.Bool("groups", false, "list the groups and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *groups {
		for _, g := range tune.Groups() {
			fmt.Println(g)
		}
		return nil
	}

	s := tune.NewSet()
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "NAME\tTYPE\tGROUP\tVALUE")
	for _, p := range tune.Params() {
		if *group != "" && p.Group != *group {
			continue
		}
		v, _ := s.Get(p.Name)
		note := ""
		if p.Inert != "" {
			note = "  (inert: " + p.Inert + ")"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s%s\n", p.Name, p.Type, p.Group, p.Format(v), note)
	}
	return nil
}
