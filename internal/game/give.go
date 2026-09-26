package game

import (
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/match"
	"github.com/FatmanUK/fuzzball_emerald/internal/password"
	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

func init() {
	register("give", (*Server).cmdGive)
	register("read", (*Server).cmdLook)
	register("@newpassword", (*Server).cmdNewPassword)
}

// cmdGive is do_give (pennies.c): hand over currency.
//
// A wizard may give a negative amount, which takes it back, and may
// give to a *thing* — where it sets the thing's value outright
// rather than adding to somebody's purse, and says so in different
// words. Everyone else may only give a positive amount to a player
// who is not already at max_pennies.
func (s *Server) cmdGive(c *ctx) {
	name, amountArg, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	amount := leadingInt(strings.TrimSpace(amountArg))
	wizard := isWizard(c.w, ownerOf(c.w, c.who))

	if amount == 0 || (amount < 0 && !wizard) {
		c.tell("You must specify a valid number of %s.",
			c.w.Tune.String("pennies"))
		return
	}

	m := match.New(c.w, c.who, name).Neighbor().Me()
	if wizard {
		m = m.Player().Absolute()
	}
	who := m.Result()
	if !noisyMatch(c, name, who) {
		return
	}
	o := c.w.Get(who)

	if !wizard {
		if o.Type() != ref.TypePlayer {
			c.tell("You can only give to other players.")
			return
		}
		if valueOf(c.w, who)+int64(amount) >
			c.w.Tune.Int("max_pennies") {
			c.tell("That player doesn't need that many "+
				"%s!", c.w.Tune.String("pennies"))
			return
		}
	}
	if !s.payFor(c.w, c.who, amount) {
		c.tell("You don't have that many %s to give!",
			c.w.Tune.String("pennies"))
		return
	}

	switch o.Type() {
	case ref.TypePlayer:
		c.w.SetProp(who, propValue, props.Value{
			Type: props.Int,
			Num:  valueOf(c.w, who) + int64(amount)})
		me := nameOf(c.w, c.who)
		if amount >= 0 {
			c.tell("You give %d %s to %s.", amount,
				s.coins(c, amount), o.Name)
			s.notify(c.w, who, "%s gives you %d %s.", me,
				amount, s.coins(c, amount))
			return
		}
		c.tell("You take %d %s from %s.", -amount,
			s.coins(c, amount), o.Name)
		s.notify(c.w, who, "%s takes %d %s from you!", me,
			-amount, s.coins(c, -amount))
	case ref.TypeThing:
		// Giving to a thing sets its value rather than adding
		// to a purse, and reports the result rather than the
		// amount.
		c.w.SetProp(who, propValue, props.Value{
			Type: props.Int,
			Num:  valueOf(c.w, who) + int64(amount)})
		now := valueOf(c.w, who)
		c.tell("You change the value of %s to %d %s.", o.Name,
			now, s.coins(c, int(now)))
	default:
		c.tell("You can't give %s to that!",
			c.w.Tune.String("pennies"))
	}
}

// coins names the currency in the singular or the plural, which are
// separate @tune parameters.
//
// Upstream tests the *amount* rather than its magnitude in two of the
// four places, so a negative one is plural even at minus one. Both
// spellings are reproduced where they appear.
func (s *Server) coins(c *ctx, amount int) string {
	if amount == 1 {
		return c.w.Tune.String("penny")
	}
	return c.w.Tune.String("pennies")
}

// cmdNewPassword is do_newpassword: set somebody else's password.
//
// Two guards are upstream's and both matter: God's password cannot be
// changed by anyone else, and a true wizard's only by God — so the
// command cannot be used to take over an account that could undo it.
func (s *Server) cmdNewPassword(c *ctx) {
	name, pass, _ := strings.Cut(c.arg, "=")
	name = strings.TrimSpace(name)
	pass = strings.TrimSpace(pass)

	victim, ok := c.w.PlayerNamed(name)
	if !ok {
		c.tell("No such player.")
		return
	}
	if pass == "" {
		c.tell("Invalid password.")
		return
	}
	if victim == ref.God && c.who != ref.God {
		c.tell("You can't change God's password!")
		return
	}
	o := c.w.Get(victim)
	if o.Flags.IsTrueWizard() && c.who != ref.God {
		c.tell("Only God can change a wizard's password.")
		return
	}

	hashed, err := password.Hash(pass)
	if err != nil {
		c.tell("That password could not be used.")
		return
	}
	o.PasswordHash = hashed
	c.w.Modified(victim)
	c.tell("Password changed.")
	s.notify(c.w, victim, "Your password has been changed by %s.",
		nameOf(c.w, c.who))
	s.securityLog().Warn("password changed for another player",
		"by", c.who.String(), "byName", nameOf(c.w, c.who),
		"player", victim.String(), "name", o.Name)
}
