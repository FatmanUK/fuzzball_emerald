package config

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// BuildTLS turns the configuration into a usable tls.Config.
//
// Fuzzball took an OpenSSL cipher string, which Go's crypto/tls
// cannot consume in any form. A named policy replaces it: the choice
// is between requiring TLS 1.3 and also allowing 1.2 for older
// clients, which is the only decision an operator actually needs to
// make.
func (c Config) BuildTLS(log *slog.Logger) (*tls.Config, error) {
	if c.TLS.KeyPassword != "" {
		// Go removed support for encrypted PEM keys because
		// the format is not authenticated. Saying so is
		// better than failing to parse the file with a vague
		// error.
		return nil, fmt.Errorf("encrypted private keys are not supported; " +
			"decrypt the key file and protect it with file permissions instead")
	}

	loader := &certLoader{
		certFile: c.TLS.CertFile,
		keyFile:  c.TLS.KeyFile,
		log:      log,
	}
	if err := loader.load(); err != nil {
		return nil, err
	}

	cfg := &tls.Config{
		GetCertificate: loader.get,
		MinVersion:     tls.VersionTLS13,
	}
	if c.TLS.Policy == PolicyCompat {
		cfg.MinVersion = tls.VersionTLS12
		// Only the AEAD suites, and only with forward
		// secrecy. Go chooses the order itself; there is no
		// knob for that any more and there should not be.
		cfg.CipherSuites = []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		}
	}

	if c.TLS.AutoReload {
		loader.startReloading()
	}
	return cfg, nil
}

// certLoader holds the current certificate and can swap it without a
// restart, which is what replaces Fuzzball's @reconfigure_ssl.
type certLoader struct {
	certFile, keyFile string
	log               *slog.Logger

	mu   sync.RWMutex
	cert *tls.Certificate
	// modTime is when the certificate we hold was loaded, used to
	// notice a replacement on disk.
	loadedAt time.Time
}

func (l *certLoader) load() error {
	cert, err := tls.LoadX509KeyPair(l.certFile, l.keyFile)
	if err != nil {
		return fmt.Errorf("loading the TLS certificate: %w", err)
	}
	l.mu.Lock()
	l.cert, l.loadedAt = &cert, time.Now()
	l.mu.Unlock()
	return nil
}

func (l *certLoader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.cert == nil {
		return nil, fmt.Errorf("no certificate loaded")
	}
	return l.cert, nil
}

// reloadInterval is how often the certificate files are re-read when
// auto-reload is on. Certificates change on the order of months;
// checking every minute is already generous.
const reloadInterval = time.Minute

func (l *certLoader) startReloading() {
	go func() {
		for range time.Tick(reloadInterval) {
			if err := l.load(); err != nil &&
				l.log != nil {
				// Keep serving with the certificate
				// we have.
				l.log.Error("could not reload the TLS certificate", "error", err)
			}
		}
	}()
}
