package signals

import (
	"os"
	"os/signal"
	"syscall"
)

// WaitForSignal returns a channel that closes on SIGTERM or SIGINT.
func WaitForSignal() <-chan struct{} {
	sig := make(chan os.Signal, 1)
	done := make(chan struct{})

	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sig
		close(done)
	}()

	return done
}
