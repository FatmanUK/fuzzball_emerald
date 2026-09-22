package muf

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
)

// GETSEED and SETSEED expose the per-frame seeded generator SRAND draws from,
// upstream's fr->rndbuf — a 16-byte buffer re-hashed on every draw.
func init() {
	register("GETSEED", func(f *Frame) (*Result, error) {
		if f.rndbuf == nil {
			return nil, f.Push(Str(""))
		}
		return nil, f.Push(Str(encodeSeed(f.rndbuf)))
	})

	register("SETSEED", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		// Upstream's "!oper1->data.string" is the empty string, which a
		// Fuzzball stack holds as a null pointer rather than a zero-length
		// one: an empty seed re-seeds from the clock instead of decoding.
		if s == "" {
			f.rndbuf = newSeed()
			return nil, nil
		}
		f.rndbuf = decodeSeed(s)
		return nil, nil
	})
}

// newSeed is upstream's init_seed(NULL).
func newSeed() []byte {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return b
}

// rndFrom is upstream's rnd(): hash the buffer back over itself and take the
// first word.
//
// Upstream hashes "sizeof(digest)" bytes, where digest is a uint32* — so it
// feeds MD5 the first 8 bytes of the 16-byte buffer, not all of it. That is
// reproduced, because the sequence a given seed produces is observable and
// programs that record a seed expect to replay it.
func rndFrom(buf []byte) uint32 {
	sum := md5.Sum(buf[:8])
	copy(buf, sum[:])
	return binary.LittleEndian.Uint32(buf[:4])
}

// encodeSeed is GETSEED's nibble-per-character encoding: low nibble first,
// then high, each offset by 'A'.
func encodeSeed(buf []byte) string {
	out := make([]byte, 32)
	for i := 0; i < 16; i++ {
		out[i*2] = buf[i]&0x0F + 'A'
		out[i*2+1] = (buf[i]&0xF0)>>4 + 'A'
	}
	return string(out)
}

// decodeSeed is SETSEED's inverse. A seed shorter than 32 characters is
// repeated to fill, and a longer one truncated, both upstream's own.
func decodeSeed(s string) []byte {
	if len(s) > 32 {
		s = s[:32]
	}
	var hold [32]byte
	for i := range hold {
		hold[i] = s[i%len(s)]
	}
	buf := make([]byte, 16)
	for i := 0; i < 16; i++ {
		buf[i] = (hold[i*2]-'A')&0x0F | ((hold[i*2+1]-'A')&0x0F)<<4
	}
	return buf
}
