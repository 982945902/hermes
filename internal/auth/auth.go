package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/982945902/hermes/internal/config"
	"github.com/982945902/hermes/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	config config.Config
}

type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func New(config config.Config) *Service {
	return &Service{config: config}
}

func (s *Service) Login(username string, password string) (string, error) {
	if username != s.config.AdminUsername {
		return "", errors.New("invalid username or password")
	}
	if s.config.AdminPasswordHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(s.config.AdminPasswordHash), []byte(password)); err != nil {
			return "", errors.New("invalid username or password")
		}
	} else if password != s.config.AdminPassword {
		return "", errors.New("invalid username or password")
	}
	now := time.Now()
	claims := Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.config.JWTSecret))
}

func (s *Service) Parse(tokenText string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenText, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.config.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func AdminMiddleware(service *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "admin token required"})
			return
		}
		claims, err := service.Parse(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid admin token"})
			return
		}
		c.Set("admin_username", claims.Username)
		c.Next()
	}
}

func GatewayMiddleware(apiKey string, tokenStore *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := gatewayToken(c)
		if apiKey != "" && token == apiKey {
			c.Set("gateway_legacy_key", true)
			c.Next()
			return
		}
		if tokenStore == nil {
			if apiKey == "" {
				c.Next()
				return
			}
			abortGatewayAuth(c, http.StatusUnauthorized, "invalid API key", "invalid_api_key")
			return
		}
		identity, err := tokenStore.ValidateGatewayToken(c.Request.Context(), token, c.ClientIP())
		if err != nil {
			abortGatewayTokenError(c, err)
			return
		}
		modelLimit := map[string]bool{}
		for _, modelName := range identity.Token.ModelLimits {
			modelLimit[modelName] = true
		}
		c.Set("api_user_id", identity.User.ID.Hex())
		c.Set("api_username", identity.User.Username)
		c.Set("api_user_group", identity.User.Group)
		c.Set("api_token_id", identity.Token.ID.Hex())
		c.Set("api_token_name", identity.Token.Name)
		c.Set("api_token_model_limits_enabled", identity.Token.ModelLimitsEnabled)
		c.Set("api_token_model_limits", modelLimit)
		c.Next()
	}
}

func gatewayToken(c *gin.Context) string {
	if token := bearerToken(c.GetHeader("Authorization")); token != "" {
		return token
	}
	if token := strings.TrimSpace(c.Query("key")); token != "" {
		return token
	}
	return strings.TrimSpace(c.GetHeader("X-API-Key"))
}

func abortGatewayTokenError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrGatewayTokenRequired):
		abortGatewayAuth(c, http.StatusUnauthorized, "API key required", "missing_api_key")
	case errors.Is(err, store.ErrGatewayTokenInvalid):
		abortGatewayAuth(c, http.StatusUnauthorized, "invalid API key", "invalid_api_key")
	case errors.Is(err, store.ErrGatewayTokenDisabled):
		abortGatewayAuth(c, http.StatusUnauthorized, "API key is disabled", "invalid_api_key")
	case errors.Is(err, store.ErrGatewayTokenExpired):
		abortGatewayAuth(c, http.StatusUnauthorized, "API key has expired", "invalid_api_key")
	case errors.Is(err, store.ErrGatewayUserDisabled):
		abortGatewayAuth(c, http.StatusForbidden, "user is disabled", "user_disabled")
	case errors.Is(err, store.ErrGatewayIPForbidden):
		abortGatewayAuth(c, http.StatusForbidden, "API key is not allowed from this IP", "access_denied")
	default:
		abortGatewayAuth(c, http.StatusInternalServerError, "failed to validate API key", "gateway_auth_error")
	}
}

func abortGatewayAuth(c *gin.Context, status int, message string, code string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
			"code":    code,
		},
	})
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return header
}
