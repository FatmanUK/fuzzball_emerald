// Package store persists the world in Postgres.
//
// The in-memory graph is authoritative: reads never come here after
// boot, and writes arrive as batched snapshots from the world
// goroutine. That is what removes Fuzzball's dump pause — saving no
// longer has to stop the game, because it never touches the live
// objects.
package store

// maxPathLen bounds a property path. Postgres cannot index a btree
// entry larger than about 2704 bytes, and the composite key on (ref,
// path) has to fit. Real property paths are far shorter; anything
// longer is rejected rather than silently truncated.
const maxPathLen = 1024

// Object is one database object. Fuzzball keeps type-specific fields
// in a union; here they are nullable columns, which is cheaper than a
// join per object on a table that is read in full exactly once, at
// boot.
type Object struct {
	Ref      int32  `gorm:"primaryKey;autoIncrement:false"`
	Name     string `gorm:"not null;index"`
	Type     uint8  `gorm:"not null;index"`
	Flags    uint32 `gorm:"not null"`
	Owner    int32  `gorm:"not null;index"`
	Location int32  `gorm:"not null;index"`

	// Contents, Exits and Next reproduce the intrusive lists
	// Fuzzball threads objects onto, because traversal order is
	// observable from MUF. Location is stored too, so a damaged
	// chain can be rebuilt on load instead of orphaning whatever
	// hung off it.
	Contents int32 `gorm:"not null"`
	Exits    int32 `gorm:"not null"`
	Next     int32 `gorm:"not null"`

	// Home applies to things and players, Dropto to rooms.
	Home   int32 `gorm:"not null"`
	Dropto int32 `gorm:"not null"`

	// PasswordHash is set only for players. Argon2id for anything
	// Emerald wrote; a legacy import carries base64 MD5 until the
	// player next logs in, at which point it is upgraded in
	// place.
	PasswordHash string

	// Timestamps are Unix seconds, as the dump format stores
	// them.
	Created  int64 `gorm:"not null"`
	Modified int64 `gorm:"not null"`
	LastUsed int64 `gorm:"not null"`
	UseCount int32 `gorm:"not null"`
}

func (Object) TableName() string { return "objects" }

// Property is one property on one object. Directories that carry no
// value of their own are not stored; they are implied by their
// children's paths.
type Property struct {
	Ref  int32  `gorm:"primaryKey;autoIncrement:false"`
	Path string `gorm:"primaryKey;size:1024"`

	Type uint8 `gorm:"not null"`
	Str  string
	Num  int64
	// Float is double precision rather than GORM's default
	// numeric: MUF float properties can hold the infinities,
	// which numeric did not represent before Postgres 14, and
	// float64 maps to this type exactly.
	Float   float64 `gorm:"type:double precision"`
	RefVal  int32
	Blessed bool `gorm:"not null"`
}

func (Property) TableName() string { return "properties" }

// ExitDest is one destination of an exit, keeping the order the exit
// lists them in.
type ExitDest struct {
	Ref  int32 `gorm:"primaryKey;autoIncrement:false"`
	Idx  int32 `gorm:"primaryKey;autoIncrement:false"`
	Dest int32 `gorm:"not null"`
}

func (ExitDest) TableName() string { return "exit_dests" }

// Program is a MUF program's source, which upstream keeps in
// muf/<ref>.m beside the dump rather than in it.
type Program struct {
	Ref    int32 `gorm:"primaryKey;autoIncrement:false"`
	Source string
}

func (Program) TableName() string { return "programs" }

// TuneParam is one @tune setting. Values are stored in their textual
// form, the same one a dump header uses, so the table stays readable
// and type changes stay backwards compatible.
type TuneParam struct {
	Name  string `gorm:"primaryKey;size:128"`
	Value string
}

func (TuneParam) TableName() string { return "tune_params" }

// Macro is a MUF editor macro, upstream's muf/macros file.
type Macro struct {
	Name       string `gorm:"primaryKey;size:128"`
	Definition string
	Owner      int32
}

func (Macro) TableName() string { return "macros" }

// HelpTopic is one entry in one help corpus: a block of an index
// file, a file in the info directory, or the whole of a single text
// such as the motd.
//
// Upstream keeps these as files under the game directory. Emerald has
// no game directory, so they live here — which is also what lets a
// wizard edit them from inside the game and what the configurator
// reads.
type HelpTopic struct {
	Corpus string `gorm:"primaryKey;size:16"`
	// Name is the topic's first alias, and is empty for the
	// header block a corpus shows when no topic is asked for.
	Name string `gorm:"primaryKey;size:64"`
	// Aliases are the topic's other names, "|"-separated exactly
	// as upstream's index line writes them.
	Aliases string
	Ord     int32  `gorm:"not null"`
	Body    string `gorm:"not null"`
	// Modified is Unix seconds, and zero for a topic written by
	// seeding and never edited since.
	Modified int64 `gorm:"not null"`
}

func (HelpTopic) TableName() string { return "help_topics" }

// Meta holds small scalars about the database itself, such as the ref
// ceiling.
type Meta struct {
	Key   string `gorm:"primaryKey;size:64"`
	Value string
}

func (Meta) TableName() string { return "meta" }

// metaTop is the key under which the ref ceiling is stored, and
// metaHelpSeed the version of the built-in help content last seeded
// into the database.
const (
	metaTop      = "db_top"
	metaHelpSeed = "help_seed_version"
)

// Gripe is one complaint, upstream's line in the log file named by
// file_log_gripes. Emerald has no game directory, so they are rows
// — the same answer the help texts got, and for the same reason.
//
// The names are stored beside the refs because upstream's log line is
// text: a player renamed or recycled afterwards would otherwise
// rewrite history.
type Gripe struct {
	ID int64 `gorm:"primaryKey;autoIncrement"`
	// At is Unix seconds.
	At        int64  `gorm:"not null;index"`
	Who       int32  `gorm:"not null"`
	WhoName   string `gorm:"not null"`
	Where     int32  `gorm:"not null"`
	WhereName string `gorm:"not null"`
	Message   string `gorm:"not null"`
}

func (Gripe) TableName() string { return "gripes" }

// allModels is what Migrate creates.
var allModels = []any{
	&Object{}, &Property{}, &ExitDest{}, &Program{},
	&TuneParam{}, &Macro{}, &HelpTopic{}, &Gripe{}, &Meta{},
}
