package session

import "sync"

type State struct {
	mu        sync.RWMutex
	masterKey []byte
	unlocked  bool
}

func NewState() *State {
	return &State{}
}

func (s *State) Unlock(masterKey []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	zero(s.masterKey)
	s.masterKey = append([]byte(nil), masterKey...)
	s.unlocked = true
}

func (s *State) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	zero(s.masterKey)
	s.masterKey = nil
	s.unlocked = false
}

func (s *State) IsUnlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.unlocked
}

func (s *State) MasterKeyCopy() ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.unlocked || len(s.masterKey) == 0 {
		return nil, false
	}
	return append([]byte(nil), s.masterKey...), true
}

func zero(buf []byte) {
	for i := range buf {
		buf[i] = 0
	}
}
