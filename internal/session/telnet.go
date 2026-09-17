package session

// Telnet option negotiation, enough of it to read lines reliably and to learn
// the client's window size.
//
// Fuzzball also negotiates STARTTLS (option 46). Emerald does not: every
// listener is already TLS, so there is no cleartext session to upgrade.

// Telnet commands, from RFC 854.
const (
	IAC  = 255 // interpret as command
	DONT = 254
	DO   = 253
	WONT = 252
	WILL = 251
	SB   = 250 // begin subnegotiation
	GA   = 249
	EL   = 248
	EC   = 247
	AYT  = 246
	AO   = 245
	IP   = 244
	BRK  = 243
	DM   = 242
	NOP  = 241
	SE   = 240 // end subnegotiation
)

// Telnet options we care about.
const (
	OptNAWS = 31 // negotiate about window size, RFC 1073
)

// telnetState is the decoder's position in the protocol.
type telnetState int

const (
	stateNormal telnetState = iota
	stateIAC
	stateWill
	stateDo
	stateWont
	stateDont
	stateSubneg
	stateSubnegIAC
)

// WindowSize is a client's reported terminal size.
type WindowSize struct{ Width, Height int }

// telnetDecoder strips telnet control sequences from a byte stream, leaving
// the application data, and reports window sizes as it learns them.
type telnetDecoder struct {
	state telnetState
	// subOpt is the option currently being subnegotiated.
	subOpt byte
	subBuf []byte
	// pendingDup tracks the doubled 255 that represents a literal 255
	// inside a subnegotiation.
	pendingDup bool

	// onResize is called when the client reports its window size.
	onResize func(WindowSize)
	// reply queues bytes to send back, such as option refusals.
	reply []byte
}

func newTelnetDecoder(onResize func(WindowSize)) *telnetDecoder {
	return &telnetDecoder{onResize: onResize}
}

// Decode consumes raw bytes and returns the application data among them.
func (d *telnetDecoder) Decode(in []byte) []byte {
	out := in[:0:0] // a fresh slice; never alias the caller's buffer
	for _, c := range in {
		switch d.state {
		case stateNormal:
			if c == IAC {
				d.state = stateIAC
				continue
			}
			out = append(out, c)

		case stateIAC:
			switch c {
			case IAC:
				// A doubled IAC is a literal 255.
				out = append(out, IAC)
				d.state = stateNormal
			case WILL:
				d.state = stateWill
			case WONT:
				d.state = stateWont
			case DO:
				d.state = stateDo
			case DONT:
				d.state = stateDont
			case SB:
				d.state = stateSubneg
				d.subOpt = 0
				d.subBuf = d.subBuf[:0]
			default:
				// Every other command is a no-op for us.
				d.state = stateNormal
			}

		case stateWill:
			d.onWill(c)
			d.state = stateNormal
		case stateWont:
			d.state = stateNormal
		case stateDo:
			// We offer nothing, so refuse whatever is asked of us.
			d.reply = append(d.reply, IAC, WONT, c)
			d.state = stateNormal
		case stateDont:
			d.state = stateNormal

		case stateSubneg:
			if c == IAC {
				d.state = stateSubnegIAC
				continue
			}
			if d.subOpt == 0 && len(d.subBuf) == 0 {
				d.subOpt = c
				continue
			}
			d.appendSub(c)

		case stateSubnegIAC:
			switch c {
			case IAC:
				// A doubled 255 inside a subnegotiation is a
				// literal 255.
				d.appendSub(IAC)
				d.state = stateSubneg
			case SE:
				d.endSubneg()
				d.state = stateNormal
			default:
				// Malformed; abandon the subnegotiation.
				d.state = stateNormal
			}
		}
	}
	return out
}

// appendSub adds a byte to the subnegotiation buffer, bounded so a client
// cannot make us allocate without limit.
func (d *telnetDecoder) appendSub(c byte) {
	const maxSub = 64
	if len(d.subBuf) < maxSub {
		d.subBuf = append(d.subBuf, c)
	}
}

// onWill responds to a client offering an option.
func (d *telnetDecoder) onWill(opt byte) {
	if opt == OptNAWS {
		// Accept: window size is worth having.
		d.reply = append(d.reply, IAC, DO, OptNAWS)
		return
	}
	d.reply = append(d.reply, IAC, DONT, opt)
}

// endSubneg acts on a completed subnegotiation.
func (d *telnetDecoder) endSubneg() {
	if d.subOpt == OptNAWS && len(d.subBuf) >= 4 && d.onResize != nil {
		d.onResize(WindowSize{
			Width:  int(d.subBuf[0])<<8 | int(d.subBuf[1]),
			Height: int(d.subBuf[2])<<8 | int(d.subBuf[3]),
		})
	}
	d.subBuf = d.subBuf[:0]
	d.subOpt = 0
}

// TakeReply returns and clears any bytes owed to the client.
func (d *telnetDecoder) TakeReply() []byte {
	if len(d.reply) == 0 {
		return nil
	}
	out := d.reply
	d.reply = nil
	return out
}

// initialNegotiation is what the server offers on connect: ask the client to
// report its window size.
func initialNegotiation() []byte {
	return []byte{IAC, DO, OptNAWS}
}

// escapeIAC doubles any 255 in outgoing text, which the protocol requires so
// it is not read as a command.
func escapeIAC(b []byte) []byte {
	if !hasIAC(b) {
		return b
	}
	out := make([]byte, 0, len(b)+8)
	for _, c := range b {
		if c == IAC {
			out = append(out, IAC)
		}
		out = append(out, c)
	}
	return out
}

func hasIAC(b []byte) bool {
	for _, c := range b {
		if c == IAC {
			return true
		}
	}
	return false
}

// Decoder is the exported telnet decoder a transport drives.
type Decoder = telnetDecoder

// NewDecoder returns a decoder that reports window-size changes to onResize.
func NewDecoder(onResize func(WindowSize)) *Decoder { return newTelnetDecoder(onResize) }

// InitialNegotiation is what a server offers a client on connect.
func InitialNegotiation() []byte { return initialNegotiation() }

// EncodeLine renders one line for the wire: escaped, and terminated the way
// the line protocol expects.
func EncodeLine(text string) []byte {
	out := escapeIAC([]byte(text))
	return append(out, '\r', '\n')
}
