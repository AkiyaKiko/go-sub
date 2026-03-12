package lib

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

var GLOBAL_SESSION_STORE *MemoryStore

func init() {
	GLOBAL_SESSION_STORE = NewMemoryStore(4320 * time.Hour)
}

func GetSession(sid string) (SessionData, bool) {
	return GLOBAL_SESSION_STORE.Get(sid)
}

func SetSession(sid string, data SessionData) {
	GLOBAL_SESSION_STORE.Set(sid, data)
}

func DeleteSession(sid string) {
	GLOBAL_SESSION_STORE.Delete(sid)
}

type SessionData struct {
	UserID    int64
	ExpiresAt time.Time
}

type MemoryStore struct {
	sync.RWMutex
	data map[string]SessionData
}

// 1. 增加构造函数，并启动自动清理协程
func NewMemoryStore(cleanupInterval time.Duration) *MemoryStore {
	s := &MemoryStore{
		data: make(map[string]SessionData),
	}
	go s.gc(cleanupInterval)
	return s
}

func (s *MemoryStore) Get(sid string) (SessionData, bool) {
	s.RLock()
	defer s.RUnlock()
	d, ok := s.data[sid]
	// 即使存在，如果过期了也返回 false
	if !ok || time.Now().After(d.ExpiresAt) {
		return SessionData{}, false
	}
	return d, true
}

func (s *MemoryStore) Set(sid string, d SessionData) {
	s.Lock()
	defer s.Unlock()
	s.data[sid] = d
}

func (s *MemoryStore) Delete(sid string) {
	s.Lock()
	defer s.Unlock()
	delete(s.data, sid)
}

// 3. 内部清理逻辑 (Garbage Collection)
func (s *MemoryStore) gc(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		s.Lock()
		now := time.Now()
		for sid, d := range s.data {
			if now.After(d.ExpiresAt) {
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
