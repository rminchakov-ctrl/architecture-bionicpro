package session

import (
	"bionicpro-auth/models"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"golang.org/x/crypto/nacl/secretbox"
)

type memoryStore struct {
	mu        sync.RWMutex
	db        map[string]*models.Session
	ttl       time.Duration
	cryptoKey [32]byte
}

// NewMemoryStore создаёт in-memory хранилище с шифрованием
func NewMemoryStore(ttl time.Duration, keyBase64 string) (Store, error) {
	raw, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, err
	}
	if len(raw) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	var k [32]byte
	copy(k[:], raw)

	store := &memoryStore{
		db:        make(map[string]*models.Session),
		ttl:       ttl,
		cryptoKey: k,
	}

	// Запускаем cleanup для expired сессий
	go store.cleanupExpired()

	return store, nil
}

func (m *memoryStore) Save(s *models.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	encRefresh, err := m.encrypt(s.RefreshToken)
	if err != nil {
		return err
	}

	tmp := *s
	tmp.RefreshToken = []byte(encRefresh)

	m.db[s.ID] = &tmp
	return nil
}

func (m *memoryStore) Get(id string) (*models.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.db[id]
	if !ok {
		return nil, errors.New("session not found")
	}

	decRefresh, err := m.decrypt(string(s.RefreshToken))
	if err != nil {
		return nil, err
	}

	result := *s
	result.RefreshToken = decRefresh
	return &result, nil
}

func (m *memoryStore) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.db, id)
	return nil
}

func (m *memoryStore) Close() error { return nil }

func (m *memoryStore) cleanupExpired() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for id, s := range m.db {
			if now.After(s.ExpiresAt) {
				delete(m.db, id)
			}
		}
		m.mu.Unlock()
	}
}

func (m *memoryStore) encrypt(plain []byte) (string, error) {
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	encrypted := secretbox.Seal(nonce[:], plain, &nonce, &m.cryptoKey)
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

func (m *memoryStore) decrypt(enc string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	if len(raw) < 24 {
		return nil, errors.New("invalid encrypted payload")
	}
	var nonce [24]byte
	copy(nonce[:], raw[:24])
	decrypted, ok := secretbox.Open(nil, raw[24:], &nonce, &m.cryptoKey)
	if !ok {
		return nil, errors.New("failed to decrypt")
	}
	return decrypted, nil
}
