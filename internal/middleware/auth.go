package middleware

import (
	"net/http"

	"github.com/dokploy/dokploy/internal/auth"
	"github.com/dokploy/dokploy/internal/db/schema"
	"github.com/labstack/echo/v4"
)

// Context keys for storing auth data.
const (
	UserContextKey    = "user"
	SessionContextKey = "session"
	MemberContextKey  = "member"
)

// AuthMiddleware creates an authentication middleware.
func AuthMiddleware(a *auth.Auth) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Try API Key first
			apiKey := auth.GetAPIKeyFromRequest(c.Request())
			if apiKey != "" {
				user, err := a.ValidateAPIKey(apiKey)
				if err != nil {
					return echo.NewHTTPError(http.StatusUnauthorized, "Invalid API key")
				}
				c.Set(UserContextKey, user)
				return next(c)
			}

			// Try session token
			token := auth.GetSessionTokenFromRequest(c.Request())
			if token == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
			}

			user, session, err := a.ValidateSession(token)
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid session")
			}

			c.Set(UserContextKey, user)
			c.Set(SessionContextKey, session)
			return next(c)
		}
	}
}

// AdminMiddleware ensures the user has admin role.
func AdminMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := GetUser(c)
			if user == nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
			}
			if user.Role != "admin" {
				return echo.NewHTTPError(http.StatusForbidden, "Admin access required")
			}
			return next(c)
		}
	}
}

// GetUser retrieves the authenticated user from the context.
func GetUser(c echo.Context) *schema.User {
	user, ok := c.Get(UserContextKey).(*schema.User)
	if !ok {
		return nil
	}
	return user
}

// GetSession retrieves the session from the context.
func GetSession(c echo.Context) *schema.Session {
	session, ok := c.Get(SessionContextKey).(*schema.Session)
	if !ok {
		return nil
	}
	return session
}

// GetMember retrieves the current organization member from the context.
func GetMember(c echo.Context) *schema.Member {
	member, ok := c.Get(MemberContextKey).(*schema.Member)
	if !ok {
		return nil
	}
	return member
}
