package auth

import (
	"context"
	"net/http"
	"time"

	"bionicpro-auth/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

const (
	preUpdateSeconds  = 60
	sessionTTLSeconds = 3600
)

func (h *AuthHandler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if cookie, err := c.Cookie("session_id"); err == nil {
			var sessionID string
			if h.secureCookie.Decode("session_id", cookie, &sessionID) == nil {
				// Получаем сессию из персистентного хранилища (Redis/Memory)
				sess, err := h.sessionStorage.Get(sessionID)
				if err == nil && sess != nil && !sess.ExpiresAt.IsZero() {
					// Проверим - не надо ли обновить токен
					if time.Until(sess.ExpiresAt) < preUpdateSeconds*time.Second {
						newSession, err := h.RefreshToken(sessionID, sess)
						if err != nil {
							c.AbortWithStatus(http.StatusUnauthorized)
							return
						}
						sess = newSession
					}

					// Ротируем session_id при каждом запросе (защита от fixation)
					h.sessionStorage.Delete(sessionID)
					sessionID = generateRandomString(32)
					sess.ID = sessionID
					h.sessionStorage.Save(sess)

					if encoded, err := h.secureCookie.Encode("session_id", sessionID); err == nil {
						c.SetCookie("session_id", encoded, sessionTTLSeconds, "/", "", true, true)
					}

					// Получаем access_token из in-memory хранилища
					accessToken, _, err := h.tokenStorage.Get(sessionID)
					if err != nil {
						// Токен истек или отсутствует - релогин
						c.AbortWithStatus(http.StatusUnauthorized)
						return
					}
					// AccessToken в in-memory store
					// Но сохраняем в контексте для использования в API вызовах
					c.Set("access_token", accessToken)

					c.Set("session", sess)
					c.Set("user_id", sess.UserID)
					c.Next()
					return
				}
			}
		}

		c.AbortWithStatus(http.StatusUnauthorized)
	}
}

// RefreshToken обновляет access token через refresh token
func (h *AuthHandler) RefreshToken(sessionID string, sess *models.Session) (*models.Session, error) {
	token := &oauth2.Token{
		RefreshToken: string(sess.RefreshToken),
		Expiry:       sess.ExpiresAt,
	}

	newToken, err := h.Oauth2Config.TokenSource(context.Background(), token).Token()
	if err != nil {
		return nil, err
	}

	// Сохраняем access_token в in-memory хранилище
	h.tokenStorage.Refresh(sessionID, newToken.AccessToken, newToken.Expiry)

	// Обновляем refresh_token в персистентном хранилище
	sess.RefreshToken = []byte(newToken.RefreshToken)
	sess.ExpiresAt = newToken.Expiry

	return sess, h.sessionStorage.Save(sess)
}
