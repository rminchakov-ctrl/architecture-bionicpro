package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"bionicpro-auth/models"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/nacl/secretbox"
)

// RedisStore хранилище сессий в Redis
type RedisStore struct {
	rdb       *redis.Client
	ctx       context.Context
	cryptoKey [32]byte
	ttl       time.Duration
}

type stored struct {
	ID           string    `json:"id"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	UserID       string    `json:"user_id"`
}

// NewRedisStore создаёт клиент Redis и готовит шифр.
func NewRedisStore(addr, password string, db int, keyBase64 string, ttl time.Duration) (*RedisStore, error) {
	raw, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, err
	}
	if len(raw) != 32 {
		return nil, errors.New("refresh token encryption key must be 32 bytes")
	}
	var k [32]byte
	copy(k[:], raw)

	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// проверяем соединение
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}

	return &RedisStore{
		rdb:       rdb,
		ctx:       context.Background(),
		cryptoKey: k,
		ttl:       ttl,
	}, nil
}

// Save сохраняет сессию в Redis
func (s *RedisStore) Save(sess *models.Session) error {
	// Шифруем refresh_token перед записью
	encRefresh, err := s.encrypt(sess.RefreshToken)
	if err != nil {
		return err
	}

	tmp := stored{
		ID:           sess.ID,
		RefreshToken: encRefresh,
		ExpiresAt:    sess.ExpiresAt,
		UserID:       sess.UserID,
	}

	b, err := json.Marshal(&tmp)
	if err != nil {
		return err
	}
	return s.rdb.Set(s.ctx, sess.ID, b, s.ttl).Err()
}

// Get Получает сессию из Redis
func (s *RedisStore) Get(id string) (*models.Session, error) {
	b, err := s.rdb.Get(s.ctx, id).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, errors.New("session not found")
		}
		return nil, err
	}
	var st stored
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}

	// Дешифруем refresh_token
	if len(st.RefreshToken) > 0 {
		plain, err := s.decrypt(st.RefreshToken)
		if err != nil {
			return nil, err
		}
		st.RefreshToken = string(plain)
	}

	return &models.Session{
		ID:           st.ID,
		RefreshToken: []byte(st.RefreshToken),
		ExpiresAt:    st.ExpiresAt,
		UserID:       st.UserID,
	}, nil
}

// Delete удаляет сессию из Redis
func (s *RedisStore) Delete(id string) error {
	return s.rdb.Del(s.ctx, id).Err()
}

// Close закрывает соединение с Redis
func (s *RedisStore) Close() error {
	return s.rdb.Close()
}

func (s *RedisStore) encrypt(plain []byte) (string, error) {
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	encrypted := secretbox.Seal(nonce[:], plain, &nonce, &s.cryptoKey)
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

func (s *RedisStore) decrypt(enc string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	if len(raw) < 24 {
		return nil, errors.New("invalid encrypted payload")
	}
	var nonce [24]byte
	copy(nonce[:], raw[:24])
	decrypted, ok := secretbox.Open(nil, raw[24:], &nonce, &s.cryptoKey)
	if !ok {
		return nil, errors.New("failed to decrypt refresh token")
	}
	return decrypted, nil
}
