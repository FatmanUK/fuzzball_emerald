package store

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
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

// TuneValues reads the stored @tune overrides. Only parameters that
// have been set are stored; everything else is at its default, which
// the tune package holds.
func (s *Store) TuneValues(ctx context.Context) (
	map[string]string, error) {

	var rows []TuneParam
	err := s.db.WithContext(ctx).Order("name").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf(
			"reading tune parameters: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Name] = r.Value
	}
	return out, nil
}

// SetTune writes one parameter's stored value.
func (s *Store) SetTune(ctx context.Context,
	name, value string) error {

	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&TuneParam{Name: name, Value: value}).Error
	if err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	return nil
}

// ResetTune removes a parameter's stored value, which puts it back to
// its default.
func (s *Store) ResetTune(ctx context.Context, name string) error {
	err := s.db.WithContext(ctx).
		Where("name = ?", name).Delete(&TuneParam{}).Error
	if err != nil {
		return fmt.Errorf("resetting %s: %w", name, err)
	}
	return nil
}

// HelpCorpus reads one corpus in order.
func (s *Store) HelpCorpus(ctx context.Context, corpus string) (
	[]HelpTopic, error) {

	var rows []HelpTopic
	err := s.db.WithContext(ctx).
		Where("corpus = ?", corpus).Order("ord").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf(
			"reading help %s: %w", corpus, err)
	}
	return rows, nil
}

// WriteHelpCorpus replaces one corpus wholesale, which is how the
// configurator saves an edited manual: the editor works on the whole
// index text, so what comes back is the corpus and not a patch.
func (s *Store) WriteHelpCorpus(ctx context.Context, corpus string,
	topics []world.HelpTopic) error {

	db := s.db.WithContext(ctx)
	return db.Transaction(func(tx *gorm.DB) error {
		return writeHelpCorpus(tx, corpus, topics)
	})
}

// Players lists every player, for the configurator's list.
func (s *Store) Players(ctx context.Context) ([]Object, error) {
	var rows []Object
	err := s.db.WithContext(ctx).
		Where("type = ?", uint8(ref.TypePlayer)).
		Order("name").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing players: %w", err)
	}
	return rows, nil
}

// ObjectByRef reads one object.
func (s *Store) ObjectByRef(ctx context.Context, r ref.Ref) (
	Object, bool, error) {

	var o Object
	err := s.db.WithContext(ctx).
		First(&o, "ref = ?", int32(r)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Object{}, false, nil
	}
	if err != nil {
		return Object{}, false,
			fmt.Errorf("reading %v: %w", r, err)
	}
	return o, true, nil
}

// PropertiesOf reads one object's properties, in path order.
func (s *Store) PropertiesOf(ctx context.Context, r ref.Ref) (
	[]Property, error) {

	var rows []Property
	err := s.db.WithContext(ctx).
		Where("ref = ?", int32(r)).Order("path").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf(
			"reading properties of %v: %w", r, err)
	}
	return rows, nil
}

// ExitDestsOf reads an exit's destinations, in order.
func (s *Store) ExitDestsOf(ctx context.Context, r ref.Ref) (
	[]ExitDest, error) {

	var rows []ExitDest
	err := s.db.WithContext(ctx).
		Where("ref = ?", int32(r)).Order("idx").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf(
			"reading exits of %v: %w", r, err)
	}
	return rows, nil
}

// ProgramSource reads one program's text.
func (s *Store) ProgramSource(ctx context.Context, r ref.Ref) (
	string, bool, error) {

	var p Program
	err := s.db.WithContext(ctx).
		First(&p, "ref = ?", int32(r)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading %v: %w", r, err)
	}
	return p.Source, true, nil
}

// NamesOf resolves a set of refs to names, for the links an object
// inspector draws. A ref with no object is left out.
func (s *Store) NamesOf(ctx context.Context, refs []ref.Ref) (
	map[ref.Ref]string, error) {

	if len(refs) == 0 {
		return map[ref.Ref]string{}, nil
	}
	ids := make([]int32, 0, len(refs))
	for _, r := range refs {
		if r >= 0 {
			ids = append(ids, int32(r))
		}
	}
	var rows []Object
	err := s.db.WithContext(ctx).Select("ref", "name").
		Where("ref IN ?", ids).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("resolving names: %w", err)
	}
	out := make(map[ref.Ref]string, len(rows))
	for _, o := range rows {
		out[ref.Ref(o.Ref)] = o.Name
	}
	return out, nil
}

// SetPassword writes one player's password hash.
func (s *Store) SetPassword(ctx context.Context, r ref.Ref,
	hash string) error {

	res := s.db.WithContext(ctx).Model(&Object{}).
		Where("ref = ? AND type = ?", int32(r),
			uint8(ref.TypePlayer)).
		Update("password_hash", hash)
	if res.Error != nil {
		return fmt.Errorf("setting the password on %v: %w",
			r, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%v is not a player", r)
	}
	return nil
}

// SetFlags writes one object's whole flag word.
func (s *Store) SetFlags(ctx context.Context, r ref.Ref,
	flags uint32) error {

	res := s.db.WithContext(ctx).Model(&Object{}).
		Where("ref = ?", int32(r)).Update("flags", flags)
	if res.Error != nil {
		return fmt.Errorf("setting flags on %v: %w",
			r, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("there is no %v", r)
	}
	return nil
}

// CountGripes is how many complaints are on record.
func (s *Store) CountGripes(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&Gripe{}).Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("counting gripes: %w", err)
	}
	return n, nil
}

// Gripes reads a page of complaints, newest first.
//
// This is the configurator's reader, not the game's: the game holds
// the recent ones in memory from boot, and the whole log is here.
func (s *Store) Gripes(ctx context.Context, offset, limit int) (
	[]Gripe, error) {

	var out []Gripe
	err := s.db.WithContext(ctx).Order("at desc, id desc").
		Offset(offset).Limit(limit).Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("reading gripes: %w", err)
	}
	return out, nil
}
