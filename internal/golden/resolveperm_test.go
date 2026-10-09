package golden

import (
	"context"
	"testing"
)

// MPI resolves an object argument with **four** functions and this
// server used one.
//
// `mesg_dbref_raw` (`msgparse.c:676`) is the match and makes no
// permission test; the other three are thin wrappers over it, each
// adding a different one, and which wrapper a function uses is part
// of its contract (`:742`, `:775`, `:809`). Emerald matched
// unconditionally everywhere, so `{loc}`, `{tell}`, `{otell}`,
// `{flags}`, `{contains}`, `{holds}` and `{pronouns}` had no locality
// test at all and `{store}`, `{bless}`, `{unbless}` and `{delprop}`
// had only the weaker read rule. A description could name any object
// in the database.
//
// The three tests are separated here by **one object named three
// ways**: Bob owns a gem lying in the vault the player's room hangs
// under, and another lying in the room itself. Against the far one
// raw passes and all three wrappers refuse; against the near one the
// locality test passes where the read test refuses -- which is the
// point of it, and shows `mesg_local_perms` is *weaker* than
// `mesg_read_perms` rather than stronger.
//
// The probes run through PARSEPROP, which sets `perms` to the object
// carrying the property (`do_parse_mesg`, `msgparse.c:2020`) -- the
// player here, so the permissions object owns neither gem.
//
// No parentheses inside the MUF comments: ")" ends a "( ... )"
// comment wherever it appears.
const resolvePermSource = `: show[ str:s -- ]
  me @ "_/de" s @ setprop
  me @ "_/de" "" 0 parseprop
  "[" swap strcat "]" strcat me @ swap notify
;
: bshow[ str:s -- ]
  ( The same as show, with the property blessed -- PARSEPROP sets
    MPI_ISBLESSED from Prop_Blessed on the property it is handed.
    BLESSPROP is mucker 4, which is why the script raises the
    program. )
  me @ "_/de" s @ setprop
  me @ "_/de" blessprop
  me @ "_/de" "" 0 parseprop
  "[" swap strcat "]" strcat me @ swap notify
  me @ "_/de" unblessprop
;
: locks
  ( Run again once the read locks are set: the lock route is the
    third clause of mesg_read_perms and the fourth of
    mesg_local_perms, and nothing in this server evaluated the read
    lock outside examine. )
  "{prop:_x,#7}" show
  "{loc:#7}" show
;
: probes
  ( The far gem cannot be matched by NAME at all: MPI's matcher is
    match_absolute, match_all_exits, match_neighbor, match_possession
    and match_registered, so a thing in the room's PARENT is out of
    reach on both servers. Which is why every probe below names it by
    ref -- #7 -- so that match_absolute finds it and the wrapper is
    what refuses. )
  "{name:far}" show
  "{name:#7}" show
  "{name:near}" show

  ( Raw passes, so a refusal below is the wrapper and not the match.
    Bob owns both gems. )
  "{owner:#7}" show
  "{owner:near}" show

  ( The read wrapper: ownership, a read lock, or a blessed message.
    Neither gem is #1's, so both refuse -- and #1 is a WIZARD, which
    buys nothing here. )
  "{prop:_x,#7}" show
  "{prop:_x,near}" show
  "{prop:_x,mine}" show

  ( The local wrapper, which is WEAKER rather than stronger: the
    near gem is a neighbour of the reader and passes, the far one is
    not and refuses. )
  "{loc:#7}" show
  "{loc:near}" show
  "{loc:#10}" show
  "{flags:#7}" show
  "{flags:near}" show
  "{contents:#7}" show
  "{contents:near}" show

  ( The strict wrapper, which has no lock route at all -- so it
    refuses the near gem the local wrapper allowed. )
  "{store:v,_y,near}" show
  "{delprop:_x,near}" show
  "{bless:_x,near}" show
  "{store:v,_y,mine}" show

  ( Two functions spell the refusal "Permission Denied." with a
    capital D, and nothing else does. )
  "{type:#7}" show
  "{type:near}" show
  "{pronouns:%n,#7}" show

  ( Several word a failed match the other way round, or fold a
    refusal in with it. )
  "{owner:nosuch}" show
  "{dbeq:nosuch,me}" show
  "{dbeq:me,nosuch}" show
  "{contains:nosuch}" show
  "{contains:me,nosuch}" show
  "{contains:#7,me}" show
  "{contains:me,#7}" show
  "{holds:#7,me}" show
  "{flag?:nosuch,w}" show
  "{flag?:#7,w}" show
  "{locked:me,nosuch}" show
  "{locked:nosuch,me}" show
  "{locked:me,#7}" show
  "{force:nosuch,look}" show
  "{muf:nosuch,}" show
  "{muf:near,}" show

  ( And several answer rather than failing. )
  "{type:nosuch}" show
  "{awake:#7}" show
  "{awake:nosuch}" show
  "{ontime:nosuch}" show
  "{idle:nosuch}" show
  "{ref:nosuch}" show
  "{ref:#7}" show
  "{ref:near}" show

  ( {istype} folds a refusal in with a failed match, but only when
    "Bad" is what was asked for -- which upstream's own TODO calls a
    bug and asks to have removed, because "Bad" should not bypass a
    permission check. )
  "{istype:me,Player}" show
  "{istype:mine,Thing}" show
  "{istype:nosuch,Bad}" show
  "{istype:#7,Bad}" show
  "{istype:#7,Thing}" show

  ( {isdbref} is not a match at all: it wants a literal "#N". )
  "{isdbref:me}" show
  "{isdbref:#1}" show
  "{isdbref:#999}" show
  "{isdbref:near}" show

  ( {nearby} resolves raw and carries its own locality test, with a
    message no other function has. Two spaces after the stop. )
  "{nearby:near,mine}" show
  "{nearby:#7,#7}" show
  "{nearby:nosuch,me}" show

  ( {testlock} reports its THIRD argument before its first, and its
    fourth is what to answer when there is no lock -- where an
    absent lock with no fourth argument is TRUE. )
  "{testlock:#7,_x}" show
  "{testlock:mine,_nolock}" show
  "{testlock:mine,_nolock,me,fallback}" show
  "{testlock:mine,_x,nosuch}" show
  "{testlock:#7,_x,nosuch}" show
  "{testlock:mine,@__sys__/x}" show
  "{testlock:mine,@hid}" show
  "{testlock:mine,.priv}" show

  ( safeblessprop owes is_valid_propname as well as the blessed bit,
    which only a blessed message can reach -- so these two go
    through bshow. A ':' in the name is refused; the plain one
    succeeds and says nothing. )
  "{bless:a:b}" bshow
  "{bless:_ok}" bshow

  ( An argument present and empty is a failed match, where an
    argument absent is the object carrying the message. This server
    treated the two the same. )
  "{prop:_x}" show
  "{prop:_x,}" show
;
: main
  "2" stringcmp not if locks else probes then
;`

// resolvePermScript builds the two-room nest, gives Bob a gem in
// each, and keeps one thing of #1's as the control.
var resolvePermScript = Script{
	"@pcreate Bob=secret", // #4

	// Bob owns the vault, the player stands in a room of #1's
	// inside it. @teleport on a room is a reparent, and its
	// destination must be ABODE; a dug room can be named by
	// nothing but its dbref.
	"@dig Vault", // #5
	"@set Vault=abode",
	"@chown Vault=Bob",
	"@dig Inner", // #6
	"@teleport Inner=Vault",
	"@teleport me=#6",

	// Bob's far gem, in the vault, and Bob's near one, here.
	// Chowned and written to *before* it is moved: once it is in
	// the vault it is out of the matcher's reach, and "@chown
	// far=Bob" answers "I don't understand 'far'." -- which is
	// how this case first passed while leaving #7 owned by #1 and
	// every wrapper happy.
	"@create far", // #7
	"@chown far=Bob",
	"@set far=_x:far value",
	"@teleport far=#5",
	"@create near", // #8
	"drop near",
	"@chown near=Bob",

	// A third room of #1's, holding another of Bob's things, so
	// that mesg_local_perms' FIRST clause -- the permissions
	// object owning the location -- is reachable on its own: the
	// cousin is nobody's neighbour, so the pair of isneighbor
	// clauses cannot shadow it.
	"@dig Side",      // #9
	"@create cousin", // #10
	"@chown cousin=Bob",
	"@teleport cousin=#9",

	// The control: #1's own thing, here.
	"@create mine", // #9
	"drop mine",

	"@set near=_x:near value",
	"@set mine=_x:mine value",

	// A raised pennies floor, which {money} never read.
	"@tune pennies_muf_mlev=2",

	// BLESSPROP is mucker 4, and that takes both lines on the
	// program.
	"@set test.muf=wizard",
	"@set test.muf=3",

	// BLESSPROP is not used here, but {bless} is mucker-free --
	// the program only needs PARSEPROP's floor of 3, which the
	// fixture already compiles at.
	"test",

	// The money probe on its own, since the floor is what makes
	// it interesting.
	"@set me=_/de:{money:me}",
	"look me",

	// Both routes are shut with no lock set, which "test" above
	// has already shown; then one lock at a time, in the order
	// that separates them.
	//
	// mesg_local_perms' fourth clause tests the read lock on the
	// **owner of the object's location** -- not on the object and
	// not on the location -- which reads like a slip in the C and
	// is reproduced. Bob owns the vault, so a lock on *Bob* is
	// what opens {loc} while leaving {prop} shut.
	"@readlock #4=me",
	"test 2",

	// And the lock on the object itself is mesg_read_perms' own
	// third clause, which opens the read route too. Nothing in
	// this server evaluated the read lock outside examine.
	"@readlock #7=me",
	"test 2",
}

// TestResolvePermMatchesFuzzball compares the ladder.
func TestResolvePermMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	ctx := context.Background()

	fx, err := WriteFixture(t.TempDir(), resolvePermSource)
	if err != nil {
		t.Fatal(err)
	}
	script := resolvePermScript
	oracle, err := RunOracleSteps(ctx, fx, script, nil)
	if err != nil {
		t.Fatalf("driving the oracle: %v", err)
	}
	emerald, err := RunEmeraldSteps(ctx, fx, script, nil)
	if err != nil {
		t.Fatalf("driving this server: %v", err)
	}
	for i, cmd := range script {
		var want, got string
		if i < len(oracle) {
			want = oracle[i]
		}
		if i < len(emerald) {
			got = emerald[i]
		}
		if diffs := Compare(want, got); len(diffs) > 0 {
			t.Errorf("%q\n%s", cmd, Render(diffs))
		}
	}
}
