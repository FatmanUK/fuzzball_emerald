package game

import (
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/session"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// The descriptor sweep, which is the second half of upstream's main
// loop (`interface.c:4540-4581`) and did not exist here at all. Five
// `@tune` parameters had no reader: `idleboot`, `maxidle`,
// `idle_boot_mesg`, `idle_ping_enable` and `idle_ping_time`. The
// first defaults **true**, so an unconfigured world diverged: an idle
// connection was never reaped and no keepalive was ever sent.
//
// `Descriptor.IdleSince` was computed and only ever *displayed*, by
// WHO and by `DESCRIDLE`.

// loginTimeout is the one number in the sweep that is **not**
// tunable: upstream hardcodes it, with the comment "Hardcode 300 secs
// -- 5 mins -- at the login screen" and a log line that quotes the
// number back.
const loginTimeout = 300 * time.Second

// sweepDescriptors runs the three per-descriptor rules on each tick.
//
// Their order is upstream's and is observable, because the keepalive
// test is **outside** the connected/not-connected branch above it: a
// descriptor can be idle-booted and then, in the same pass, asked for
// a keepalive.
func (s *Server) sweepDescriptors(w *world.World, now time.Time) {
	boot := w.Tune.Bool("idleboot")
	maxIdle := w.Tune.Duration("maxidle")
	ping := w.Tune.Bool("idle_ping_enable")
	pingAfter := w.Tune.Duration("idle_ping_time")

	for _, d := range s.hub.All() {
		if d.Closed() {
			continue
		}
		if d.Connected {
			// A wizard is never idle-booted — and
			// `Wizard()` excludes QUELL, so a quelled
			// wizard is booted like anybody else.
			if boot && d.IdleSince(now) > maxIdle &&
				!isWizard(w, d.Player) {

				s.idleBoot(w, d)
			}
		} else if now.Sub(d.AcceptedAt) > loginTimeout {
			s.log.Info("connection screen: connection " +
				"timeout 300 secs")
			d.Close()
			continue
		}
		if !d.Connected || !ping || pingAfter <= 0 {
			continue
		}
		if now.Sub(d.LastSent()) <= pingAfter {
			continue
		}
		// `_/sys/no_idle_ping` on the player opts out. It is
		// read with `get_property_class`, so any string value
		// counts and an integer one does not.
		if v, ok := w.GetProp(d.Player, noIdlePingProp); ok &&
			v.Str != "" {

			continue
		}
		d.RequestKeepalive()
	}
}

// noIdlePingProp is NO_IDLE_PING_PROP (`game.h:82`).
const noIdlePingProp = "_/sys/no_idle_ping"

// idleBoot is `idleboot_user` (`interface.c:2246`): the message, then
// the connection goes.
//
// Upstream writes "\r\n%s\r\n\r\n" through
// `queue_immediate_and_flush`, so the message is surrounded by blank
// lines and the output queue is pushed before the socket closes.
// Emerald's descriptor is a stream of lines and the transport drains
// whatever is queued when Done fires, so the flush is already the
// shape of `Close`.
func (s *Server) idleBoot(w *world.World, d *session.Descriptor) {
	d.Send("")
	d.Send(w.Tune.String("idle_boot_mesg"))
	d.Send("")
	d.Close()
}
