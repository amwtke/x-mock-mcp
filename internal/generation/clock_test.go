package generation

import (
	"sync"
	"time"
)

type fakeTimer struct {
	at        time.Time
	fn        func()
	cancelled bool
}
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func newFakeClock() *fakeClock      { return &fakeClock{now: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)} }
func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) AfterFunc(d time.Duration, fn func()) func() {
	c.mu.Lock()
	timer := &fakeTimer{at: c.now.Add(d), fn: fn}
	c.timers = append(c.timers, timer)
	c.mu.Unlock()
	return func() { c.mu.Lock(); timer.cancelled = true; c.mu.Unlock() }
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []func()
	for _, t := range c.timers {
		if !t.cancelled && !t.at.After(c.now) {
			t.cancelled = true
			due = append(due, t.fn)
		}
	}
	c.mu.Unlock()
	for _, fn := range due {
		fn()
	}
}
