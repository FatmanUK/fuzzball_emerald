// Package password hashes and verifies player credentials.
//
// Emerald writes Argon2id. It also verifies the two formats Fuzzball
// 7 wrote, so an imported world's players can still log in, and
// upgrades them in place on the first successful login.
package password

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

// Argon2id parameters. These follow the OWASP recommendation of 19
// MiB and two passes, which is comfortable for a login that happens
// once per session.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024 // KiB
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// Fuzzball's PBKDF2 parameters, from pbkdf2_hash in src/fbmath.c.
const (
	legacyPBKDF2Iter = 1000
	// Upstream derives 57 bytes into a 128-byte buffer and then
	// writes only the first 56 as hex. Deriving 56 gives the same
	// bytes, because both fit inside PBKDF2's first output block.
	legacyPBKDF2Len = 56
)

// Prefixes identifying each stored format.
const (
	argonPrefix  = "$argon2id$"
	legacyPBKDF2 = "$1$"
	// LegacyMD5Prefix tags a bare base64 MD5 digest carried over
	// from a dump. The tag is ours: upstream stored the digest
	// with no marker at all, which is indistinguishable from a
	// corrupt field.
	LegacyMD5Prefix = "$fbmd5$"
)

// NoPassword is the stored form for a player with no usable
// credential.
//
// Fuzzball treats an empty password as "any password works", which is
// how a player with no password logs in there. Emerald refuses the
// login instead: silently accepting any password for an account is
// not a behaviour worth preserving. Such players need a password set
// before they can connect.
const NoPassword = ""

// Hash returns an Argon2id hash of plain, in PHC string format.
func Hash(plain string) (string, error) {
	if plain == "" {
		return "", fmt.Errorf("password may not be empty")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating salt: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("%sv=%d$m=%d,t=%d,p=%d$%s$%s",
		argonPrefix, argon2.Version, argonMemory, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// b64 is the unpadded base64 the PHC string format uses.
var b64 = base64.RawStdEncoding

// Result describes the outcome of a verification.
type Result struct {
	// OK reports whether the password was correct.
	OK bool
	// NeedsUpgrade reports that the password was correct but was
	// stored in a legacy format, so the caller should rehash and
	// save it.
	NeedsUpgrade bool
}

// Verify checks plain against a stored hash.
//
// Unlike upstream, which compares only strlen(computed) leading bytes
// and so accepts a stored hash that merely starts with the right
// digest, this compares the whole value in constant time.
func Verify(stored, plain string) Result {
	switch {
	case stored == NoPassword:
		// No credential: nothing can match.
		return Result{}

	case strings.HasPrefix(stored, argonPrefix):
		ok, err := verifyArgon(stored, plain)
		if err != nil {
			return Result{}
		}
		return Result{OK: ok}

	case strings.HasPrefix(stored, legacyPBKDF2):
		return Result{OK: verifyLegacyPBKDF2(stored, plain), NeedsUpgrade: true}

	case strings.HasPrefix(stored, LegacyMD5Prefix):
		return Result{OK: verifyLegacyMD5(stored[len(LegacyMD5Prefix):], plain), NeedsUpgrade: true}
	}
	return Result{}
}

func verifyArgon(stored, plain string) (bool, error) {
	// $argon2id$v=19$m=...,t=...,p=...$salt$key
	parts := strings.Split(stored, "$")
	if len(parts) != 6 {
		return false, fmt.Errorf("malformed argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("malformed argon2id version")
	}
	if version != argon2.Version {
		return false, fmt.Errorf("unsupported argon2 version %d", version)
	}
	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, fmt.Errorf("malformed argon2id parameters")
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("malformed argon2id salt")
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("malformed argon2id key")
	}
	got := argon2.IDKey([]byte(plain), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// verifyLegacyPBKDF2 checks a "$1$salt$hex" hash from Fuzzball 7.
func verifyLegacyPBKDF2(stored, plain string) bool {
	rest := stored[len(legacyPBKDF2):]
	salt, hexDigest, ok := strings.Cut(rest, "$")
	if !ok || salt == "" {
		return false
	}
	want, err := hex.DecodeString(hexDigest)
	if err != nil {
		return false
	}
	got := pbkdf2.Key([]byte(plain), []byte(salt), legacyPBKDF2Iter, legacyPBKDF2Len, sha512.New)
	if len(want) > len(got) {
		return false
	}
	return subtle.ConstantTimeCompare(got[:len(want)], want) == 1
}

// verifyLegacyMD5 checks a bare base64 MD5 digest from an old
// Fuzzball world.
func verifyLegacyMD5(storedB64, plain string) bool {
	sum := md5.Sum([]byte(plain))
	want := base64.StdEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(want), []byte(storedB64)) == 1
}

// FromLegacyDump converts the password field of a dump record into a
// stored hash. An empty field becomes NoPassword.
func FromLegacyDump(field string) string {
	f := strings.TrimSpace(field)
	if f == "" {
		return NoPassword
	}
	if strings.HasPrefix(f, legacyPBKDF2) {
		// Already self-describing; keep it as it is.
		return f
	}
	// Anything else is the oldest form, a bare base64 MD5 digest.
	return LegacyMD5Prefix + f
}
