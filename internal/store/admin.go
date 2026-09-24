package store

import (
	"context"
	"fmt"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Queries for the configurator, which reads the database directly
// rather than through a world.
//
// It has to: the server it is looking at may be running, holding the
// authoritative graph in its own memory, and what the configurator
// shows is what is stored. Loading a world here would be a snapshot
// that went stale the moment it was taken.

// PlayerByName finds a player by name, case-insensitively.
//
// The SQL narrows with lower(), which is locale-aware, and the answer
// is then confirmed with ascii.EqualFold, which is not: upstream
// folds only A-Z, so "Ä" and "ä" are distinct player names and a
// database running under a Turkish locale must not be able to log
// somebody in as somebody else.
func (s *Store) PlayerByName(ctx context.Context,
	name string) (Object, bool, error) {

	var rows []Object
	err := s.db.WithContext(ctx).
		Where("type = ? AND lower(name) = lower(?)",
			uint8(ref.TypePlayer), name).
		Limit(16).Find(&rows).Error
	if err != nil {
		return Object{}, false, fmt.Errorf("finding %q: %w", name, err)
	}
	for _, r := range rows {
		if ascii.EqualFold(r.Name, name) {
			return r, true, nil
		}
	}
	return Object{}, false, nil
}

// Counts is a tally of the world by object type, for the status page.
type Counts struct {
	Rooms    int64
	Things   int64
	Exits    int64
	Players  int64
	Programs int64
	Garbage  int64
	Total    int64
}

// CountObjects tallies the world by type.
func (s *Store) CountObjects(ctx context.Context) (Counts, error) {
	var rows []struct {
		Type uint8
		N    int64
	}
	err := s.db.WithContext(ctx).Model(&Object{}).
		Select("type, count(*) as n").Group("type").
		Scan(&rows).Error
	if err != nil {
		return Counts{}, fmt.Errorf("counting objects: %w", err)
	}

	var c Counts
	for _, r := range rows {
		c.Total += r.N
		switch ref.ObjType(r.Type) {
		case ref.TypeRoom:
			c.Rooms = r.N
		case ref.TypeThing:
			c.Things = r.N
		case ref.TypeExit:
			c.Exits = r.N
		case ref.TypePlayer:
			c.Players = r.N
		case ref.TypeProgram:
			c.Programs = r.N
		case ref.TypeGarbage:
			c.Garbage = r.N
		}
	}
	return c, nil
}

// Exec runs a statement. It exists for tests that have to create and
// drop the scratch schema they run in, which is not something any
// other caller should be doing.
func (s *Store) Exec(ctx context.Context, sql string) error {
	return s.db.WithContext(ctx).Exec(sql).Error
}
