package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The sessions of the browser (docs/plans/160-the-lock.md). A browser logs in with the token and gets a session cookie: a
// random id and its HMAC. This program keeps the state of each session in memory. A session that is idle for longer than
// lock_after, or that the owner locked, gets the lock page and nothing of the life log, until the PIN opens it.

// sessionCookie is the credential of the browser, set by POST /login. Its format and its key are fixed (newSessions), so a
// cookie made earlier with the same token is still valid, and the owner logs in only once.
const sessionCookie = "lifelog_session"

// legacyCookie held the token itself in the oldest login. It is no credential: any request that carries it clears it.
const legacyCookie = "lifelog_token"

// loginMaxAge is one year: an installed app on a phone loses a session cookie when the system ends the app. The owner
// ends the login by a new token.
const loginMaxAge = 365 * 24 * 60 * 60

// wrongPINLimit is the count of wrong PINs that ends a session: the browser must log in with the token again.
const wrongPINLimit = 5

// idleSlack is added to lock_after. The page script sends POST /awake at most once in 30 seconds, so the program waits
// one minute more than the setting before it counts the session idle.
const idleSlack = time.Minute

// forgetAfter bounds the memory. The state of a session that no request touched for a day is dropped: the longest
// setting is 60 minutes, so such a session is idle past the limit and a request finds it locked, with or without
// the state. The ended ids are kept for the life of the cookie, one year. Pruning runs at most once in a minute.
const (
	forgetAfter = 24 * time.Hour
	pruneEvery  = time.Minute
)

// sessionState is what the program remembers of one session.
type sessionState struct {
	active time.Time // the last activity of the owner (countsAsActivity)
	seen   time.Time // the last request of any kind, for forgetAfter
	locked bool
	wrong  int // the wrong PINs since the last unlock
}

// sessions is the memory of the sessions, with the key that signs their cookies. All state is under one mutex, and no
// goroutine runs. A time comes from the caller, so a test fixes it.
type sessions struct {
	key        []byte // the HMAC key, derived from the token: a new token ends every session
	mu         sync.Mutex
	state      map[string]*sessionState
	ended      map[string]time.Time // session ids that logged out or used up their PINs, with the time they ended
	lastPruned time.Time
}

func newSessions(token []byte) *sessions {
	m := hmac.New(sha256.New, token)
	m.Write([]byte("lifelog session")) // the key is HMAC-SHA256(token, "lifelog session"): a cookie of the same format stays valid
	return &sessions{key: m.Sum(nil), state: map[string]*sessionState{}, ended: map[string]time.Time{}}
}

// The cookie value is ID.MAC in hex: ID is 16 random bytes, MAC the HMAC-SHA256 of the ID. No list of sessions is on disk.
func (ss *sessions) macOf(id string) string {
	m := hmac.New(sha256.New, ss.key)
	m.Write([]byte(id))
	return hex.EncodeToString(m.Sum(nil))
}

// issue makes the cookie value of a new session, and its id.
func (ss *sessions) issue() (value, id string, err error) {
	b := make([]byte, 16)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	id = hex.EncodeToString(b)
	return id + "." + ss.macOf(id), id, nil
}

// idOf is the id of the valid session cookie of r. A forged or changed cookie, and the cookie of an ended session, are none.
func (ss *sessions) idOf(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	id, mac, ok := strings.Cut(c.Value, ".")
	if !ok || len(id) != 32 || !hmac.Equal([]byte(mac), []byte(ss.macOf(id))) || ss.isEnded(id) {
		return "", false
	}
	return id, true
}

// setCookie sets the session cookie. It is not Secure: this program serves plain HTTP on the loopback, and the proxy of
// the owner ends TLS.
func setCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}

// clearLegacy clears the cookie lifelog_token of an older login.
func clearLegacy(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: legacyCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

// prune drops what is old. The caller holds the mutex.
func (ss *sessions) prune(now time.Time) {
	if now.Sub(ss.lastPruned) < pruneEvery {
		return
	}
	ss.lastPruned = now
	for id, st := range ss.state {
		if now.Sub(st.seen) > forgetAfter {
			delete(ss.state, id)
		}
	}
	for id, at := range ss.ended {
		if now.Sub(at) > loginMaxAge*time.Second {
			delete(ss.ended, id)
		}
	}
}

func (ss *sessions) isEnded(id string) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	_, ended := ss.ended[id]
	return ended
}

// open records a new session, after the login: it is open and active now.
func (ss *sessions) open(id string, now time.Time) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.prune(now)
	ss.state[id] = &sessionState{active: now, seen: now}
}

// locked reports whether the lock holds the session shut at now, and records the request. A session with no state
// (the program started after the login) is locked when the lock is on. idle is lock_after plus idleSlack.
func (ss *sessions) locked(id string, now time.Time, lockOn bool, idle time.Duration) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.prune(now)
	st, known := ss.state[id]
	if !known {
		st = &sessionState{active: now, locked: lockOn}
		ss.state[id] = st
	}
	st.seen = now
	if lockOn && now.Sub(st.active) > idle {
		st.locked = true
	}
	return lockOn && st.locked
}

// activity records the owner's activity of an open session.
func (ss *sessions) activity(id string, now time.Time) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if st, ok := ss.state[id]; ok && !st.locked {
		st.active = now
	}
}

// lock locks the session now. It keeps the count of wrong PINs: a lock must not give the tries back. An ended
// session gets no state again: a request that was held while it ended must not bring it back.
func (ss *sessions) lock(id string, now time.Time) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if _, ended := ss.ended[id]; ended {
		return
	}
	st, ok := ss.state[id]
	if !ok {
		st = &sessionState{}
		ss.state[id] = st
	}
	st.active, st.seen, st.locked = now, now, true
}

// tryPIN counts a try before the PIN is compared. Requests that the program holds while their bodies come, or that
// come at once, then get no more than wrongPINLimit tries together. It is false for an ended session and for one that
// has used its tries.
func (ss *sessions) tryPIN(id string, now time.Time) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if _, ended := ss.ended[id]; ended {
		return false
	}
	st, ok := ss.state[id]
	if !ok {
		st = &sessionState{locked: true}
		ss.state[id] = st
	}
	st.seen = now
	if st.wrong >= wrongPINLimit {
		return false
	}
	st.wrong++
	return true
}

// opened opens the session after the right PIN, and gives the tries back. It is false when the session ended while
// the PIN was compared.
func (ss *sessions) opened(id string, now time.Time) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if _, ended := ss.ended[id]; ended {
		return false
	}
	ss.state[id] = &sessionState{active: now, seen: now}
	return true
}

// failed records a wrong PIN, whose try tryPIN has counted. It returns the count, and ends the session when the
// tries are used up.
func (ss *sessions) failed(id string, now time.Time) (n int, ended bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if _, done := ss.ended[id]; done {
		return wrongPINLimit, true
	}
	if st, ok := ss.state[id]; ok {
		n = st.wrong
	}
	if n >= wrongPINLimit {
		delete(ss.state, id)
		ss.ended[id] = now
		return n, true
	}
	return n, false
}

func (ss *sessions) end(id string, now time.Time) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	delete(ss.state, id)
	ss.ended[id] = now
}
