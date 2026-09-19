// Package importer loads a legacy Fuzzball database into an Emerald world.
//
// It reads the "Foxen9" dump format written by db_write in src/db.c, together
// with the MUF sources and macro table Fuzzball keeps in files beside the dump
// rather than inside it. The direction is one way: once a world is in
// Postgres, that is the system of record.
package importer

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// VersionString identifies the only dump format Emerald reads. Fuzzball wrote
// several earlier ones; converting those is upstream's job, and its own binary
// will do it.
const VersionString = "***Foxen9 TinyMUCK DUMP Format***"

const endOfDump = "***END OF DUMP***"

// maxLine bounds a single dump line. Upstream reads properties into a
// BUFFER_LEN*3 buffer, so nothing it wrote can be longer than that.
const maxLine = 16384 * 3

// Report describes what an import did.
type Report struct {
	Objects     int
	Properties  int
	Programs    int
	Macros      int
	ParamsSet   int
	ParamsReset int
	// ParamsDropped counts parameters Emerald deliberately does not
	// implement, such as the TLS settings that moved to the environment.
	ParamsDropped int
	// Warnings collects anything survivable: an unknown parameter, a
	// property that would not parse, a default that no longer matches.
	Warnings []string
}

func (r *Report) warnf(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// scanner reads a dump line by line, tracking position for error messages.
type scanner struct {
	sc   *bufio.Scanner
	line int
	// peeked holds a line read ahead of time, if any.
	peeked  string
	hasPeek bool
}

func newScanner(r io.Reader) *scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	return &scanner{sc: sc}
}

// next returns the next line. The bool reports whether one was available.
func (s *scanner) next() (string, bool) {
	if s.hasPeek {
		s.hasPeek = false
		s.line++
		return s.peeked, true
	}
	if !s.sc.Scan() {
		return "", false
	}
	s.line++
	return s.sc.Text(), true
}

// peek returns the next line without consuming it.
func (s *scanner) peek() (string, bool) {
	if s.hasPeek {
		return s.peeked, true
	}
	if !s.sc.Scan() {
		return "", false
	}
	s.peeked, s.hasPeek = s.sc.Text(), true
	return s.peeked, true
}

// errorf builds an error tagged with the current line.
func (s *scanner) errorf(format string, args ...any) error {
	return fmt.Errorf("line %d: %s", s.line, fmt.Sprintf(format, args...))
}

// ref reads a line as a dbref. Upstream uses atol, which yields 0 for
// anything unparseable; being strict here turns a corrupt dump into a clear
// error instead of an object silently parented to #0.
func (s *scanner) ref() (ref.Ref, error) {
	line, ok := s.next()
	if !ok {
		return 0, s.errorf("unexpected end of dump, wanted a dbref")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(line), 10, 32)
	if err != nil {
		return 0, s.errorf("expected a dbref, got %q", line)
	}
	return ref.Ref(n), nil
}

// int64 reads a line as an integer.
func (s *scanner) int64() (int64, error) {
	line, ok := s.next()
	if !ok {
		return 0, s.errorf("unexpected end of dump, wanted a number")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64)
	if err != nil {
		return 0, s.errorf("expected a number, got %q", line)
	}
	return n, nil
}

// Parse reads a dump into w, which should be empty.
func Parse(r io.Reader, w *world.World) (*Report, error) {
	s := newScanner(r)
	rep := &Report{}

	if err := parseHeader(s, w, rep); err != nil {
		return rep, err
	}

	for {
		line, ok := s.next()
		if !ok {
			return rep, s.errorf("dump ended without %s", endOfDump)
		}
		if line == endOfDump {
			return rep, nil
		}
		if !strings.HasPrefix(line, "#") {
			return rep, s.errorf("expected an object header or %s, got %q",
				endOfDump, line)
		}
		r, err := strconv.ParseInt(strings.TrimSpace(line[1:]), 10, 32)
		if err != nil {
			return rep, s.errorf("bad object header %q", line)
		}
		if err := parseObject(s, w, rep, ref.Ref(r)); err != nil {
			return rep, err
		}
		rep.Objects++
	}
}

// parseHeader reads the version line, the two counts and the parameter block.
func parseHeader(s *scanner, w *world.World, rep *Report) error {
	version, ok := s.next()
	if !ok {
		return s.errorf("empty dump")
	}
	if version != VersionString {
		return s.errorf("unsupported dump format %q; Emerald reads only %q",
			version, VersionString)
	}

	// The object count is a sizing hint upstream uses to preallocate, and
	// the flags word after it has been ignored since Foxen8.
	if _, err := s.int64(); err != nil {
		return fmt.Errorf("object count: %w", err)
	}
	if _, err := s.int64(); err != nil {
		return fmt.Errorf("database flags: %w", err)
	}

	nparams, err := s.int64()
	if err != nil {
		return fmt.Errorf("parameter count: %w", err)
	}
	for i := int64(0); i < nparams; i++ {
		line, ok := s.next()
		if !ok {
			return s.errorf("dump ended inside the parameter block")
		}
		if err := applyParam(w, rep, line); err != nil {
			return s.errorf("%v", err)
		}
	}
	return nil
}

// parseObject reads one object record.
func parseObject(s *scanner, w *world.World, rep *Report, r ref.Ref) error {
	name, ok := s.next()
	if !ok {
		return s.errorf("%v: dump ended before the name", r)
	}

	// Every link starts at NOTHING, as db_clear_object does upstream before
	// reading a record. Go's zero value for a ref is #0, which would quietly
	// attach objects to the global environment: a dump stores `exits` only
	// for things, players and rooms, so a program left at the zero value
	// would claim #0 as its exit list.
	o := &world.Object{
		Ref:      r,
		Name:     name,
		Props:    props.New(),
		Owner:    ref.Nothing,
		Location: ref.Nothing,
		Contents: ref.Nothing,
		Exits:    ref.Nothing,
		Next:     ref.Nothing,
		Home:     ref.Nothing,
		Dropto:   ref.Nothing,
	}

	var err error
	if o.Location, err = s.ref(); err != nil {
		return fmt.Errorf("%v location: %w", r, err)
	}
	if o.Contents, err = s.ref(); err != nil {
		return fmt.Errorf("%v contents: %w", r, err)
	}
	if o.Next, err = s.ref(); err != nil {
		return fmt.Errorf("%v next: %w", r, err)
	}

	rawFlags, err := s.int64()
	if err != nil {
		return fmt.Errorf("%v flags: %w", r, err)
	}
	// Flags describing live server state are not meaningful in a dump.
	o.Flags = ref.Flags(uint32(rawFlags)) &^ ref.DumpMask

	created, err := s.int64()
	if err != nil {
		return fmt.Errorf("%v created: %w", r, err)
	}
	lastUsed, err := s.int64()
	if err != nil {
		return fmt.Errorf("%v last used: %w", r, err)
	}
	useCount, err := s.int64()
	if err != nil {
		return fmt.Errorf("%v use count: %w", r, err)
	}
	modified, err := s.int64()
	if err != nil {
		return fmt.Errorf("%v modified: %w", r, err)
	}
	o.Created = time.Unix(created, 0).UTC()
	o.LastUsed = time.Unix(lastUsed, 0).UTC()
	o.UseCount = int32(useCount)
	o.Modified = time.Unix(modified, 0).UTC()

	// What follows is either a property block or, when the object has no
	// properties, the first type-specific field. Upstream distinguishes them
	// by peeking at a single character.
	link := ref.Nothing
	hadProps := false
	if line, ok := s.peek(); ok && strings.HasPrefix(line, "*") {
		hadProps = true
		n, err := parseProps(s, o, rep, r)
		if err != nil {
			return err
		}
		rep.Properties += n
	} else {
		if link, err = s.ref(); err != nil {
			return fmt.Errorf("%v: %w", r, err)
		}
	}

	// readLink returns the type-specific slot, which was already consumed
	// when the object had no properties.
	readLink := func() (ref.Ref, error) {
		if hadProps {
			return s.ref()
		}
		return link, nil
	}

	switch o.Type() {
	case ref.TypeThing, ref.TypePlayer:
		if o.Home, err = readLink(); err != nil {
			return fmt.Errorf("%v home: %w", r, err)
		}
		if o.Exits, err = s.ref(); err != nil {
			return fmt.Errorf("%v exits: %w", r, err)
		}
		if o.Type() == ref.TypePlayer {
			pw, ok := s.next()
			if !ok {
				return s.errorf("%v: dump ended before the password", r)
			}
			o.PasswordHash = legacyPassword(pw)
			// A player owns itself; the dump does not store it.
			o.Owner = r
		} else {
			if o.Owner, err = s.ref(); err != nil {
				return fmt.Errorf("%v owner: %w", r, err)
			}
		}

	case ref.TypeRoom:
		if o.Dropto, err = readLink(); err != nil {
			return fmt.Errorf("%v drop-to: %w", r, err)
		}
		if o.Exits, err = s.ref(); err != nil {
			return fmt.Errorf("%v exits: %w", r, err)
		}
		if o.Owner, err = s.ref(); err != nil {
			return fmt.Errorf("%v owner: %w", r, err)
		}

	case ref.TypeExit:
		ndest, err := readLink()
		if err != nil {
			return fmt.Errorf("%v destination count: %w", r, err)
		}
		if ndest < 0 || int(ndest) > maxExitDests {
			return s.errorf("%v: implausible destination count %d", r, ndest)
		}
		for i := 0; i < int(ndest); i++ {
			d, err := s.ref()
			if err != nil {
				return fmt.Errorf("%v destination %d: %w", r, i, err)
			}
			o.Dest = append(o.Dest, d)
		}
		if o.Owner, err = s.ref(); err != nil {
			return fmt.Errorf("%v owner: %w", r, err)
		}

	case ref.TypeProgram:
		if o.Owner, err = readLink(); err != nil {
			return fmt.Errorf("%v owner: %w", r, err)
		}
		// INTERNAL marks a program as compiled in the running server,
		// which a freshly loaded one is not.
		o.Flags &^= ref.Internal

	case ref.TypeGarbage:
		o.Owner = ref.Nothing

	default:
		return s.errorf("%v: unknown object type %d", r, o.Type())
	}

	return w.Add(o)
}

// maxExitDests bounds an exit's destination list, so a corrupt count cannot
// make the importer allocate without limit.
const maxExitDests = 4096

// parseProps reads a *Props* ... *End* block, returning how many properties
// were stored.
//
// Stored, not read: a dump can carry a property with an empty value, and
// storing one unsets the property instead, exactly as upstream does. Counting
// lines would overstate what the world actually holds.
func parseProps(s *scanner, o *world.Object, rep *Report, r ref.Ref) (int, error) {
	first, ok := s.next()
	if !ok {
		return 0, s.errorf("%v: dump ended at the property block", r)
	}
	if first != "*Props*" {
		return 0, s.errorf("%v: expected *Props*, got %q", r, first)
	}

	before := o.Props.Len()
	for {
		line, ok := s.next()
		if !ok {
			return o.Props.Len() - before, s.errorf("%v: properties ended without *End*", r)
		}
		if line == "*End*" {
			return o.Props.Len() - before, nil
		}
		name, value, err := parseProp(line)
		if err != nil {
			// A single unreadable property is not worth abandoning
			// the whole world for; upstream skips it too.
			rep.warnf("%v: %v", r, err)
			continue
		}
		if name == "" {
			continue
		}
		// Upstream's C never validated a property's bytes as any
		// encoding, so a live world can carry a value that is not valid
		// UTF-8 — seen in the wild as debug code that stored a raw
		// struct in a string property. Postgres's TEXT columns require
		// valid UTF-8, so this is sanitized here rather than failing the
		// whole import (or, worse, failing a flush after import, on a
		// property nothing touched again).
		if (value.Type == props.String || value.Type == props.Lock) && !utf8.ValidString(value.Str) {
			rep.warnf("%v: property %q was not valid UTF-8; invalid bytes replaced", r, name)
			value.Str = strings.ToValidUTF8(value.Str, "�")
		}
		o.Props.Set(name, value)
	}
}

// parseProp splits one "name:flags:value" line.
func parseProp(line string) (string, props.Value, error) {
	name, rest, ok := strings.Cut(line, ":")
	if !ok {
		return "", props.Value{}, fmt.Errorf("property %q has no flags", line)
	}
	flagText, value, ok := strings.Cut(rest, ":")
	if !ok {
		return "", props.Value{}, fmt.Errorf("property %q has no value", line)
	}
	flags, err := strconv.ParseUint(strings.TrimSpace(flagText), 10, 32)
	if err != nil {
		return "", props.Value{}, fmt.Errorf("property %q has unreadable flags %q", name, flagText)
	}

	v := props.Value{
		Type:    props.Type(flags) & props.TypeMask,
		Blessed: flags&props.FlagBlessed != 0,
	}
	switch v.Type {
	case props.String, props.Lock:
		v.Str = value
	case props.Int:
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return "", props.Value{}, fmt.Errorf("property %q has a bad integer %q", name, value)
		}
		v.Num = n
	case props.Ref:
		// Dumps store dbrefs as bare integers, with no leading #.
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
		if err != nil {
			return "", props.Value{}, fmt.Errorf("property %q has a bad dbref %q", name, value)
		}
		v.Ref = ref.Ref(n)
	case props.Float:
		f, err := parseFloat(value)
		if err != nil {
			return "", props.Value{}, fmt.Errorf("property %q has a bad float %q", name, value)
		}
		v.Float = f
	default:
		return "", props.Value{}, fmt.Errorf("property %q has unknown type %d", name, v.Type)
	}
	return name, v, nil
}

// parseFloat accepts the spellings Fuzzball writes, which include the
// infinities in forms Go's parser does not take on its own.
func parseFloat(s string) (float64, error) {
	t := strings.TrimSpace(s)
	if f, err := strconv.ParseFloat(t, 64); err == nil {
		return f, nil
	}
	sign := 1.0
	switch {
	case strings.HasPrefix(t, "+"):
		t = t[1:]
	case strings.HasPrefix(t, "-"):
		sign, t = -1, t[1:]
	}
	switch {
	case len(t) >= 3 && strings.EqualFold(t[:3], "inf"):
		return math.Inf(int(sign)), nil
	case len(t) >= 3 && strings.EqualFold(t[:3], "nan"):
		return math.NaN(), nil
	}
	return 0, fmt.Errorf("not a number")
}
