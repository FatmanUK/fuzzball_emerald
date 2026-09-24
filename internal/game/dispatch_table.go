// Code generated from the do_command dispatcher in
// fuzzball/src/game.c. Regenerate by hand if the submodule moves; see
// dispatch.go for what each field means and why the table exists.

package game

// commandTable is every command Fuzzball 7 dispatches, in the order
// its nested switch visits them. Resolution is a linear scan taking
// the first entry that accepts the typed word, which is equivalent to
// walking the trie because min encodes how deep the trie commits
// before it reaches each name.
//
// Entries with no handler registered resolve normally and then report
// that this server does not implement them, which is what keeps the
// abbreviations correct: dropping a name Emerald lacks would silently
// widen every abbreviation that name used to constrain.
var commandTable = []command{
	{n: "@action", min: 3, p: pBuild | pGuest},
	{n: "@armageddon", rule: rExact},
	{n: "@attach", min: 3, p: pBuild | pGuest},
	{n: "@bless", min: 3, p: pNoForce | pPlayer | pWiz},
	{n: "@boot", min: 3, p: pPlayer | pWiz},
	{n: "@chlock", min: 4, p: pGuest},
	{n: "@chown", min: 4, max: 6},
	{n: "@chown_lock", min: 7, p: pGuest},
	{n: "@clone", min: 3, p: pBuild | pGuest},
	{n: "@conlock", min: 5, p: pGuest},
	{n: "@contents", min: 5, p: pBuild | pGuest},
	{n: "@credits", rule: rFold},
	{n: "@create", min: 3, p: pBuild | pGuest},
	{n: "@debug", rule: rFold, p: pWiz},
	{n: "@describe", min: 3, p: pGuest},
	{n: "@dig", min: 3, p: pBuild | pGuest},
	{n: "@doing", min: 3, p: pGuest},
	{n: "@drop", min: 3, p: pGuest},
	{n: "@dump", min: 3, p: pPlayer | pWiz},
	{n: "@edit", min: 3, p: pMuck | pGuest | pPlayer},
	{n: "@entrances", min: 3, p: pBuild | pGuest},
	{n: "@examine", min: 3, p: pGod},
	{n: "@fail", min: 3, p: pGuest},
	{n: "@find", min: 3, p: pBuild | pGuest},
	{n: "@flock", min: 3, p: pNoForce | pGuest},
	{n: "@force", min: 3, max: 6},
	{n: "@force_lock", min: 7, p: pNoForce | pGuest},
	{n: "@idescribe", min: 2, p: pGuest},
	{n: "@kill", min: 2, p: pBuild | pGuest},
	{n: "@link", min: 4, max: 6, p: pGuest},
	{n: "@linklock", min: 7, p: pGuest},
	{n: "@list", min: 4, p: pGuest | pPlayer},
	{n: "@lock", min: 3, p: pGuest},
	{n: "@mcpedit", min: 3, p: pMuck | pGuest | pPlayer},
	{n: "@mcpprogram", min: 3, p: pMuck | pGuest | pPlayer},
	{n: "@memory", min: 3, p: pWiz},
	{n: "@name", min: 3, p: pGuest},
	{n: "@newpassword", rule: rExact, p: pPlayer | pWiz},
	{n: "@odrop", min: 3, p: pGuest},
	{n: "@oecho", min: 3, p: pGuest},
	{n: "@ofail", min: 3, p: pGuest},
	{n: "@open", min: 3, p: pBuild | pGuest},
	{n: "@osuccess", min: 3, p: pGuest},
	{n: "@owned", min: 3, p: pBuild | pGuest},
	{n: "@ownlock", min: 3, p: pNoForce | pGuest},
	{n: "@password", min: 3, p: pGuest | pPlayer},
	{n: "@pcreate", min: 3, p: pPlayer | pWiz},
	{n: "@pecho", min: 3, p: pGuest},
	{n: "@program", min: 3, p: pMuck | pGuest | pPlayer},
	{n: "@propset", min: 3, p: pGuest},
	{n: "@ps", min: 3, p: pBuild | pGuest},
	{n: "@readlock", min: 4, p: pNoForce | pGuest},
	{n: "@reconfiguressl", rule: rExact, p: pPlayer | pWiz},
	{n: "@recycle", min: 4, p: pGuest},
	{n: "@register", min: 4, p: pGuest},
	{n: "@relink", min: 4, p: pGuest},
	{n: "@restart", rule: rExact},
	{n: "@restrict", rule: rExact, p: pPlayer | pWiz},
	{n: "@sanity", rule: rExact, p: pGod},
	{n: "@sanchange", rule: rExact, p: pGod | pNoForce},
	{n: "@sanfix", rule: rExact, p: pGod | pNoForce},
	{n: "@set", min: 3},
	{n: "@shutdown", rule: rExact},
	{n: "@stats", min: 3},
	{n: "@success", min: 3, p: pGuest},
	{n: "@sweep", min: 3, p: pBuild | pGuest},
	{n: "@teledump", rule: rExact, p: pNoForce | pPlayer | pWiz},
	{n: "@teleport", min: 3, p: pGuest},
	{n: "@toad", rule: rExact, p: pNoForce | pPlayer | pWiz},
	{n: "@tops", rule: rExact, p: pWiz},
	{n: "@trace", min: 3, p: pBuild | pGuest},
	{n: "@tune", min: 3, p: pPlayer | pWiz},
	{n: "@unbless", min: 4, p: pNoForce | pPlayer | pWiz},
	{n: "@unlink", min: 5, p: pGuest},
	{n: "@unlock", min: 5, p: pGuest},
	{n: "@uncompile", min: 6, p: pPlayer | pWiz},
	{n: "@usage", min: 3, p: pWiz},
	{n: "@version", min: 2},
	{n: "@wall", rule: rExact, p: pPlayer | pWiz},
	{n: "disembark", min: 2},
	{n: "drop", min: 2},
	{n: "examine", min: 1},
	{n: "get", min: 2},
	{n: "give", min: 2},
	{n: "goto", min: 2},
	{n: "gripe", rule: rFold},
	{n: "hand", rule: rFold},
	{n: "help", min: 1},
	{n: "info", rule: rFold},
	{n: "inventory", min: 1},
	{n: "look", min: 1},
	{n: "leave", min: 1},
	// The one command upstream reaches with no Matched() at all:
	// game.c:1667 tests string_prefix(command, "move"), whose
	// arguments are the other way round, so the typed word has to
	// *begin* with "move" and anything after it is accepted.
	// "movex" runs it. Reproduced rather than tidied.
	{n: "move", rule: rStartsWith},
	{n: "motd", rule: rFold},
	{n: "mpi", rule: rFold},
	{n: "man", rule: rFold},
	{n: "news", min: 1},
	{n: "page", min: 2},
	{n: "pose", min: 2},
	{n: "put", min: 2},
	{n: "read", min: 2},
	{n: "say", min: 2},
	{n: "score", min: 2},
	{n: "take", min: 2},
	{n: "throw", min: 2},
	{n: "uptime", min: 1},
	{n: "whisper", min: 1},

	// Below here are Emerald's own, which Fuzzball has no entry
	// for. They are exact-match so they cannot widen or shadow
	// any abbreviation above, and they come last so an upstream
	// name always wins a tie.
	//
	// "home" is not really an extension: upstream reaches it
	// inside can_move (move.c:730), gated by enable_home, so it
	// is matched as a *direction* before the dispatcher is
	// consulted at all. Emerald matches it here instead, which
	// differs where a room defines an exit called "home" and
	// where enable_home is off. Worth moving to the exit-matching
	// path.
	{n: "home", rule: rFold},
	// @help edits the help corpora, which upstream keeps as files
	// beside the server and edits with a text editor.
	{n: "@help", rule: rFold, p: pWiz},
}
