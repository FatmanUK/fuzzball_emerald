package world

import (
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// The six values upstream keeps on #0 under the _sys propdir, which
// is SYSTEM_PROPDIR_PROTECT2 (include/game.h:70). A program reads
// them like any other property, and do_uptime reads one of them back
// rather than consulting a clock.
//
// Emerald wrote none of them, so `uptime` agreed with upstream and a
// program asking #0 got nothing.
const (
	SysStartupTime  = "_sys/startuptime"
	SysMaxPennies   = "_sys/maxpennies"
	SysDumpInterval = "_sys/dumpinterval"
	SysMaxConnects  = "_sys/max_connects"
	SysLastDumpTime = "_sys/lastdumptime"
	SysShutdownTime = "_sys/shutdowntime"
)

// WriteBootProps writes the four values upstream sets once the
// database is loaded (game.c:487-490).
//
// They are integers, because add_property is called with a NULL
// string and a value — so a program reads them with getpropval, not
// getpropstr. max_connects starts at zero and RecordMaxConnects
// raises it.
//
// Two of them name parameters this server treats differently and are
// written anyway, because a program asking is entitled to an answer
// rather than an empty property:
//
//   - maxpennies is max_pennies, which Emerald honours.
//   - dumpinterval is dump_interval, which is **inert** here: the
//     dump_* family is a declared divergence, since persistence is
//     write-behind rather than a dump cycle. The number is what the
//     parameter says, and it describes nothing.
func (w *World) WriteBootProps(now time.Time) {
	w.setSysInt(SysStartupTime, now.Unix())
	w.setSysInt(SysMaxPennies, w.Tune.Int("max_pennies"))
	w.setSysInt(SysDumpInterval,
		int64(w.Tune.Duration("dump_interval").Seconds()))
	w.setSysInt(SysMaxConnects, 0)
}

// RecordMaxConnects raises _sys/max_connects to n, which upstream
// does from its own connection counter (interface.c:4585). It is a
// high-water mark, never a current count, and upstream only ever
// moves it up.
func (w *World) RecordMaxConnects(n int) {
	if v, ok := w.GetProp(ref.GlobalEnvironment,
		SysMaxConnects); ok && v.Num >= int64(n) {
		return
	}
	w.setSysInt(SysMaxConnects, int64(n))
}

// RecordLastDump writes _sys/lastdumptime, which upstream sets per
// dump (events.c:99).
//
// Emerald has no dump cycle, so the analogue is a flush — and it is
// written only when a flush has something to write. Writing it on
// every tick would make the snapshot never empty, so an idle world
// would write to Postgres for ever just to record that it had.
func (w *World) RecordLastDump(now time.Time) {
	w.setSysInt(SysLastDumpTime, now.Unix())
}

// RecordShutdown writes _sys/shutdowntime, which upstream sets at
// shovechars's tail just before the final dump (interface.c:4600).
//
// It must be written **before** the last flush takes its snapshot, or
// it never reaches the database. @armageddon deliberately does not
// come this way at all — it exits without dumping, so upstream
// leaves the property at whatever the previous clean shutdown wrote.
func (w *World) RecordShutdown(now time.Time) {
	w.setSysInt(SysShutdownTime, now.Unix())
}

// setSysInt writes one integer property on #0 and marks it for
// writing.
func (w *World) setSysInt(path string, v int64) {
	w.SetProp(ref.GlobalEnvironment, path, props.Value{
		Type: props.Int, Num: v,
	})
}

// HasPending reports whether a snapshot taken now would carry
// anything, mirroring Snapshot.Empty.
//
// The flush loop needs this to decide whether to stamp
// _sys/lastdumptime, which must happen *before* TakeSnapshot to be
// included in it — and which must not happen at all when there is
// nothing else to write. DirtyCount is not enough: a snapshot can be
// non-empty on a @tune change alone, with no dirty objects.
func (w *World) HasPending() bool {
	return len(w.dirty) > 0 || len(w.deleted) > 0 ||
		len(w.progDirty) > 0 || w.macrosDirty ||
		len(w.newGripes) > 0 || len(w.helpDirty) > 0 ||
		w.tuneDirty
}
