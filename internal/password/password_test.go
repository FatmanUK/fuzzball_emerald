package password

import (
	"strings"
	"testing"
)

// The starter database ships with this password for #1, and documents
// it in its README, so it is a public fixture rather than a secret.
// It gives a real vector for the legacy formats.
const starterPassword = "potrzebie"

// As stored in dbs/starterdb and dbs/minimal: a bare base64 MD5
// digest.
const starterMD5 = "CuG4ZtGvyRbfJubgNISTcg=="

// Generated independently with Python's hashlib, mirroring
// pbkdf2_hash in src/fbmath.c: HMAC-SHA512, 1000 iterations, first 56
// derived bytes as hex.
const starterPBKDF2 = "$1$abcdefghij$" +
	"fb904b0156dedfef0ae31121dacbb8c786e04b498f89581337cb9b3ad4858fc0" +
	"179324829f958c4af80613ff060edfa69c91902cc709a9c0"

func TestHashAndVerifyRoundTrip(t *testing.T) {
	h, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, argonPrefix) {
		t.Errorf("hash = %q, want an argon2id PHC string", h)
	}
	if got := Verify(h, "correct horse battery staple"); !got.OK {
		t.Error("the correct password should verify")
	} else if got.NeedsUpgrade {
		t.Error("a freshly written hash should not need upgrading")
	}
	if Verify(h, "wrong").OK {
		t.Error("the wrong password should not verify")
	}
}

func TestHashesAreSalted(t *testing.T) {
	a, err := Hash("same")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Hash("same")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two hashes of the same password should differ; the salt is not random")
	}
}

func TestEmptyPasswordCannotBeHashed(t *testing.T) {
	if _, err := Hash(""); err == nil {
		t.Error("hashing an empty password should fail")
	}
}

func TestLegacyMD5(t *testing.T) {
	stored := FromLegacyDump(starterMD5)
	if stored != LegacyMD5Prefix+starterMD5 {
		t.Fatalf("FromLegacyDump = %q, want it tagged", stored)
	}

	got := Verify(stored, starterPassword)
	if !got.OK {
		t.Error("the starter password should verify against its MD5 digest")
	}
	if !got.NeedsUpgrade {
		t.Error("a legacy MD5 password should be flagged for upgrade")
	}
	if Verify(stored, "wrong").OK {
		t.Error("the wrong password should not verify")
	}
}

func TestLegacyPBKDF2(t *testing.T) {
	// A self-describing hash is stored unchanged.
	stored := FromLegacyDump(starterPBKDF2)
	if stored != starterPBKDF2 {
		t.Fatalf("a $1$ hash should be kept as it is, got %q", stored)
	}

	got := Verify(stored, starterPassword)
	if !got.OK {
		t.Error("the starter password should verify against its PBKDF2 hash")
	}
	if !got.NeedsUpgrade {
		t.Error("a legacy PBKDF2 password should be flagged for upgrade")
	}
	if Verify(stored, "wrong").OK {
		t.Error("the wrong password should not verify")
	}
}

func TestNoPasswordRefusesEverything(t *testing.T) {
	// Fuzzball accepts any password when none is stored. Emerald
	// refuses, because silently accepting anything for an account
	// is not behaviour worth preserving.
	for _, attempt := range []string{"", "anything", starterPassword} {
		if Verify(NoPassword, attempt).OK {
			t.Errorf("an account with no password accepted %q", attempt)
		}
	}
	if got := FromLegacyDump("   "); got != NoPassword {
		t.Errorf("a blank dump field = %q, want NoPassword", got)
	}
}

func TestTruncatedHashIsRejected(t *testing.T) {
	// Upstream compares only strlen(computed) leading bytes, so a
	// stored hash that merely starts with the right digest is
	// accepted. Emerald compares the whole value.
	full := LegacyMD5Prefix + starterMD5
	truncated := full[:len(full)-4]
	if Verify(truncated, starterPassword).OK {
		t.Error("a truncated stored hash should not verify")
	}

	h, err := Hash(starterPassword)
	if err != nil {
		t.Fatal(err)
	}
	if Verify(h[:len(h)-4], starterPassword).OK {
		t.Error("a truncated argon2id hash should not verify")
	}
}

func TestMalformedHashesAreRejected(t *testing.T) {
	cases := []string{
		"$argon2id$",
		"$argon2id$v=19$m=1,t=1,p=1$notbase64!$abc",
		"$argon2id$v=1$m=19456,t=2,p=1$AAAA$AAAA",
		"$1$",
		"$1$salt$nothex",
		"$fbmd5$",
		"garbage",
	}
	for _, c := range cases {
		if Verify(c, starterPassword).OK {
			t.Errorf("malformed hash %q should not verify", c)
		}
	}
}
