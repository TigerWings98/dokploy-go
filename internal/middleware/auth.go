package middleware

import (
	"net/http"
	"strings"

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
// publicProcedures are tRPC procedure names that skip authentication (e.g. "settings.health").
func AuthMiddleware(a *auth.Auth, publicProcedures ...string) echo.MiddlewareFunc {
	publicSet := make(map[string]bool, len(publicProcedures))
	for _, p := range publicProcedures {
		publicSet[p] = true
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip auth for public tRPC procedures
			if len(publicSet) > 0 {
				procedures := c.Param("procedures")
				if procedures != "" && allPublic(procedures, publicSet) {
					return next(c)
				}
			}

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

// TRPCAuthMiddleware is like AuthMiddleware but returns tRPC-formatted errors
// so the frontend tRPC client can properly handle 401 and redirect to login.
func TRPCAuthMiddleware(a *auth.Auth, publicProcedures ...string) echo.MiddlewareFunc {
	publicSet := make(map[string]bool, len(publicProcedures))
	for _, p := range publicProcedures {
		publicSet[p] = true
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip auth for public tRPC procedures
			if len(publicSet) > 0 {
				procedures := c.Param("procedures")
				if procedures != "" && allPublic(procedures, publicSet) {
					return next(c)
				}
			}

			// Try API Key first
			apiKey := auth.GetAPIKeyFromRequest(c.Request())
			if apiKey != "" {
				user, err := a.ValidateAPIKey(apiKey)
				if err != nil {
					return trpcUnauthorized(c, "Invalid API key")
				}
				c.Set(UserContextKey, user)
				return next(c)
			}

			// Try session token
			token := auth.GetSessionTokenFromRequest(c.Request())
			if token == "" {
				return trpcUnauthorized(c, "Authentication required")
			}

			user, session, err := a.ValidateSession(token)
			if err != nil {
				return trpcUnauthorized(c, "Invalid session")
			}

			c.Set(UserContextKey, user)
			c.Set(SessionContextKey, session)
			return next(c)
		}
	}
}

func trpcUnauthorized(c echo.Context, message string) error {
	return c.JSON(http.StatusUnauthorized, map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"code":    -32001,
			"data": map[string]interface{}{
				"code":       "UNAUTHORIZED",
				"httpStatus": 401,
			},
		},
	})
}

// allPublic checks if all comma-separated procedure names are in the public set.
// This handles batch requests like "settings.health,sso.showSignInWithSSO".
func allPublic(procedures string, publicSet map[string]bool) bool {
	for _, p := range strings.Split(procedures, ",") {
		if !publicSet[strings.TrimSpace(p)] {
			return false
		}
	}
	return true
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
