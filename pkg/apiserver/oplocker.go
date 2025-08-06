package apiserver

import (
	"fmt"
	"sync"
)

type lock struct {
	mu      sync.Mutex
	counter uint
}

type OpLockManager struct {
	mu       sync.Mutex
	locksKey map[string]*lock
}

func (olm *OpLockManager) Lock(name string) {
	olm.mu.Lock()
	l, ok := olm.locksKey[name]
	if !ok {
		l = &lock{}
		olm.locksKey[name] = l
	}
	l.counter++
	olm.mu.Unlock()
	l.mu.Lock()
}

func (olm *OpLockManager) TryLock(name string) bool {
	olm.mu.Lock()
	defer olm.mu.Unlock()

	l, ok := olm.locksKey[name]
	if !ok {
		l = &lock{counter: 1}
		olm.locksKey[name] = l
		l.mu.Lock()

		return true
	}
	if l.mu.TryLock() {
		l.counter++
		return true
	}
	return false
}

func (olm *OpLockManager) Unlock(name string) {
	olm.mu.Lock()

	l, ok := olm.locksKey[name]
	if !ok {
		return
	}
	l.counter--
	if l.counter == 0 {
		delete(olm.locksKey, name)
	}
	olm.mu.Unlock()
	l.mu.Unlock()
}

func NewLocker() *OpLockManager {
	return &OpLockManager{
		locksKey: make(map[string]*lock),
	}
}

func lockKey(app, owner string) string {
	return fmt.Sprintf("%s-%s", app, owner)
}
