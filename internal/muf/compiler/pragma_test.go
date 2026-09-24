package compiler

import (
	"strings"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// TestPragmaDirectivesCompile is the point of the exercise: all three
// of these used to hit the unrecognised-directive branch, so a
// program carrying one did not compile at all.
func TestPragmaDirectivesCompile(t *testing.T) {
	for _, src := range []string{
		"$pragma comment_strict\n: main ;",
		"$pragma comment_recurse\n: main ;",
		"$pragma comment_loose\n: main ;",
		": main ; $entrypoint main",
		`$language "muf" : main ;`,
	} {
		if _, err := Compile(src, Options{}); err != nil {
			t.Errorf("compiling %q: %v", src, err)
		}
	}
}

// TestPragmaCommentStrictDoesNotNest pins the difference the pragma
// exists for. Under the default loose mode the nested parse wins and
// the whole "(a (b) c)" is one comment; under strict the comment ends
// at the first ')', leaving "c)" as code.
func TestPragmaCommentStrictDoesNotNest(t *testing.T) {
	const src = "$pragma comment_strict\n: main (a (b) c) ;"
	_, err := Compile(src, Options{})
	if err == nil {
		t.Fatal("strict comments should have left \"c)\" as code")
	}
	if !strings.Contains(err.Error(), "c)") {
		t.Errorf("error = %q, want it to name the leftover word", err)
	}

	if _, err := Compile(": main (a (b) c) ;", Options{}); err != nil {
		t.Errorf("the same source in loose mode: %v", err)
	}
}

// TestCommentsStrictOption checks the muf_comments_strict parameter
// reaches the lexer, since a world can set the mode without any
// $pragma in the source at all.
func TestCommentsStrictOption(t *testing.T) {
	const src = ": main (a (b) c) ;"
	if _, err := Compile(src, Options{CommentsStrict: true}); err == nil {
		t.Error("with muf_comments_strict set, this should not compile")
	}
}

// TestPragmaCommentRecurseReportsWhy checks the mode that refuses to
// fall back: loose mode would have recovered by treating the comment
// as flat.
func TestPragmaCommentRecurseReportsWhy(t *testing.T) {
	const src = "$pragma comment_recurse\n: main (unbalanced ( ) 1 pop ;"
	_, err := Compile(src, Options{})
	if err == nil {
		t.Fatal("an unterminated nested comment should not compile")
	}
	if !strings.Contains(err.Error(), "Unterminated comment.") {
		t.Errorf("error = %q, want upstream's wording", err)
	}
	if _, err := Compile(": main (unbalanced ( ) 1 pop ;", Options{}); err != nil {
		t.Errorf("loose mode should still fall back: %v", err)
	}
}

func TestPragmaCommentRecurseDepthLimit(t *testing.T) {
	deep := strings.Repeat("(", 8) + strings.Repeat(")", 8)
	src := "$pragma comment_recurse\n: main " + deep + " ;"
	_, err := Compile(src, Options{})
	if err == nil {
		t.Fatal("eight levels of comment should be too deep")
	}
	if !strings.Contains(err.Error(),
		"Comments nested too deep (more than 7 levels).") {
		t.Errorf("error = %q, want upstream's wording", err)
	}
}

func TestPragmaWarnings(t *testing.T) {
	res, err := CompileResult("$pragma no_such_pragma\n: main ;", Options{})
	if err != nil {
		t.Fatalf("an unrecognised pragma should be a warning: %v", err)
	}
	if len(res.Notes) != 1 ||
		!strings.Contains(res.Notes[0], "unrecognized") {
		t.Errorf("notes = %q, want one unrecognised-pragma warning",
			res.Notes)
	}

	res, err = CompileResult("$pragma comment_loose extra junk\n: main ;",
		Options{})
	if err != nil {
		t.Fatalf("extra pragma arguments should be a warning: %v", err)
	}
	if len(res.Notes) != 1 ||
		!strings.Contains(res.Notes[0], "extra junk") {
		t.Errorf("notes = %q, want the ignored arguments quoted",
			res.Notes)
	}
}

// TestPragmaWithoutArgumentDoesNotEatTheNextLine pins the reason
// lineArgToken exists: upstream checks for end of line before it
// reads a token, so the ": main" below is compiled, not consumed.
func TestPragmaWithoutArgumentDoesNotEatTheNextLine(t *testing.T) {
	_, err := Compile("$pragma\n: main ;", Options{})
	if err == nil {
		t.Fatal("$pragma with no argument should fail")
	}
	if !strings.Contains(err.Error(),
		"Pragma requires at least one argument.") {
		t.Errorf("error = %q, want upstream's wording", err)
	}
}

func TestEntrypointChoosesTheStart(t *testing.T) {
	p, err := Compile(": first 1 pop ; : second 2 pop ; $entrypoint first",
		Options{})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	if got := p.Code[p.Start].Proc.Name; got != "first" {
		t.Errorf("start = %q, want the procedure $entrypoint named", got)
	}
}

// TestEntrypointIsNotAForwardReference pins upstream's ordering
// requirement: the directive searches the procedures compiled so far.
func TestEntrypointIsNotAForwardReference(t *testing.T) {
	_, err := Compile("$entrypoint later : later 1 pop ;", Options{})
	if err == nil {
		t.Fatal("a forward $entrypoint should fail")
	}
	if !strings.Contains(err.Error(),
		"$entrypoint - unrecognized function name 'later'.") {
		t.Errorf("error = %q, want upstream's wording", err)
	}
}

func TestLanguageDirective(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{": main ;\n$language", "$language - argument is required."},
		{"$language muf : main ;",
			"$language - argument must be enclosed in double quotes."},
		{`$language "cobol" : main ;`,
			"$language - 'cobol' is not implemented on this server."},
	} {
		_, err := Compile(tc.src, Options{})
		if err == nil {
			t.Errorf("compiling %q should have failed", tc.src)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("error for %q = %q, want %q", tc.src, err, tc.want)
		}
	}
}

// TestLibdefWritesACaller is the finding behind this change: $libdef
// is how a library exports a callable name, and recording the
// directive without the property exported nothing.
func TestLibdefWritesACaller(t *testing.T) {
	res, err := CompileResult(
		": greet 1 pop ; public greet $libdef greet",
		Options{Ref: ref.Ref(57)})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	want := PropWrite{Path: "_defs/greet", Value: `#57 "greet" call`}
	if len(res.Props) != 1 || res.Props[0] != want {
		t.Errorf("props = %+v, want %+v", res.Props, want)
	}
}

func TestPubdefWritesItsValue(t *testing.T) {
	res, err := CompileResult(
		"$pubdef stamp __PROG__ \"stamp\" call\n: main ;",
		Options{Ref: ref.Ref(57)})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	want := PropWrite{Path: "_defs/stamp", Value: `#57 "stamp" call`}
	if len(res.Props) != 1 || res.Props[0] != want {
		t.Errorf("props = %+v, want %+v", res.Props, want)
	}
}

// TestPubdefColonClearsEverything covers the one name that skips the
// name rules.
func TestPubdefColonClearsEverything(t *testing.T) {
	res, err := CompileResult("$pubdef :\n: main ;", Options{})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	want := PropWrite{Path: "_defs", Delete: true}
	if len(res.Props) != 1 || res.Props[0] != want {
		t.Errorf("props = %+v, want %+v", res.Props, want)
	}
}

func TestPubdefBackslashKeepsWhatIsThere(t *testing.T) {
	res, err := CompileResult("$pubdef \\stamp 1 pop\n: main ;", Options{})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	want := PropWrite{Path: "_defs/stamp", Value: "1 pop",
		KeepExisting: true}
	if len(res.Props) != 1 || res.Props[0] != want {
		t.Errorf("props = %+v, want %+v", res.Props, want)
	}
}

func TestDefNamesWithPropertyCharactersAreRefused(t *testing.T) {
	for _, src := range []string{
		"$pubdef a/b 1\n: main ;",
		"$pubdef a:b 1\n: main ;",
		"$libdef @hidden\n: main ;",
		"$libdef ~seeonly\n: main ;",
	} {
		_, err := Compile(src, Options{})
		if err == nil {
			t.Errorf("compiling %q should have failed", src)
			continue
		}
		if !strings.Contains(err.Error(),
			"No /, :, @ nor ~ are allowed.") {
			t.Errorf("error for %q = %q", src, err)
		}
	}
}

func TestDocumentationDirectivesWriteTheirProperties(t *testing.T) {
	res, err := CompileResult(
		"$author Igor\n$version 1.0\n$note hello\n"+
			"$lib-version 2.0\n$doccmd __PROG__ #help\n: main ;",
		Options{Ref: ref.Ref(57)})
	if err != nil {
		t.Fatalf("compiling: %v", err)
	}
	want := []PropWrite{
		{Path: "_author", Value: "Igor"},
		{Path: "_version", Value: "1.0"},
		{Path: "_note", Value: "hello"},
		{Path: "_lib-version", Value: "2.0"},
		{Path: "_docs", Value: "#57 #help"},
	}
	if len(res.Props) != len(want) {
		t.Fatalf("props = %+v, want %+v", res.Props, want)
	}
	for i := range want {
		if res.Props[i] != want[i] {
			t.Errorf("props[%d] = %+v, want %+v", i, res.Props[i], want[i])
		}
	}
}
