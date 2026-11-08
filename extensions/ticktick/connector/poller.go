package connector

import (
	"errors"
	"sync"
	"time"

	"context"

	"aminte-ticktick/ticktick"
	"aminte/client"
)

// Poller runs the polls, one at a time. The timer of Serve and the sync action share it, so that two polls never run
// together and never write the state at once.
type Poller struct {
	TT  *ticktick.Client
	LL  *client.Client // writes as import:ticktick
	Now func() time.Time

	dir     string
	mu      sync.Mutex // held for the whole poll
	st      *State
	onLogin func() // called when TickTick refuses the token; set by Serve
}

// NewPoller loads the state in dir.
func NewPoller(tt *ticktick.Client, ll *client.Client, dir string) (*Poller, error) {
	st, err := LoadState(dir)
	if err != nil {
		return nil, err
	}
	return &Poller{TT: tt, LL: ll, Now: time.Now, dir: dir, st: st}, nil
}

// Poll waits for a poll in progress, then does one and saves the state. It returns the number of new entries. When
// TickTick answers 401 it returns ErrLoginAgain.
func (p *Poller) Poll(ctx context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n, err := Sync(ctx, p.TT, p.LL, p.st, p.Now())
	if err != nil {
		return n, p.check(err)
	}
	if err := p.st.Save(p.dir); err != nil {
		return n, err
	}
	return n, nil
}

// check turns a 401 of TickTick into ErrLoginAgain and tells Serve to stop. Any other error passes.
func (p *Poller) check(err error) error {
	var se *ticktick.StatusError
	if errors.As(err, &se) && se.Status == 401 {
		if p.onLogin != nil {
			p.onLogin()
		}
		return ErrLoginAgain
	}
	return err
}
