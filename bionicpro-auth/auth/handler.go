package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"bionicpro-auth/config"
	"bionicpro-auth/internal/session"
	"bionicpro-auth/models"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/securecookie"
	"golang.org/x/oauth2"
)

const (
	maxAgeSeconds = 3600
)

// AuthHandler ...
type AuthHandler struct {
	config         *config.Config
	Oauth2Config   *oauth2.Config
	sessionStorage session.Store
	tokenStorage   *session.InMemoryTokenStore
	secureCookie   *securecookie.SecureCookie
}

// NewAuthHandler ...
func NewAuthHandler(cfg *config.Config, storage session.Store, tokenStorage *session.InMemoryTokenStore, oauth2Config *oauth2.Config) *AuthHandler {
	secureCookie := securecookie.New(
		[]byte(cfg.SessionSecret),
		nil,
	).MaxAge(3600)

	return &AuthHandler{
		config:         cfg,
		Oauth2Config:   oauth2Config,
		sessionStorage: storage,
		tokenStorage:   tokenStorage,
		secureCookie:   secureCookie,
	}
}

// Login ...
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

	// Редирект в Keycloak с PKCE (S256)
	url := h.Oauth2Config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", generateCodeChallenge(codeVerifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	c.Redirect(http.StatusFound, url)
}

// Callback ...
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

			// Получаем user info для получения UserID
			userInfo, _ := h.GetUserInfo(token.AccessToken)

			sessionID := generateRandomString(32)
			userID := ""
			if userInfo != nil {
				userID = userInfo.Sub
			}

			// Сохраняем access_token в in-memory хранилище
			h.tokenStorage.Set(sessionID, token.AccessToken, token.Expiry)

			// В персистентном хранилище (Redis/Memory) сохраняем ТОЛЬКО refresh_token
			session := &models.Session{
				ID:           sessionID,
				RefreshToken: []byte(token.RefreshToken),
				ExpiresAt:    token.Expiry,
				UserID:       userID,
			}

			if err := h.sessionStorage.Save(session); err != nil {
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}

			if encoded, err := h.secureCookie.Encode("session_id", sessionID); err == nil {
				c.SetCookie("session_id", encoded, 3600, "/", "", true, true)
			}

			// Редирект на фронтенд
			c.Redirect(http.StatusFound, "http://localhost:3000")
		}
	}
	c.AbortWithStatus(http.StatusBadRequest)
}

// GetUserInfo ...
func (h *AuthHandler) GetUserInfo(accessToken string) (*models.UserInfo, error) {
	// Запрос к Keycloak userinfo endpoint
	req, err := http.NewRequest("GET", h.Oauth2Config.Endpoint.TokenURL+"/../userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var userInfo models.UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

// Logout ...
func (h *AuthHandler) Logout(c *gin.Context) {
	if cookie, err := c.Cookie("session_id"); err == nil {
		var sessionID string
		if h.secureCookie.Decode("session_id", cookie, &sessionID) == nil {
			// Удаляем из персистентного хранилища (Redis/Memory)
			h.sessionStorage.Delete(sessionID)
			// Удаляем access_token из in-memory хранилища
			h.tokenStorage.Delete(sessionID)
		}
	}
	// Удаляем куки
	c.SetCookie("session_id", "", -1, "/", "", true, true)
	c.SetCookie("oauth_state", "", -1, "/", "", true, true)

	c.JSON(200, gin.H{"message": "Logged out successfully"})
}

// CheckAuth проверяет статус аутентификации
func (h *AuthHandler) CheckAuth(c *gin.Context) {
	if cookie, err := c.Cookie("session_id"); err == nil {
		var sessionID string
		if h.secureCookie.Decode("session_id", cookie, &sessionID) == nil {
			session, err := h.sessionStorage.Get(sessionID)
			if err == nil && session != nil && !session.ExpiresAt.IsZero() {
				c.JSON(200, gin.H{"authenticated": true, "user_id": session.UserID})
				return
			}
		}
	}
	c.JSON(200, gin.H{"authenticated": false})
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
