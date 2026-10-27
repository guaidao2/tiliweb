package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Serve runs srv on ln and shuts down gracefully when ctx is canceled. When
// useTLS is true, srv.TLSConfig must already contain a certificate.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, useTLS bool, shutdownTimeout time.Duration, logger *slog.Logger) error {
	if srv == nil {
		return fmt.Errorf("nil http server")
	}
	if ln == nil {
		return fmt.Errorf("nil listener")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = 15 * time.Second
	}

	errCh := make(chan error, 1)
	go func() {
		var err error
		if useTLS {
			err = srv.ServeTLS(ln, "", "")
		} else {
			err = srv.Serve(ln)
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("http server shutting down", "addr", ln.Addr().String())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("http shutdown failed", "error", err)
			_ = srv.Close()
			return err
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
