package store

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// Store is a Postgres-backed persister.
type Store struct {
	db  *gorm.DB
	log *slog.Logger
}

// Open connects to Postgres. It does not create any schema; call
// Migrate.
func Open(ctx context.Context, dsn string, log *slog.Logger) (*Store, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// GORM's own logging duplicates ours and defaults to
		// stdout.
		Logger: logger.Discard,
		// The world goroutine assigns every ref, so GORM has
		// no reason to ask Postgres what a write produced.
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	// One writer goroutine plus the occasional load means a small
	// pool is plenty. The ninth is for the liveness lease, which
	// pins a connection for the life of the process and would
	// otherwise be taking a slot the writer needs.
	sqlDB.SetMaxOpenConns(9)
	sqlDB.SetMaxIdleConns(4)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}
	return &Store{db: db, log: log}, nil
}

// Close releases the connection pool.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Migrate creates or updates the schema.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.db.WithContext(ctx).AutoMigrate(allModels...); err != nil {
		return fmt.Errorf("migrating schema: %w", err)
	}
	return nil
}

// IsEmpty reports whether the database holds no objects, which is how
// the server decides whether it needs a world imported.
func (s *Store) IsEmpty(ctx context.Context) (bool, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&Object{}).Count(&n).Error; err != nil {
		return false, err
	}
	return n == 0, nil
}

// Flush writes a snapshot in one transaction, so a crash mid-write
// leaves the previous state intact rather than a half-applied batch.
func (s *Store) Flush(ctx context.Context, snap world.Snapshot) error {
	if snap.Empty() {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := writeObjects(tx, snap.Objects); err != nil {
			return err
		}
		if err := deleteObjects(tx, snap.Deleted); err != nil {
			return err
		}
		if err := writeTune(tx, snap.Tune); err != nil {
			return err
		}
		if err := writePrograms(tx, snap.Programs); err != nil {
			return err
		}
		if err := writeMacros(tx, snap.Macros); err != nil {
			return err
		}
		if err := writeHelp(tx, snap.Help); err != nil {
			return err
		}
		if err := writeGripes(tx, snap.Gripes); err != nil {
			return err
		}
		return writeMeta(tx, metaTop, fmt.Sprint(int32(snap.Top)))
	})
}

func writeObjects(tx *gorm.DB, objs []*world.Object) error {
	if len(objs) == 0 {
		return nil
	}

	rows := make([]Object, 0, len(objs))
	refs := make([]int32, 0, len(objs))
	var propRows []Property
	var destRows []ExitDest

	for _, o := range objs {
		rows = append(rows, toRow(o))
		refs = append(refs, int32(o.Ref))

		if o.Props != nil {
			for _, e := range o.Props.All() {
				if len(e.Path) > maxPathLen {
					return fmt.Errorf("object %v: property path is %d bytes, over the %d limit: %q",
						o.Ref, len(e.Path), maxPathLen, e.Path[:64])
				}
				propRows = append(propRows, toPropRow(o.Ref, e))
			}
		}
		for i, d := range o.Dest {
			destRows = append(destRows, ExitDest{
				Ref:  int32(o.Ref),
				Idx:  int32(i),
				Dest: int32(d),
			})
		}
	}

	if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).
		CreateInBatches(rows, 500).Error; err != nil {
		return fmt.Errorf("writing objects: %w", err)
	}

	// Properties and destinations are replaced wholesale for each
	// object in the batch. Tracking individual property changes
	// would save writes on objects with large property trees, but
	// it is not worth the bookkeeping until a profile says so.
	if err := tx.Where("ref IN ?", refs).Delete(&Property{}).Error; err != nil {
		return fmt.Errorf("clearing properties: %w", err)
	}
	if len(propRows) > 0 {
		if err := tx.CreateInBatches(propRows, 1000).Error; err != nil {
			return fmt.Errorf("writing properties: %w", err)
		}
	}

	if err := tx.Where("ref IN ?", refs).Delete(&ExitDest{}).Error; err != nil {
		return fmt.Errorf("clearing exit destinations: %w", err)
	}
	if len(destRows) > 0 {
		if err := tx.CreateInBatches(destRows, 1000).Error; err != nil {
			return fmt.Errorf("writing exit destinations: %w", err)
		}
	}
	return nil
}

func deleteObjects(tx *gorm.DB, refs []ref.Ref) error {
	if len(refs) == 0 {
		return nil
	}
	ids := make([]int32, len(refs))
	for i, r := range refs {
		ids[i] = int32(r)
	}
	for _, m := range []any{&Property{}, &ExitDest{}, &Program{}, &Object{}} {
		if err := tx.Where("ref IN ?", ids).Delete(m).Error; err != nil {
			return fmt.Errorf("deleting objects: %w", err)
		}
	}
	return nil
}

func writeTune(tx *gorm.DB, params map[string]string) error {
	if params == nil {
		return nil
	}
	rows := make([]TuneParam, 0, len(params))
	for k, v := range params {
		rows = append(rows, TuneParam{Name: k, Value: v})
	}
	if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).
		CreateInBatches(rows, 500).Error; err != nil {
		return fmt.Errorf("writing tune parameters: %w", err)
	}
	return nil
}

// writePrograms saves the MUF source of programs changed since the
// last flush. Only edited programs appear here, so a world with
// thousands of programs writes nothing until someone saves one.
func writePrograms(tx *gorm.DB, sources map[ref.Ref]string) error {
	if len(sources) == 0 {
		return nil
	}
	rows := make([]Program, 0, len(sources))
	for r, src := range sources {
		rows = append(rows, Program{Ref: int32(r), Source: src})
	}
	if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).
		CreateInBatches(rows, 200).Error; err != nil {
		return fmt.Errorf("writing program source: %w", err)
	}
	return nil
}

// writeMacros replaces the editor's macro table. It is written whole,
// because it is small and a deletion has to be visible.
func writeMacros(tx *gorm.DB, macros []world.Macro) error {
	if macros == nil {
		return nil
	}
	if err := tx.Where("1 = 1").Delete(&Macro{}).Error; err != nil {
		return fmt.Errorf("clearing macros: %w", err)
	}
	if len(macros) == 0 {
		return nil
	}
	rows := make([]Macro, 0, len(macros))
	for _, m := range macros {
		rows = append(rows, Macro{
			Name:       m.Name,
			Definition: m.Definition,
			Owner:      int32(m.Owner),
		})
	}
	if err := tx.CreateInBatches(rows, 200).Error; err != nil {
		return fmt.Errorf("writing macros: %w", err)
	}
	return nil
}

// writeGripes appends the complaints made since the last flush.
//
// This is the one thing in a snapshot that is added rather than
// replaced, because a gripe log is append-only: nothing edits or
// deletes a complaint, so there is nothing for a whole-table rewrite
// to make visible.
func writeGripes(tx *gorm.DB, gripes []world.Gripe) error {
	if len(gripes) == 0 {
		return nil
	}
	rows := make([]Gripe, 0, len(gripes))
	for _, g := range gripes {
		rows = append(rows, Gripe{
			At:        g.When,
			Who:       int32(g.Who),
			WhoName:   g.WhoName,
			Where:     int32(g.Where),
			WhereName: g.WhereName,
			Message:   g.Message,
		})
	}
	if err := tx.CreateInBatches(rows, 200).Error; err != nil {
		return fmt.Errorf("writing gripes: %w", err)
	}
	return nil
}

// writeHelp replaces each changed corpus. A corpus is written whole
// for the reason the macro table is: a deleted topic has to
// disappear, and there is no per-row deletion to track. Corpora that
// did not change are not touched, so a motd append leaves the rest
// alone.
func writeHelp(tx *gorm.DB, help map[string][]world.HelpTopic) error {
	for corpus, topics := range help {
		err := writeHelpCorpus(tx, corpus, topics)
		if err != nil {
			return err
		}
	}
	return nil
}

func writeHelpCorpus(tx *gorm.DB, corpus string,
	topics []world.HelpTopic) error {

	err := tx.Where("corpus = ?", corpus).
		Delete(&HelpTopic{}).Error
	if err != nil {
		return fmt.Errorf("clearing help %s: %w", corpus, err)
	}
	if len(topics) == 0 {
		return nil
	}
	rows := make([]HelpTopic, 0, len(topics))
	for _, t := range topics {
		rows = append(rows, HelpTopic{
			Corpus:   corpus,
			Name:     t.Name,
			Aliases:  strings.Join(t.Aliases, "|"),
			Ord:      int32(t.Ord),
			Body:     t.Body,
			Modified: t.Modified,
		})
	}
	if err := tx.CreateInBatches(rows, 200).Error; err != nil {
		return fmt.Errorf("writing help %s: %w", corpus, err)
	}
	return nil
}

func writeMeta(tx *gorm.DB, key, value string) error {
	return tx.Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&Meta{Key: key, Value: value}).Error
}

func toRow(o *world.Object) Object {
	return Object{
		Ref:          int32(o.Ref),
		Name:         o.Name,
		Type:         uint8(o.Type()),
		Flags:        uint32(o.Flags),
		Owner:        int32(o.Owner),
		Location:     int32(o.Location),
		Contents:     int32(o.Contents),
		Exits:        int32(o.Exits),
		Next:         int32(o.Next),
		Home:         int32(o.Home),
		Dropto:       int32(o.Dropto),
		PasswordHash: o.PasswordHash,
		Created:      o.Created.Unix(),
		Modified:     o.Modified.Unix(),
		LastUsed:     o.LastUsed.Unix(),
		UseCount:     o.UseCount,
	}
}

func toPropRow(r ref.Ref, e props.Entry) Property {
	return Property{
		Ref:     int32(r),
		Path:    e.Path,
		Type:    uint8(e.Value.Type),
		Str:     e.Value.Str,
		Num:     e.Value.Num,
		Float:   e.Value.Float,
		RefVal:  int32(e.Value.Ref),
		Blessed: e.Value.Blessed,
	}
}
