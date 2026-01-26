package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/krateoplatformops/deviser/internal/util/signals"
)

// Run runs a service loop with graceful shutdown.
//
// The service function receives a context that gets cancelled when the
// process receives SIGTERM/SIGINT.
//
// Optionally, callbacks can be provided for starting/stopping resources
// (DB, HTTP server, etc.).
func Run(
	log *slog.Logger,
	service func(ctx context.Context) error,
	onShutdown ...func(context.Context) error,
) error {
	// Base context for the service
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start goroutine waiting for OS signals
	go func() {
		<-signals.WaitForSignal()
		log.Info("received shutdown signal")
		cancel()
	}()

	// Run the service logic
	err := service(ctx)

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	for _, fn := range onShutdown {
		if fn != nil {
			_ = fn(shutdownCtx)
		}
	}

	return err
}
