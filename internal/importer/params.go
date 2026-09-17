package importer

import (
	"fmt"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/tune"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// applyParam handles one line of a dump's parameter block.
//
// A leading '%' is Fuzzball's "reset to default" marker, written for any
// parameter still holding its built-in value. The stored value on such a line
// is informational: upstream discards it and resets the parameter. We do the
// same, but compare first, because a mismatch means Emerald's default has
// drifted from Fuzzball's and the operator should know.
func applyParam(w *world.World, rep *Report, line string) error {
	if strings.HasPrefix(line, "#") {
		return nil // a comment
	}

	isDefault := strings.HasPrefix(line, string(defaultFlag))
	if isDefault {
		line = line[1:]
	}

	name, value, ok := strings.Cut(line, "=")
	if !ok {
		rep.warnf("ignoring malformed parameter line %q", line)
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	if env, dropped := tune.DroppedReplacement(name); dropped {
		rep.ParamsDropped++
		if env != "" {
			rep.warnf("parameter %q is not implemented; set %s instead", name, env)
		} else {
			rep.warnf("parameter %q is not implemented; every listener is TLS", name)
		}
		return nil
	}

	p, found := tune.Lookup(name)
	if !found {
		rep.warnf("ignoring unknown parameter %q", name)
		return nil
	}

	if isDefault {
		if got, err := p.Parse(value); err == nil {
			if p.Format(got) != p.Format(p.Default) {
				rep.warnf("parameter %q is marked as a default in the dump but "+
					"Fuzzball's default (%q) differs from Emerald's (%q); "+
					"using Emerald's",
					p.Name, p.Format(got), p.Format(p.Default))
			}
		}
		if err := w.Tune.Reset(p.Name); err != nil {
			return fmt.Errorf("resetting %q: %w", p.Name, err)
		}
		rep.ParamsReset++
		return nil
	}

	if err := w.SetTune(p.Name, value); err != nil {
		rep.warnf("ignoring parameter %q: %v", name, err)
		return nil
	}
	rep.ParamsSet++
	return nil
}

// defaultFlag is Fuzzball's TP_FLAG_DEFAULT.
const defaultFlag = '%'

// legacyPassword converts a dump's password field into a stored hash.
func legacyPassword(field string) string {
	return password.FromLegacyDump(field)
}
