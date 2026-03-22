package auth

import (
	"encoding/json"
	"errors"
	"time"

	"bionicpro-auth/models"

	"github.com/boltdb/bolt"
)

var ErrSessionNotFound = errors.New("session not found")

type SessionStorage struct {
	DB *bolt.DB
}

func NewSessionStorage() (*SessionStorage, error) {
	db, err := bolt.Open("sessions.db", 0600, nil)
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("sessions"))
		return err
	})

	return &SessionStorage{DB: db}, nil
}

func (s *SessionStorage) SaveSession(session *models.Session) error {
	return s.DB.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("sessions"))
		data, err := json.Marshal(session)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(session.ID), data)
	})
}

func (s *SessionStorage) GetSession(sessionID string) (*models.Session, error) {
	var session models.Session
	err := s.DB.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("sessions"))
		data := bucket.Get([]byte(sessionID))
		if data == nil {
			return ErrSessionNotFound
		}
		return json.Unmarshal(data, &session)
	})
	return &session, err
}

func (s *SessionStorage) DeleteSession(sessionID string) error {
	return s.DB.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("sessions"))
		return bucket.Delete([]byte(sessionID))
	})
}

func (s *SessionStorage) CleanupExpiredSessions() {
	ticker := time.NewTicker(time.Hour)
	for range ticker.C {
		s.DB.Update(func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("sessions"))
			cursor := bucket.Cursor()

			for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
				var session models.Session
				if json.Unmarshal(v, &session) == nil {
					if time.Now().After(session.ExpiresAt) {
						bucket.Delete(k)
					}
				}
			}
			return nil
		})
	}
}
