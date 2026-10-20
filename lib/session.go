package lib

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

var sessionStore = NewMemoryStore(12 * time.Hour)

type SessionData struct {
	UserID             int64
	ExpiresAt          time.Time
	CredentialsVersion [32]byte
}

type MemoryStore struct {
	sync.RWMutex
	data map[string]SessionData
}

func NewMemoryStore(cleanupInterval time.Duration) *MemoryStore {
	store := &MemoryStore{data: make(map[string]SessionData)}
	go store.gc(cleanupInterval)
	return store
}

func GetSession(sid string) (SessionData, bool) {
	return sessionStore.Get(sid)
}

func SetSession(sid string, data SessionData) error {
	sessionStore.Set(sid, data)
	return nil
}

func DeleteSession(sid string) {
	sessionStore.Delete(sid)
}

func (s *MemoryStore) Get(sid string) (SessionData, bool) {
	s.RLock()
	data, ok := s.data[sid]
	s.RUnlock()
	if !ok {
		return SessionData{}, false
	}
	if time.Now().After(data.ExpiresAt) {
		s.Delete(sid)
		return SessionData{}, false
	}
	return data, true
}

func (s *MemoryStore) Set(sid string, data SessionData) {
	s.Lock()
	defer s.Unlock()
	s.data[sid] = data
}

func (s *MemoryStore) Delete(sid string) {
	s.Lock()
	defer s.Unlock()
	delete(s.data, sid)
}

func (s *MemoryStore) gc(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		now := time.Now()
		s.Lock()
		for sid, data := range s.data {
			if now.After(data.ExpiresAt) {
				delete(s.data, sid)
			}
		}
		s.Unlock()
	}
}

func GenerateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
