package game

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// SMTP security modes, upstream's smtp_tls_mode (formerly smtp_ssl_type).
// The numbering is the C library's, not anything sensible: STARTTLS is 0.
const (
	smtpStartTLS = 0
	smtpTLS      = 1
	smtpPlain    = 2
)

// SMTP authentication modes, upstream's smtp_auth_type. CRAM-MD5 is 0, which
// is also the default this server ships with.
const (
	smtpAuthCramMD5 = 0
	smtpAuthNone    = 1
	smtpAuthPlain   = 2
	smtpAuthLogin   = 3
)

// smtpTimeout bounds a send. It is generous, because the send happens off the
// world goroutine and nobody is waiting on it.
const smtpTimeout = 30 * time.Second

// SMTPConfigured implements muf.Host for SMTP_SEND's own "is this server set
// up to send mail at all" check, which is answered before anything is
// queued so the primitive can report it.
func (h *mufHost) SMTPConfigured() bool {
	return h.w.Tune.String("smtp_server") != "" && h.w.Tune.String("smtp_port") != ""
}

// SMTPModesValid reports whether the two mode parameters hold values the
// server knows, which upstream checks before sending rather than at the point
// they are set.
func (h *mufHost) SMTPModesValid() (tlsOK, authOK bool) {
	t := h.w.Tune.Int("smtp_tls_mode")
	a := h.w.Tune.Int("smtp_auth_type")
	return t >= 0 && t <= 2, a >= 0 && a <= 3
}

// SendMail implements muf.Host for SMTP_SEND.
//
// The send runs off the world goroutine, which is the one deliberate
// divergence here: upstream blocks its whole server for the round trip, and a
// mail server that has stopped answering would freeze every player for the
// length of a TCP timeout. The cost is that SMTP_SEND cannot report a
// delivery failure — it returns "accepted" once the message is queued, and a
// failure is logged rather than handed back. A program that needs to know
// whether mail arrived could not learn it from upstream's answer either,
// which only covers the conversation with the relay.
func (h *mufHost) SendMail(toEmail, toName, subject, body string, by ref.Ref) {
	cfg := smtpSettings{
		server:   h.w.Tune.String("smtp_server"),
		port:     h.w.Tune.String("smtp_port"),
		user:     h.w.Tune.String("smtp_user"),
		password: h.w.Tune.String("smtp_password"),
		fromAddr: h.w.Tune.String("smtp_from_email"),
		fromName: h.w.Tune.String("smtp_from_name"),
		tlsMode:  int(h.w.Tune.Int("smtp_tls_mode")),
		authMode: int(h.w.Tune.Int("smtp_auth_type")),
		noVerify: h.w.Tune.Bool("smtp_no_verify_cert"),
	}
	msg := buildMessage(cfg, toEmail, toName, subject, body)
	log := h.s.statusLog()
	who := by

	go func() {
		if err := sendMail(cfg, toEmail, msg); err != nil {
			log.Error("email send failed",
				"to", toEmail, "by", who.String(), "error", err)
			return
		}
		log.Info("email sent", "to", toEmail, "by", who.String())
	}()
}

// smtpSettings is the @tune parameters a send needs, read on the world
// goroutine and then owned by the sending one.
type smtpSettings struct {
	server, port       string
	user, password     string
	fromAddr, fromName string
	tlsMode, authMode  int
	noVerify           bool
}

// buildMessage assembles the RFC 5322 message. The body arrives with the
// caller's own line endings already normalised to CRLF by the primitive.
func buildMessage(cfg smtpSettings, toEmail, toName, subject, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", address(cfg.fromName, cfg.fromAddr))
	fmt.Fprintf(&b, "To: %s\r\n", address(toName, toEmail))
	fmt.Fprintf(&b, "Subject: %s\r\n", headerValue(subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}

// address renders a display name and an address as one header value.
func address(name, addr string) string {
	if name == "" {
		return addr
	}
	return `"` + headerValue(strings.ReplaceAll(name, `"`, "")) + `" <` + addr + ">"
}

// headerValue strips what would otherwise let a caller inject headers of
// their own. A subject is chosen by a MUF program, so a newline in it must
// not be able to add a Bcc.
func headerValue(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

// sendMail runs the conversation with the relay.
func sendMail(cfg smtpSettings, to, msg string) error {
	addr := net.JoinHostPort(cfg.server, cfg.port)
	tlsCfg := &tls.Config{
		ServerName:         cfg.server,
		InsecureSkipVerify: cfg.noVerify, //nolint:gosec // smtp_no_verify_cert is upstream's own opt-out
	}

	var conn net.Conn
	var err error
	dialer := &net.Dialer{Timeout: smtpTimeout}
	if cfg.tlsMode == smtpTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(smtpTimeout)); err != nil {
		return err
	}

	c, err := smtp.NewClient(conn, cfg.server)
	if err != nil {
		return err
	}
	defer func() { _ = c.Quit() }()

	if cfg.tlsMode == smtpStartTLS {
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if auth := smtpAuth(cfg); auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("authenticating: %w", err)
		}
	}

	if err := c.Mail(cfg.fromAddr); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	return w.Close()
}

// smtpAuth picks the authentication mechanism, or nil for none.
//
// CRAM-MD5 is upstream's default and Go's net/smtp implements it, but it is
// obsolete and most relays have withdrawn it; PLAIN over TLS is what a
// working configuration usually ends up as.
func smtpAuth(cfg smtpSettings) smtp.Auth {
	if cfg.user == "" || cfg.authMode == smtpAuthNone {
		return nil
	}
	switch cfg.authMode {
	case smtpAuthCramMD5:
		return smtp.CRAMMD5Auth(cfg.user, cfg.password)
	case smtpAuthPlain, smtpAuthLogin:
		// Go has no LOGIN mechanism. PLAIN carries the same credentials
		// and every relay offering LOGIN offers it too, so the two are
		// treated alike rather than one of them failing outright.
		return smtp.PlainAuth("", cfg.user, cfg.password, cfg.server)
	}
	return nil
}
