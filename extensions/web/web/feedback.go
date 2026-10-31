package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sync"
	"time"
)

const (
	feedbackLimit    = 64 << 10
	feedbackEntries  = 64
	feedbackLifetime = 10 * time.Minute
)

// feedbackReceipt is what the toast after a write needs: the page it belongs to, the text, and when it expires.
// Receipts are private, per-process, one-use presentation state, not sessions. A restart or an expiry loses only the
// toast, never the saved resource.
type feedbackReceipt struct {
	path, text string
	expires    time.Time
}

type feedbackStore struct {
	mu      sync.Mutex
	entries map[string]feedbackReceipt
	now     func() time.Time // the clock of the receipts
}

// put keeps the result of a write for the page at destination, and returns the destination with the receipt in its
// query. A write with no result has no receipt. The result is a JSON value, or a json.RawMessage.
func (s *feedbackStore) put(destination string, result any) string {
	if result == nil {
		return destination
	}
	u, err := url.Parse(destination)
	if err != nil {
		return destination
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return destination
	}
	// Keep a bounded textual preview rather than a whole entity or request. The
	// explicit notice distinguishes an incomplete list from a complete outcome.
	const notice = "\nFeedback truncated: more result details were omitted from this temporary preview."
	if len(b) > feedbackLimit {
		b = append(b[:feedbackLimit-len(notice):feedbackLimit-len(notice)], notice...)
	}
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		return destination
	}
	token := hex.EncodeToString(random[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.entries == nil {
		s.entries = make(map[string]feedbackReceipt)
	}
	s.prune(now)
	if len(s.entries) >= feedbackEntries {
		var oldest string
		var expiry time.Time
		for key, r := range s.entries {
			if oldest == "" || r.expires.Before(expiry) {
				oldest, expiry = key, r.expires
			}
		}
		delete(s.entries, oldest)
	}
	s.entries[token] = feedbackReceipt{u.Path, string(b), now.Add(feedbackLifetime)}
	q := u.Query()
	q.Set(feedbackField, token)
	u.RawQuery = q.Encode()
	return u.String()
}

// move re-keys a receipt to another path: a GET with a receipt that is answered with a redirect (the page of a live
// metric, to its series) keeps its toast at the address it lands on.
func (s *feedbackStore) move(token, from, to string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.entries[token]; ok && r.path == from {
		r.path = to
		s.entries[token] = r
	}
}

// take returns the text of the receipt for path and deletes it: a receipt is read once, and only at its own path.
func (s *feedbackStore) take(path, token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(s.now())
	r, ok := s.entries[token]
	if !ok || r.path != path {
		return ""
	}
	delete(s.entries, token)
	return r.text
}

// prune deletes the receipts that expired at now. The caller holds mu.
func (s *feedbackStore) prune(now time.Time) {
	for key, r := range s.entries {
		if !now.Before(r.expires) {
			delete(s.entries, key)
		}
	}
}
