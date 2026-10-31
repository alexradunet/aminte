package relay

import (
	"context"
	"fmt"
	"log"
	"time"

	"lifelog/client"
)

// OwnerEvery is the time between two reads of the settings while no owner number is set.
var OwnerEvery = 30 * time.Second

// WaitOwner reads the owner's number from GET /settings (the property owner_number) with c, which has the owner's
// token and no source. With none set, it logs one line, then reads again at each tick (nil: every OwnerEvery) until
// a number is set. It returns the error of ctx when ctx ends. A number that is set is used for the whole run.
func WaitOwner(ctx context.Context, c *client.Client, logger *log.Logger, tick <-chan time.Time) (string, error) {
	if tick == nil {
		t := time.NewTicker(OwnerEvery)
		defer t.Stop()
		tick = t.C
	}
	last := "" // the line said last: each state is said once, and again when it changes
	for {
		e, err := c.Get(ctx, "/settings")
		line := ""
		switch {
		case ctx.Err() != nil:
			return "", ctx.Err()
		case err != nil:
			line = fmt.Sprintf("settings: %v", err)
		default:
			p, _ := e.Properties.(map[string]any)
			if n, _ := p["owner_number"].(string); n != "" {
				return n, nil
			}
			line = "no owner number: set it in Settings (owner_number)"
		}
		if line != last {
			logger.Print(line)
			last = line
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-tick:
		}
	}
}
