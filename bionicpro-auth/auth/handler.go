package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"

	"bionicpro-auth/config"
	"bionicpro-auth/models"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/securecookie"
	"golang.org/x/oauth2"
)

const maxAge = 3600 // 1 час

type AuthHandler struct {
	config         *config.Config
	Oauth2Config   *oauth2.Config
	sessionStorage *SessionStorage
	secureCookie   *securecookie.SecureCookie
}

func NewAuthHandler(cfg *config.Config, storage *SessionStorage, oauth2Config *oauth2.Config) *AuthHandler {
	secureCookie := securecookie.New(
		[]byte(cfg.SessionSecret),
		nil,
	).MaxAge(3600)

	return &AuthHandler{
		config:         cfg,
		Oauth2Config:   oauth2Config, // Правильный тип
		sessionStorage: storage,
		secureCookie:   secureCookie,
	}
}

func (h *AuthHandler) Login(c *gin.Context) {
	// Создаем стейт и PKCE код
	state := generateRandomString(32)
	codeVerifier := generateRandomString(32)

	sessionData := map[string]string{
		"state":         state,
		"code_verifier": codeVerifier,
	}

	if encoded, err := h.secureCookie.Encode("oauth_state", sessionData); err == nil {
		c.SetCookie("oauth_state", encoded, 300, "/", "", true, true)
	}

	// Редирект в Keycloak
	url := h.Oauth2Config.AuthCodeURL(state, oauth2.SetAuthURLParam("code_challenge", generateCodeChallenge(codeVerifier)))
	c.Redirect(http.StatusFound, url)
}

func (h *AuthHandler) Callback(c *gin.Context) {
	state := c.Query("state")
	code := c.Query("code")

	// Берем state из cookie
	if cookie, err := c.Cookie("oauth_state"); err == nil {
		var sessionData map[string]string
		if h.secureCookie.Decode("oauth_state", cookie, &sessionData) == nil {
			if sessionData["state"] != state {
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}

			token, err := h.Oauth2Config.Exchange(context.Background(), code,
				oauth2.SetAuthURLParam("code_verifier", sessionData["code_verifier"]))
			if err != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}

			sessionID := generateRandomString(32)
			session := &models.Session{
				ID:           sessionID,
				AccessToken:  token.AccessToken,
				RefreshToken: token.RefreshToken,
				ExpiresAt:    token.Expiry,
			}

			if err := h.sessionStorage.SaveSession(session); err != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}

			if encoded, err := h.secureCookie.Encode("session_id", sessionID); err == nil {
				c.SetCookie("session_id", encoded, 3600, "/", "", true, true)
			}

			c.Redirect(http.StatusFound, "/")
		}
	}
	c.AbortWithStatus(http.StatusBadRequest)
}

func (h *AuthHandler) RefreshToken(session *models.Session) (*models.Session, error) {
	token := &oauth2.Token{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		Expiry:       session.ExpiresAt,
	}

	newToken, err := h.Oauth2Config.TokenSource(context.Background(), token).Token()
	if err != nil {
		return nil, err
	}

	session.AccessToken = newToken.AccessToken
	session.RefreshToken = newToken.RefreshToken
	session.ExpiresAt = newToken.Expiry

	return session, h.sessionStorage.SaveSession(session)
}

func generateRandomString(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.URLEncoding.EncodeToString(hash[:])
}

func (h *AuthHandler) Logout(c *gin.Context) {
	if cookie, err := c.Cookie("session_id"); err == nil {
		var sessionID string
		if h.secureCookie.Decode("session_id", cookie, &sessionID) == nil {
			h.sessionStorage.DeleteSession(sessionID)
		}
	}

	// Удаляем куки
	c.SetCookie("session_id", "", -1, "/", "", true, true)
	c.SetCookie("oauth_state", "", -1, "/", "", true, true)

	c.JSON(200, gin.H{"message": "Logged out successfully"})
}
