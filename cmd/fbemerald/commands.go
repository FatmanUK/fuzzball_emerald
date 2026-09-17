package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/logging"
	"github.com/FatmanUK/fuzzball_emerald/internal/store"
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

	w := world.New()
	loadStart := time.Now()
	rep, err := st.Load(ctx, w)
	if err != nil {
		return fmt.Errorf("loading the world: %w", err)
	}
	log.Info("world loaded",
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

	// M3 starts the listeners here; until then the engine just runs.
	if err := engine.Run(ctx); err != nil {
		return err
	}
	log.Info("stopped cleanly")
	return nil
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

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: fbemerald import [flags] <dump.db>\n\n")
		fs.PrintDefaults()
	}
	c, err := loadConfig(fs, args)
	if err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("expected exactly one dump file")
	}
	_ = c
	return fmt.Errorf("import is not implemented yet (M2)")
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
