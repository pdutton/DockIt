package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"time"
)

// loadCertificate reads the TLS certificate and key that serve presents, and
// logs who the certificate is for and when it expires.  Its errors name the
// file at fault.
func loadCertificate(log *slog.Logger, certFile, keyFile string) (*tls.Certificate, error) {
	certPEM, err := readTLSFile("certificate", certFile)
	if err != nil {
		return nil, err
	}
	keyPEM, err := readTLSFile("key", keyFile)
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("TLS certificate %s and key %s: %w", certFile, keyFile, err)
	}
	leaf := cert.Leaf
	log.Info("TLS certificate loaded", "file", certFile, "subject", leaf.Subject.String(),
		"names", strings.Join(certNames(leaf), ","), "expires", leaf.NotAfter.UTC().Format(time.RFC3339))
	if time.Now().After(leaf.NotAfter) {
		log.Warn("the TLS certificate has expired; browsers will refuse it", "file", certFile,
			"expired", leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	return &cert, nil
}

func readTLSFile(what, name string) ([]byte, error) {
	b, err := os.ReadFile(name)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return nil, fmt.Errorf("TLS %s: %w; the user DockIt runs as must be able to read it", what, err)
	case err != nil:
		return nil, fmt.Errorf("TLS %s: %w", what, err)
	}
	return b, nil
}

// certNames returns the host names and addresses a certificate is valid for.
func certNames(c *x509.Certificate) []string {
	names := append([]string(nil), c.DNSNames...)
	for _, ip := range c.IPAddresses {
		names = append(names, ip.String())
	}
	return names
}
