package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if cookie, err := c.Cookie("session_id"); err == nil {
			var sessionID string
			if h.secureCookie.Decode("session_id", cookie, &sessionID) == nil {
				if session, err := h.sessionStorage.GetSession(sessionID); err == nil {
					// Проверим - не надо ли обновить токен
					if time.Until(session.ExpiresAt) < 2*time.Minute {
						newSession, err := h.RefreshToken(session)
						if err != nil {
							c.AbortWithStatus(http.StatusUnauthorized)
							return
						}
						session = newSession

						// Ротируем sessionID
						h.sessionStorage.DeleteSession(sessionID)
						sessionID = generateRandomString(32)
						session.ID = sessionID
						h.sessionStorage.SaveSession(session)

						if encoded, err := h.secureCookie.Encode("session_id", sessionID); err == nil {
							c.SetCookie("session_id", encoded, 3600, "/", "", true, true)
						}
					}

					c.Set("session", session)
					c.Next()
					return
				}
			}
		}

		c.AbortWithStatus(http.StatusUnauthorized)
	}
}
