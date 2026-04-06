package session

import (
	"errors"
	"sync"
	"time"
)

// InMemoryTokenStore - хранилище для access_token в памяти процесса
// Не персистентно - при перезапуске токены теряются
type InMemoryTokenStore struct {
	mu      sync.RWMutex
	tokens  map[string]*tokenEntry // sessionID -> tokenEntry
	cleanup time.Duration
}

type tokenEntry struct {
	accessToken string
	expiresAt   time.Time
}

// NewInMemoryTokenStore создаёт хранилище для access token в памяти
func NewInMemoryTokenStore(cleanupInterval time.Duration) *InMemoryTokenStore {
	store := &InMemoryTokenStore{
		tokens:  make(map[string]*tokenEntry),
		cleanup: cleanupInterval,
	}
	go store.cleanupExpired()
	return store
}

// Set сохраняет access token для session
func (s *InMemoryTokenStore) Set(sessionID string, accessToken string, expiresAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[sessionID] = &tokenEntry{
		accessToken: accessToken,
		expiresAt:   expiresAt,
	}
}

// Get возвращает access token для session
func (s *InMemoryTokenStore) Get(sessionID string) (string, time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.tokens[sessionID]
	if !ok {
		return "", time.Time{}, errors.New("token not found")
	}

	if time.Now().After(entry.expiresAt) {
		return "", time.Time{}, errors.New("token expired")
	}

	return entry.accessToken, entry.expiresAt, nil
}

// Delete удаляет token для session
func (s *InMemoryTokenStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, sessionID)
}

// Refresh обновляет access token
func (s *InMemoryTokenStore) Refresh(sessionID string, accessToken string, expiresAt time.Time) {
	s.Set(sessionID, accessToken, expiresAt)
}

// GetSessionData возвращает данные для middleware (только access token)
func (s *InMemoryTokenStore) GetSessionData(sessionID string) (string, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.tokens[sessionID]
	if !ok {
		return "", time.Time{}
	}

	return entry.accessToken, entry.expiresAt
}

func (s *InMemoryTokenStore) cleanupExpired() {
	ticker := time.NewTicker(s.cleanup)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, entry := range s.tokens {
			if now.After(entry.expiresAt) {
				delete(s.tokens, id)
			}
		}
		s.mu.Unlock()
	}
}