package web

// Settings are the choices of the owner that only the browser needs, and that the core does not hold.
type Settings struct {
	// Ground is the floor of the page: boards, oak, firelight or embers.
	Ground string
	// LockMinutes is the minutes of idle time after which the lock closes the page; 0 means that the lock is off.
	LockMinutes int
}

// settings reads the choices of the browser. It is the one place where they are read. Until the browser keeps its own
// settings, every page has the boards and no lock.
func (b *Browser) settings() Settings { return Settings{Ground: "boards"} }
