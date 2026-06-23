package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	proxyproto "github.com/pires/go-proxyproto"
)

// Config is read from environment variables so the same binary works locally
// and inside the container without code changes.
type Config struct {
	Addr     string // address to listen on, e.g. ":8443"
	CertFile string // path to TLS certificate (PEM)
	KeyFile  string // path to TLS private key (PEM)
	// ProxyProtocol enables parsing of the PROXY protocol header that nginx
	// prepends when running as a stream (SSL passthrough) proxy. This is how
	// we recover the real client IP behind nginx.
	ProxyProtocol bool
}

func loadConfig() Config {
	return Config{
		Addr:          getenv("LISTEN_ADDR", ":8443"),
		CertFile:      getenv("TLS_CERT_FILE", "/etc/tls/tls.crt"),
		KeyFile:       getenv("TLS_KEY_FILE", "/etc/tls/tls.key"),
		ProxyProtocol: getenv("PROXY_PROTOCOL", "true") == "true",
	}
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func main() {
	cfg := loadConfig()

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		log.Fatalf("failed to load TLS keypair (cert=%s key=%s): %v", cfg.CertFile, cfg.KeyFile, err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// 1. Plain TCP listener.
	rawLn, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", cfg.Addr, err)
	}

	// 2. Wrap with PROXY protocol so we can read the original client address
	//    that nginx forwards. With ssl_preread + proxy_protocol on, nginx
	//    passes the encrypted bytes through untouched but prepends a small
	//    header describing the real client.
	var ln net.Listener = rawLn
	if cfg.ProxyProtocol {
		ln = &proxyproto.Listener{
			Listener: rawLn,
			// Require the header — connections without it (other than the
			// loopback health check below) are rejected. REQUIRE is the safe
			// choice once nginx is the only ingress; use OPTIONAL while testing
			// by hitting the service directly.
			Policy: func(upstream net.Addr) (proxyproto.Policy, error) {
				return proxyproto.REQUIRE, nil
			},
			ReadHeaderTimeout: 5 * time.Second,
		}
		log.Printf("PROXY protocol parsing enabled")
	}

	// 3. We terminate TLS ourselves, on top of the (proxy-protocol-aware) listener.
	tlsLn := tls.NewListener(ln, tlsConfig)

	srv := &http.Server{
		Handler:           http.HandlerFunc(handle),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM (container stop).
	idleClosed := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Printf("shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown error: %v", err)
		}
		close(idleClosed)
	}()

	log.Printf("listening on %s (TLS terminated in-process)", cfg.Addr)
	if err := srv.Serve(tlsLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
	<-idleClosed
}

func handle(w http.ResponseWriter, r *http.Request) {
	clientIP := realClientIP(r)

	log.Printf("%s %s%s from client=%s (transport=%s tls=%s)",
		r.Method, r.Host, r.URL.Path, clientIP, r.RemoteAddr, tlsVersion(r))

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "hello from go-proxy-pass\n")
	fmt.Fprintf(w, "real client ip: %s\n", clientIP)
	fmt.Fprintf(w, "remote addr:    %s\n", r.RemoteAddr)
	fmt.Fprintf(w, "tls version:    %s\n", tlsVersion(r))
	fmt.Fprintf(w, "sni host:       %s\n", r.TLS.ServerName)
}

// realClientIP returns the originating client IP.
//
// Because TLS is terminated here (not at nginx), there is no X-Forwarded-For
// header to trust. Instead, r.RemoteAddr is already the *real* client address:
// the proxyproto listener rewrote the connection's remote address using the
// PROXY protocol header that nginx sent. So we simply read it off RemoteAddr.
func realClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func tlsVersion(r *http.Request) string {
	if r.TLS == nil {
		return "none"
	}
	switch r.TLS.Version {
	case tls.VersionTLS13:
		return "1.3"
	case tls.VersionTLS12:
		return "1.2"
	case tls.VersionTLS11:
		return "1.1"
	case tls.VersionTLS10:
		return "1.0"
	default:
		return fmt.Sprintf("0x%04x", r.TLS.Version)
	}
}
