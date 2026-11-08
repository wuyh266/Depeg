package middleware_test

import (
	"context"
	"depeg.local/platform/internal/adapter/http/middleware"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/port"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

type unavailableAuth struct{ port.AuthService }

func (unavailableAuth) Authenticate(context.Context, string) (auth.User, error) {
	return auth.User{}, domain.ErrUnavailable
}
func TestDependencyFailureNeverAuthorizes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, header := range []string{"", "Basic credentials", "Bearer arbitrary"} {
		router := gin.New()
		called := false
		router.Use(middleware.NewJWT(unavailableAuth{}).Handler())
		router.GET("/protected", func(c *gin.Context) { called = true; c.Status(200) })
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", header)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		expected := 401
		if header == "Bearer arbitrary" {
			expected = 503
		}
		if called || response.Code != expected {
			t.Fatalf("header %q reached handler=%v status=%d", header, called, response.Code)
		}
	}
}
