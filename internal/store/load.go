package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// LoadReport describes what a Load did, so the server can log it and
// tests can assert on it.
type LoadReport struct {
	Objects    int
	Properties int
	Programs   int
	Tune       int
	// ChainsRepaired counts containers whose contents or exits
	// list did not agree with what the objects in them claimed,
	// and so was rebuilt from the location column.
	ChainsRepaired int
}

// Load reads the whole database into w. It runs once, at boot; after
// that the in-memory graph serves every read.
func (s *Store) Load(ctx context.Context, w *world.World) (LoadReport, error) {
	var rep LoadReport
	db := s.db.WithContext(ctx)

	// Objects first, so properties and destinations have
	// somewhere to go.
	byRef := make(map[ref.Ref]*world.Object)
	var batch []Object
	err := db.Model(&Object{}).Order("ref").FindInBatches(&batch, 1000,
		func(*gorm.DB, int) error {
			for i := range batch {
				o := fromRow(&batch[i])
				if err := w.Add(o); err != nil {
					return err
				}
				byRef[o.Ref] = o
				rep.Objects++
			}
			return nil
		}).Error
	if err != nil {
		return rep, fmt.Errorf("loading objects: %w", err)
	}

	var propBatch []Property
	err = db.Model(&Property{}).Order("ref, path").FindInBatches(&propBatch, 2000,
		func(*gorm.DB, int) error {
			for i := range propBatch {
				p := &propBatch[i]
				o := byRef[ref.Ref(p.Ref)]
				if o == nil {
					// A property whose object is
					// gone is dropped rather than
					// resurrecting a phantom
					// object.
					s.log.Warn("property with no object",
						"ref", ref.Ref(p.Ref), "path", p.Path)
					continue
				}
				o.Props.Set(p.Path, fromPropRow(p))
				rep.Properties++
			}
			return nil
		}).Error
	if err != nil {
		return rep, fmt.Errorf("loading properties: %w", err)
	}

	var destRows []ExitDest
	if err := db.Order("ref, idx").Find(&destRows).Error; err != nil {
		return rep, fmt.Errorf("loading exit destinations: %w", err)
	}
	for _, d := range destRows {
		if o := byRef[ref.Ref(d.Ref)]; o != nil {
			o.Dest = append(o.Dest, ref.Ref(d.Dest))
		}
	}

	var tuneRows []TuneParam
	if err := db.Find(&tuneRows).Error; err != nil {
		return rep, fmt.Errorf("loading tune parameters: %w", err)
	}
	for _, p := range tuneRows {
		if err := w.Tune.SetString(p.Name, p.Value); err != nil {
			// A stored parameter that no longer exists,
			// or no longer parses, must not stop the
			// server from booting.
			s.log.Warn("ignoring stored tune parameter",
				"name", p.Name, "value", p.Value, "error", err)
			continue
		}
		rep.Tune++
	}

	// The ref ceiling is stored so a world whose highest objects
	// were all recycled still hands out fresh refs rather than
	// reusing them.
	var top Meta
	if err := db.First(&top, "key = ?", metaTop).Error; err == nil {
		if n, convErr := strconv.ParseInt(top.Value, 10, 32); convErr == nil {
			w.SetTop(ref.Ref(n))
		}
	} else if err != gorm.ErrRecordNotFound {
		return rep, fmt.Errorf("loading metadata: %w", err)
	}

	rep.ChainsRepaired = w.RepairChains()
	if rep.ChainsRepaired > 0 {
		s.log.Warn("rebuilt damaged containment chains",
			"containers", rep.ChainsRepaired)
	}
	return rep, nil
}

// LoadPrograms reads MUF source into a callback. Program text is not
// needed to serve a look or a move, so it is loaded separately from
// the object graph.
func (s *Store) LoadPrograms(ctx context.Context, fn func(ref.Ref, string) error) (int, error) {
	n := 0
	var batch []Program
	err := s.db.WithContext(ctx).Model(&Program{}).Order("ref").
		FindInBatches(&batch, 200, func(*gorm.DB, int) error {
			for i := range batch {
				if err := fn(ref.Ref(batch[i].Ref), batch[i].Source); err != nil {
					return err
				}
				n++
			}
			return nil
		}).Error
	if err != nil {
		return n, fmt.Errorf("loading programs: %w", err)
	}
	return n, nil
}

// SaveProgram stores a program's source.
func (s *Store) SaveProgram(ctx context.Context, r ref.Ref, source string) error {
	return s.db.WithContext(ctx).Save(&Program{Ref: int32(r), Source: source}).Error
}

func fromRow(r *Object) *world.Object {
	// The internal flags do not survive a load, exactly as
	// db_read_object drops them: they describe a live session,
	// not the object. Without this a crash mid-edit would leave a
	// program permanently claiming someone else is editing it.
	flags := ref.Flags(r.Flags) &^ ref.DumpMask
	if flags.Type() == ref.TypeProgram {
		flags &^= ref.Internal
	}
	return &world.Object{
		Ref:          ref.Ref(r.Ref),
		Name:         r.Name,
		Flags:        flags,
		Owner:        ref.Ref(r.Owner),
		Location:     ref.Ref(r.Location),
		Contents:     ref.Ref(r.Contents),
		Exits:        ref.Ref(r.Exits),
		Next:         ref.Ref(r.Next),
		Home:         ref.Ref(r.Home),
		Dropto:       ref.Ref(r.Dropto),
		PasswordHash: r.PasswordHash,
		Props:        props.New(),
		Created:      time.Unix(r.Created, 0).UTC(),
		Modified:     time.Unix(r.Modified, 0).UTC(),
		LastUsed:     time.Unix(r.LastUsed, 0).UTC(),
		UseCount:     r.UseCount,
	}
}

func fromPropRow(p *Property) props.Value {
	return props.Value{
		Type:    props.Type(p.Type),
		Str:     p.Str,
		Num:     p.Num,
		Float:   p.Float,
		Ref:     ref.Ref(p.RefVal),
		Blessed: p.Blessed,
	}
}
