package connector

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"aminte-ticktick/ticktick"
	"aminte/app"
)

// Serve runs the app under `aminte serve --with ticktick`. It serves the task service on AMINTE_LISTEN, polls once at the
// start and then every interval, and returns when ctx ends. When TickTick refuses the token, in the timer or in a
// handler, it stops and returns ErrLoginAgain: the owner acts first.
func Serve(ctx context.Context, p *Poller, lists []ticktick.Project, every time.Duration) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	p.onLogin = func() { cancel(ErrLoginAgain) }

	var loop sync.WaitGroup
	loop.Add(1)
	go func() { // owned by Serve: it ends with ctx, and Serve waits for it
		defer loop.Done()
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			n, err := p.Poll(ctx)
			switch {
			case errors.Is(err, ErrLoginAgain), ctx.Err() != nil:
				return
			case err != nil:
				log.Printf("poll failed: %v", err) // the next poll comes after the interval; sync on demand is the retry
			default:
				log.Printf("poll: wrote %d entries", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()

	err := app.Listen(ctx, Service(p, lists))
	cancel(nil) // a clean end of Listen must stop the loop too
	loop.Wait()
	if errors.Is(context.Cause(ctx), ErrLoginAgain) {
		return ErrLoginAgain
	}
	return err
}
