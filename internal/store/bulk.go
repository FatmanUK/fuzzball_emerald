package store

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
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
