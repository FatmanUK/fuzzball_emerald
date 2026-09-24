package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// SavePrograms writes program sources, replacing any already stored.
func (s *Store) SavePrograms(ctx context.Context, progs map[ref.Ref]string) error {
	if len(progs) == 0 {
		return nil
	}
	rows := make([]Program, 0, len(progs))
	for r, src := range progs {
		rows = append(rows, Program{Ref: int32(r), Source: src})
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).
		CreateInBatches(rows, 100).Error
	if err != nil {
		return fmt.Errorf("writing program sources: %w", err)
	}
	return nil
}

// SaveMacros replaces the macro table wholesale. It is small and only
// ever changes as a unit, so there is nothing to gain from tracking
// individual entries.
func (s *Store) SaveMacros(ctx context.Context, macros []Macro) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&Macro{}).Error; err != nil {
			return fmt.Errorf("clearing macros: %w", err)
		}
		if len(macros) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(macros, 200).Error; err != nil {
			return fmt.Errorf("writing macros: %w", err)
		}
		return nil
	})
}

// LoadMacros reads the macro table.
func (s *Store) LoadMacros(ctx context.Context) ([]Macro, error) {
	var out []Macro
	if err := s.db.WithContext(ctx).Order("name").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("loading macros: %w", err)
	}
	return out, nil
}

// Reset empties every table. It exists so an import can replace a
// world outright; nothing in the running server calls it.
func (s *Store) Reset(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, m := range allModels {
			if err := tx.Where("1 = 1").Delete(m).Error; err != nil {
				return fmt.Errorf("clearing the database: %w", err)
			}
		}
		return nil
	})
}

// LoadHelp reads every help corpus into the world, without marking
// any of it for writing.
func (s *Store) LoadHelp(ctx context.Context,
	w *world.World) (int, error) {

	var rows []HelpTopic
	err := s.db.WithContext(ctx).Order("corpus, ord").
		Find(&rows).Error
	if err != nil {
		return 0, fmt.Errorf("loading help topics: %w", err)
	}
	byCorpus := map[string][]world.HelpTopic{}
	for _, r := range rows {
		byCorpus[r.Corpus] = append(byCorpus[r.Corpus],
			world.HelpTopic{
				Corpus:   r.Corpus,
				Name:     r.Name,
				Aliases:  splitAliases(r.Aliases),
				Ord:      int(r.Ord),
				Body:     r.Body,
				Modified: r.Modified,
			})
	}
	for corpus, topics := range byCorpus {
		w.SetHelp(corpus, topics)
	}
	return len(rows), nil
}

// splitAliases reads back the "|"-separated alias list. An empty
// string means none, which strings.Split would render as one empty
// alias.
func splitAliases(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "|")
}

// Meta reads one scalar from the meta table, reporting whether it was
// set.
func (s *Store) Meta(ctx context.Context,
	key string) (string, bool, error) {

	var row Meta
	err := s.db.WithContext(ctx).First(&row, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false,
			fmt.Errorf("reading %s: %w", key, err)
	}
	return row.Value, true, nil
}

// SetMeta writes one scalar into the meta table.
func (s *Store) SetMeta(ctx context.Context,
	key, value string) error {

	return writeMeta(s.db.WithContext(ctx), key, value)
}

// HelpSeedVersion reports which release of the built-in help content
// was last seeded, and SetHelpSeedVersion records a fresh one.
func (s *Store) HelpSeedVersion(ctx context.Context) (string, error) {
	v, _, err := s.Meta(ctx, metaHelpSeed)
	return v, err
}

// SetHelpSeedVersion records the seeded content's version.
func (s *Store) SetHelpSeedVersion(ctx context.Context,
	v string) error {

	return s.SetMeta(ctx, metaHelpSeed, v)
}
