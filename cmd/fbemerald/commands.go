package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/FatmanUK/fuzzball_emerald/internal/logging"
	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
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

	// M1 wires the world and the persister in here.
	<-ctx.Done()
	log.Info("shutting down")
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
	return fmt.Errorf("migrate is not implemented yet (M1)")
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
