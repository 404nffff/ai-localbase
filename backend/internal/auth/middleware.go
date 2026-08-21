package auth

import (
	"net/http"
	"strings"

	"ai-localbase/internal/service"

	"github.com/gin-gonic/gin"
)

const SessionCookieName = "ai_localbase_session"
const CSRFCookieName = "ai_localbase_csrf"

// Middleware JWT 认证中间件
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing authorization header",
			})
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid authorization format",
			})
			return
		}

		claims, err := ValidateToken(tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": err.Error(),
			})
			return
		}

		c.Set("username", claims.Username)
		c.Next()
	}
}

func SessionMiddleware(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := sessionToken(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		principal, err := authService.ValidateSessionToken(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired session"})
			return
		}

		setPrincipal(c, principal)
		c.Set("auth_token", token)
		c.Next()
	}
}

func SessionOrAPIKeyMiddleware(authService *service.AuthService, requiredScopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		bearer, hasBearer := bearerToken(c)
		cookieToken, hasCookie := cookieSessionToken(c)
		if !hasBearer && !hasCookie {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		var (
			principal service.AuthPrincipal
			err       error
		)
		token := cookieToken
		if hasBearer && strings.HasPrefix(bearer, "ailb_sk_") {
			token = bearer
			principal, err = authService.ValidateAPIKey(bearer)
		} else if hasCookie {
			principal, err = authService.ValidateSessionToken(cookieToken)
		} else {
			token = bearer
			principal, err = authService.ValidateSessionToken(bearer)
			if err != nil {
				principal, err = authService.ValidateAPIKey(bearer)
			}
		}
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}
		if principal.AuthType == "api_key" && !hasRequiredScopes(principal.Scopes, requiredScopes) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "api key does not have required scope"})
			return
		}

		setPrincipal(c, principal)
		if principal.AuthType == "session" {
			c.Set("auth_token", token)
		}
		c.Next()
	}
}

func hasRequiredScopes(grantedScopes, requiredScopes []string) bool {
	if len(requiredScopes) == 0 {
		return true
	}
	granted := make(map[string]struct{}, len(grantedScopes))
	for _, scope := range grantedScopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope != "" {
			granted[scope] = struct{}{}
		}
	}
	_, hasMCPAdmin := granted["mcp:admin"]
	for _, scope := range requiredScopes {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if hasMCPAdmin && strings.HasPrefix(scope, "mcp:") {
			continue
		}
		if _, ok := granted[scope]; !ok {
			return false
		}
	}
	return true
}

func sessionToken(c *gin.Context) (string, bool) {
	if token, ok := cookieSessionToken(c); ok {
		return token, true
	}
	return bearerToken(c)
}

func cookieSessionToken(c *gin.Context) (string, bool) {
	token, err := c.Cookie(SessionCookieName)
	if err != nil {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func bearerToken(c *gin.Context) (string, bool) {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader == "" {
		return "", false
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	return token, token != ""
}

func setPrincipal(c *gin.Context, principal service.AuthPrincipal) {
	c.Set("auth_type", principal.AuthType)
	c.Set("user_id", principal.UserID)
	c.Set("username", principal.Username)
	c.Set("role", principal.Role)
	c.Set("session_id", principal.SessionID)
	c.Set("api_key_id", principal.APIKeyID)
	c.Set("scopes", principal.Scopes)
	if !principal.ExpiresAt.IsZero() {
		c.Set("expires_at", principal.ExpiresAt.Unix())
	}
}

// PrincipalFromContext returns the authenticated principal installed by one of
// the session or API key middleware handlers.
func PrincipalFromContext(c *gin.Context) service.AuthPrincipal {
	if c == nil {
		return service.AuthPrincipal{}
	}
	principal := service.AuthPrincipal{}
	if value, ok := c.Get("auth_type"); ok {
		principal.AuthType, _ = value.(string)
	}
	if value, ok := c.Get("user_id"); ok {
		principal.UserID, _ = value.(string)
	}
	if value, ok := c.Get("username"); ok {
		principal.Username, _ = value.(string)
	}
	if value, ok := c.Get("role"); ok {
		principal.Role, _ = value.(string)
	}
	if value, ok := c.Get("session_id"); ok {
		principal.SessionID, _ = value.(string)
	}
	if value, ok := c.Get("api_key_id"); ok {
		principal.APIKeyID, _ = value.(string)
	}
	if value, ok := c.Get("scopes"); ok {
		principal.Scopes, _ = value.([]string)
	}
	return principal
}
