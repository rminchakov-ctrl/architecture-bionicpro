package models

import (
	"time"
)

// Session - хранится в Redis/Memory (персистентно)
type Session struct {
	ID           string    `json:"id"`
	RefreshToken []byte    `json:"refresh_token"` // зашифрован
	ExpiresAt    time.Time `json:"expires_at"`
	UserID       string    `json:"user_id"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

type UserInfo struct {
	Sub               string `json:"sub"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
}
