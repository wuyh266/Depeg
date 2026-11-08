package httpserver

import (
	"context"
	"depeg.local/platform/internal/adapter/http/handler"
	"depeg.local/platform/internal/adapter/http/middleware"
	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/identity"
	"depeg.local/platform/internal/port"
	"depeg.local/platform/web"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	"time"
)

type server struct {
	settings config.HTTP
	handlers handler.Handlers
	auth     middleware.JWTGuard
	logger   port.Logger
	engine   *gin.Engine
}

func New(settings config.HTTP, handlers handler.Handlers, auth middleware.JWTGuard, logger port.Logger) port.Server {
	s := &server{settings: settings, handlers: handlers, auth: auth, logger: logger, engine: gin.New()}
	s.engine.Use(s.boundary())
	s.registerRoutes()
	s.engine.NoRoute(func(c *gin.Context) { handler.Failure(c, domain.ErrNotFound) })
	return s
}
func (s *server) boundary() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := identity.New()
		if err != nil {
			handler.Failure(c, domain.ErrUnavailable)
			return
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		defer func() {
			if recover() != nil {
				s.logger.Error(c.Request.Context(), "request panic", "request_id", id)
				handler.Failure(c, fmt.Errorf("internal error"))
			}
		}()
		if origin := c.GetHeader("Origin"); origin != "" {
			scheme := "http"
			if c.Request.TLS != nil {
				scheme = "https"
			}
			allowed := origin == scheme+"://"+c.Request.Host
			for _, candidate := range s.settings.AllowedOrigins {
				if origin == candidate {
					allowed = true
					break
				}
			}
			c.Header("Vary", "Origin")
			if !allowed {
				c.AbortWithStatusJSON(403, gin.H{"error": gin.H{"code": "origin_not_allowed", "message": "Origin is not allowed"}, "request_id": id})
				return
			}
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
			c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Header("Access-Control-Expose-Headers", "X-Request-ID")
			if c.Request.Method == http.MethodOptions {
				c.Status(204)
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
func (s *server) registerRoutes() {
	s.engine.GET("/", gin.WrapH(web.Handler()))
	s.engine.GET("/static/:name", gin.WrapH(web.Handler()))
	s.engine.GET("/healthz", s.handlers.Health)
	s.engine.GET("/readyz", s.handlers.Ready)
	v1 := s.engine.Group("/api/v1")
	v1.POST("/auth/register", s.handlers.Register)
	v1.POST("/auth/login", s.handlers.Login)
	v1.POST("/auth/refresh", s.handlers.Refresh)
	protected := v1.Group("")
	protected.Use(s.auth.Handler())
	protected.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Next() })
	protected.GET("/auth/me", s.handlers.Me)
	protected.POST("/auth/logout", s.handlers.Logout)
	protected.GET("/assets", s.handlers.Assets)
	protected.GET("/trends", s.handlers.Trends)
	protected.GET("/market-snapshots", s.handlers.Snapshots)
	protected.GET("/risks", s.handlers.Risks)
	protected.GET("/dashboard", s.handlers.Overview)
	protected.POST("/monitor/rules", s.handlers.SaveMonitorRule)
	protected.POST("/monitor/replay", s.handlers.ReplayMonitor)
	protected.GET("/monitor/events", s.handlers.MonitorEvents)
}
func (s *server) Handler() http.Handler { return s.engine }
func (s *server) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", s.settings.Address)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	httpServer := &http.Server{Handler: s.engine, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: s.settings.ReadTimeout, WriteTimeout: s.settings.WriteTimeout, IdleTimeout: s.settings.IdleTimeout, MaxHeaderBytes: 32 * 1024}
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	s.logger.Info(ctx, "HTTP server listening", "address", listener.Addr().String())
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), s.settings.ShutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdown); err != nil {
			_ = httpServer.Close()
			return err
		}
		err := <-done
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
