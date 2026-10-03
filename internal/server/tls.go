package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"time"

	"pi-chat-gateway/internal/db"
	"pi-chat-gateway/internal/llm"
)

// EnsureSelfSignedCert creates certFile/keyFile if missing, with SANs for
// localhost + the given extra hosts/IPs. Returns nil if files already exist.
func EnsureSelfSignedCert(certFile, keyFile string, hosts []string) error {
	if _, err := os.Stat(certFile); err == nil {
		if _, err2 := os.Stat(keyFile); err2 == nil {
			return nil // both exist — reuse
		}
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("serial: %w", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "vividMistress LAN"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	seen := map[string]bool{}
	addHost := func(h string) {
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	addHost("localhost")
	for _, h := range hosts {
		addHost(h)
	}
	// Always include common LAN/loopback IPs so the cert works regardless
	// of which interface the phone reaches.
	for _, ip := range localIPv4s() {
		addHost(ip)
	}
	addHost("127.0.0.1")
	addHost("::1")
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("create cert: %w", err)
	}
	certOut, err := os.Create(certFile)
	if err != nil {
		return err
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		certOut.Close()
		return err
	}
	certOut.Close()
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		keyOut.Close()
		return err
	}
	keyOut.Close()
	log.Printf("generated self-signed cert %s (SANs: dns=%v ip=%v)", certFile, tmpl.DNSNames, tmpl.IPAddresses)
	return nil
}

func localIPv4s() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				out = append(out, v4.String())
			}
		}
	}
	return out
}

// RunTLSWithConfig starts the HTTPS server on addr with the given back-end config.
func RunTLSWithConfig(addr string, store db.Store, llmClient *llm.Client, cfg Config, certFile, keyFile string) error {
	s := NewWithConfig(store, llmClient, cfg)
	return s.RunTLS(addr, certFile, keyFile)
}

// RunTLS serves the server's handler over TLS.
func (s *Server) RunTLS(addr, certFile, keyFile string) error {
	// Verify cert loads before announcing.
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return fmt.Errorf("load TLS cert: %w", err)
	}
	log.Printf("Vivid Mistress HTTPS listening on %s", addr)
	return http.ListenAndServeTLS(addr, certFile, keyFile, logMiddleware(s.Handler()))
}
