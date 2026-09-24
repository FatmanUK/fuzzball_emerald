package session

import (
	"bytes"
	"testing"
)

func decodeAll(d *telnetDecoder, in []byte) []byte {
	return d.Decode(in)
}

func TestDecodePassesPlainText(t *testing.T) {
	d := newTelnetDecoder(nil)
	got := decodeAll(d, []byte("connect One potrzebie\r\n"))
	if string(got) != "connect One potrzebie\r\n" {
		t.Errorf("got %q", got)
	}
}

func TestDecodeStripsCommands(t *testing.T) {
	d := newTelnetDecoder(nil)
	in := []byte{'a', IAC, NOP, 'b', IAC, AYT, 'c'}
	if got := decodeAll(d, in); string(got) != "abc" {
		t.Errorf("got %q, want abc", got)
	}
}

func TestDoubledIACIsALiteralByte(t *testing.T) {
	d := newTelnetDecoder(nil)
	in := []byte{'a', IAC, IAC, 'b'}
	want := []byte{'a', IAC, 'b'}
	if got := decodeAll(d, in); !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDecodeRefusesOptionsItDoesNotOffer(t *testing.T) {
	d := newTelnetDecoder(nil)
	// The client asks us to enable option 24 (terminal type).
	decodeAll(d, []byte{IAC, DO, 24})
	want := []byte{IAC, WONT, 24}
	if got := d.TakeReply(); !bytes.Equal(got, want) {
		t.Errorf("reply = %v, want %v", got, want)
	}
	// Taking the reply clears it.
	if got := d.TakeReply(); got != nil {
		t.Errorf("reply should have been cleared, got %v", got)
	}
}

func TestDecodeAcceptsNAWS(t *testing.T) {
	d := newTelnetDecoder(nil)
	decodeAll(d, []byte{IAC, WILL, OptNAWS})
	want := []byte{IAC, DO, OptNAWS}
	if got := d.TakeReply(); !bytes.Equal(got, want) {
		t.Errorf("reply = %v, want %v", got, want)
	}

	// Any other offer is declined.
	decodeAll(d, []byte{IAC, WILL, 24})
	if got := d.TakeReply(); !bytes.Equal(got, []byte{IAC, DONT, 24}) {
		t.Errorf("reply = %v, want a refusal", got)
	}
}

func TestWindowSizeSubnegotiation(t *testing.T) {
	var got WindowSize
	d := newTelnetDecoder(func(ws WindowSize) { got = ws })

	// 132 x 43.
	decodeAll(d, []byte{IAC, SB, OptNAWS, 0, 132, 0, 43, IAC, SE})
	if got != (WindowSize{Width: 132, Height: 43}) {
		t.Errorf("window size = %+v, want 132x43", got)
	}

	// A wide terminal, where the high byte matters.
	decodeAll(d, []byte{IAC, SB, OptNAWS, 1, 44, 0, 50, IAC, SE})
	if got.Width != 300 {
		t.Errorf("width = %d, want 300", got.Width)
	}
}

func TestWindowSizeWithEscapedByte(t *testing.T) {
	var got WindowSize
	d := newTelnetDecoder(func(ws WindowSize) { got = ws })
	// A width of 255 must be sent as a doubled 255 inside the
	// subnegotiation, or it would read as a command.
	decodeAll(d, []byte{IAC, SB, OptNAWS, 0, IAC, IAC, 0, 24, IAC, SE})
	if got != (WindowSize{Width: 255, Height: 24}) {
		t.Errorf("window size = %+v, want 255x24", got)
	}
}

func TestDecodeAcrossChunkBoundaries(t *testing.T) {
	// A command split across two reads must still be handled: TCP
	// gives no guarantee about where a packet ends.
	var got WindowSize
	d := newTelnetDecoder(func(ws WindowSize) { got = ws })

	full := []byte{'h', 'i', IAC, SB, OptNAWS, 0, 80, 0, 24, IAC, SE, 'o', 'k'}
	var out []byte
	for i := range full {
		out = append(out, d.Decode(full[i:i+1])...)
	}
	if string(out) != "hiok" {
		t.Errorf("data = %q, want hiok", out)
	}
	if got != (WindowSize{Width: 80, Height: 24}) {
		t.Errorf("window size = %+v, want 80x24", got)
	}
}

func TestSubnegotiationIsBounded(t *testing.T) {
	// A client that never sends SE must not make us buffer
	// without limit.
	d := newTelnetDecoder(nil)
	d.Decode([]byte{IAC, SB, 99})
	d.Decode(bytes.Repeat([]byte{'x'}, 100_000))
	if len(d.subBuf) > 64 {
		t.Errorf("subnegotiation buffer grew to %d bytes", len(d.subBuf))
	}
}

func TestDecodeDoesNotAliasInput(t *testing.T) {
	// The decoder is handed a reusable read buffer; writing into
	// it would corrupt data the caller still holds.
	d := newTelnetDecoder(nil)
	in := []byte("hello")
	out := d.Decode(in)
	copy(in, "world")
	if string(out) != "hello" {
		t.Errorf("output changed when the input buffer was reused: %q", out)
	}
}

func TestEscapeIAC(t *testing.T) {
	if got := escapeIAC([]byte("plain")); string(got) != "plain" {
		t.Errorf("got %q", got)
	}
	got := escapeIAC([]byte{'a', IAC, 'b'})
	want := []byte{'a', IAC, IAC, 'b'}
	if !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
