package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"time"
)

// Run starts the server on addr and blocks until the server exits or ctx
// is cancelled. ctx cancellation triggers a graceful shutdown with a 10s
// timeout. Mirrors `Server.run(port)` in BGIO.
func (s *Server) Run(ctx context.Context, addr string) error {
	return s.run(ctx, addr, nil, "", "")
}

// RunTLS starts the server on addr with TLS configured from certFile /
// keyFile (PEM-encoded). Equivalent to BGIO's
// `Server({ https: { cert, key } }).run(port)`.
func (s *Server) RunTLS(ctx context.Context, addr, certFile, keyFile string) error {
	return s.run(ctx, addr, nil, certFile, keyFile)
}

// RunTLSConfig starts the server on addr using a fully-built *tls.Config.
// Use this when you're loading certs from somewhere other than disk
// (e.g. an HSM or ACME helper).
func (s *Server) RunTLSConfig(ctx context.Context, addr string, cfg *tls.Config) error {
	return s.run(ctx, addr, cfg, "", "")
}

func (s *Server) run(ctx context.Context, addr string, cfg *tls.Config, certFile, keyFile string) error {
	srv := &http.Server{
		Addr:        addr,
		Handler:     s,
		TLSConfig:   cfg,
		ReadTimeout: 30 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		var err error
		switch {
		case cfg != nil:
			err = srv.ListenAndServeTLS("", "")
		case certFile != "" || keyFile != "":
			err = srv.ListenAndServeTLS(certFile, keyFile)
		default:
			err = srv.ListenAndServe()
		}
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return <-errCh
	}
}
