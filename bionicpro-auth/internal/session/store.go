package session

import (
	"bionicpro-auth/models"
)

// Store интерфейс для хранилища сессий
type Store interface {
	Save(sess *models.Session) error
	Get(id string) (*models.Session, error)
	Delete(id string) error
	Close() error
}
