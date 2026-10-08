package golden

import "testing"

// `do_parse_mesg` (`msgparse.c:1400`) runs its scanner over **every**
// message property, whether or not the text contains a call, and the
// scanner does three things besides evaluating one:
//
//   - a **backtick** toggles literal mode and is consumed
//     (`MFUN_LITCHAR`, `mpi.h:31`);
//   - `\r` becomes a carriage return and `\[` becomes an escape,
//     which is how a world writes ANSI into a description;
//   - any other `\x` passes through as `x`, so a backslash is how
//     a literal brace or backtick is written.
//
// This server short-circuited on "does the text contain a brace?",
// which was invented -- so a description with no call in it was
// printed raw and every one of those behaviours was missing. The
// walkthrough found it on “ `look mailboxes' “ in §2.2.2's
// looktrap, where the backtick simply survived.
//
// The `\[` probe does not show colour arriving: both servers put the
// escape into the text and both then strip it on the way to a
// transcript, so what the line pins is the substitution itself --
// "1mredm plain" rather than the "\[31m..." a missing rule would
// leave. That is enough to discriminate, and the COLOR flag is set so
// the two sides at least take the same path to it.
var mpiScanScript = Script{
	"@create widget",

	// The backtick, consumed in a text with no call at all.
	"@describe widget=a `b' c",
	"look widget",

	// Two of them, which is the pair that makes a brace literal.
	"@describe widget=`{not:a:call}`",
	"look widget",

	// The escapes. A backslash before anything else is just that
	// character, which is the only way to write a brace or a
	// backtick where one is meant.
	"@describe widget=brace \\{ tick \\` slash \\\\ x \\x",
	"look widget",

	// And the two spellings that mean something else. \\[ is the
	// escape character, so this is a real SGR sequence, and \\r
	// is a carriage return.
	"@set me=C",
	"@describe widget=\\[31mred\\[0m plain",
	"look widget",
	"@describe widget=before\\rafter",
	"look widget",

	// `this` is the one name that is unique to MPI
	// (`msgparse.c:691`) and was not handled here at all, so
	// every `{...:this}` answered "Match failed." The three
	// keywords beside it, and `home`, which upstream's own
	// closing OkObj check rejects -- so it fails there too.
	"@describe widget={name:this}",
	"look widget",
	"@describe widget={name:me}",
	"look widget",
	"@describe widget={name:here}",
	"look widget",
	"@describe widget={name:home}",
	"look widget",

	// NAME has no zero-argument form, which is worth pinning
	// because `resolve` *does* default to the object carrying the
	// message -- just not for a function whose arity forbids the
	// call in the first place.
	"@describe widget={name}",
	"look widget",

	// And a name resolved by the search rather than a keyword, to
	// show the five stages still work.
	"@describe widget={name:test.muf}",
	"look widget",
}

// TestMPIScanMatchesFuzzball compares the ladder.
func TestMPIScanMatchesFuzzball(t *testing.T) {
	requireOracle(t)
	compareWalkthrough(t, mpiScanScript)
}
