package middleware

import (
	"depeg.local/platform/internal/adapter/http/handler"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/port"
	"github.com/gin-gonic/gin"
	"strings"
)

type JWTGuard interface{ Handler() gin.HandlerFunc }
type jwtGuard struct{ auth port.AuthService }

func NewJWT(auth port.AuthService) JWTGuard { return &jwtGuard{auth: auth} }
func (g *jwtGuard) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		value := c.GetHeader("Authorization")
		parts := strings.Fields(value)
		if len(value) > 8192 || len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			handler.Failure(c, domain.ErrUnauthorized)
			return
		}
		user, err := g.auth.Authenticate(c.Request.Context(), parts[1])
		if err != nil {
			handler.Failure(c, err)
			return
		}
		c.Set("user", user)
		c.Next()
	}
}
