package store

import (
	"context"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/importer"
	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestImportRoundTripKeepsAPlayerConnectable walks the whole path an imported
// world takes before anyone can log in:
//
//	dump → World → Flush → Postgres → Load → PlayerNamed → Verify
//
// Nothing covered that end to end, and a bug lived in the gap: a dump with no
// muf/ directory beside it returned early from importer.Load before marking
// the world dirty, so Flush wrote no objects. The import reported success,
// the database held only tune parameters, and the world's own player could
// not connect — "Either that player does not exist, or has a different
// password."
//
// minimal.db is the fixture precisely because it has no muf/ directory.
func TestImportRoundTripKeepsAPlayerConnectable(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	res, err := importer.Load(importer.Source{
		DumpPath: "../../testdata/minimal.db",
	})
	if err != nil {
		t.Fatalf("importing: %v", err)
	}
	if err := st.Flush(ctx, res.World.TakeSnapshot()); err != nil {
		t.Fatalf("writing the world: %v", err)
	}

	// Load it back the way the server does at boot, into a fresh world.
	loaded := world.New()
	rep, err := st.Load(ctx, loaded)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if rep.Objects != 2 {
		t.Fatalf("loaded %d objects, want 2 — the world did not survive the round trip",
			rep.Objects)
	}

	player, ok := loaded.PlayerNamed("One")
	if !ok {
		t.Fatal("the imported player is not in the player index")
	}
	o := loaded.Get(player)
	if o == nil {
		t.Fatalf("player %v has no object", player)
	}
	if o.Type() != ref.TypePlayer {
		t.Errorf("player %v is a %v", player, o.Type())
	}

	// The dump carries a bare base64 MD5, which the importer tags and the
	// verifier accepts, flagging it for upgrade on first login.
	res2 := password.Verify(o.PasswordHash, "potrzebie")
	if !res2.OK {
		t.Errorf("the imported password does not verify (stored %q)", o.PasswordHash)
	}
	if !res2.NeedsUpgrade {
		t.Error("a legacy MD5 password should be flagged for upgrade")
	}

	// The description survived too, so properties round-tripped as well as
	// the objects themselves.
	if v, ok := loaded.GetProp(player, "_/de"); !ok || v.Str == "" {
		t.Errorf("the player's description did not survive: %+v", v)
	}
}
