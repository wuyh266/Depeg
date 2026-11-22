package handler

import (
	"bytes"
	"context"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/domain/risk"
	"depeg.local/platform/internal/placeholder"
	"depeg.local/platform/internal/port"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Handlers interface {
	SendRegistrationCode(*gin.Context)
	Register(*gin.Context)
	RequestPasswordReset(*gin.Context)
	ResetPassword(*gin.Context)
	Login(*gin.Context)
	Refresh(*gin.Context)
	Logout(*gin.Context)
	Me(*gin.Context)
	Assets(*gin.Context)
	Trends(*gin.Context)
	Snapshots(*gin.Context)
	Risks(*gin.Context)
	Health(*gin.Context)
	Ready(*gin.Context)
	Overview(*gin.Context)
	SaveMonitorRule(*gin.Context)
	ReplayMonitor(*gin.Context)
	MonitorEvents(*gin.Context)
}
type handlers struct {
	auth     port.AuthService
	market   port.MarketService
	risks    port.RiskService
	monitors port.MonitorService
	database port.Database
	logger   port.Logger
}

func New(a port.AuthService, m port.MarketService, r port.RiskService, monitors port.MonitorService, db port.Database, l port.Logger) Handlers {
	return &handlers{auth: a, market: m, risks: r, monitors: monitors, database: db, logger: l}
}
func respond(c *gin.Context, status int, data any) {
	c.JSON(status, gin.H{"data": data, "request_id": c.GetString("request_id")})
}
func Failure(c *gin.Context, err error) {
	status, code, message := 500, "internal_error", "Unable to complete this request"
	switch {
	case errors.Is(err, auth.ErrEmailNotConfigured):
		status, code, message = 503, "email_not_configured", "Email service is not configured"
	case errors.Is(err, auth.ErrEmailDeliveryFailed):
		status, code, message = 503, "email_delivery_failed", "Email delivery failed; check SMTP configuration and delivery logs"
	case errors.Is(err, auth.ErrInvalidEmailCode):
		status, code, message = 400, "invalid_email_code", "Invalid or expired email code"
	case errors.Is(err, auth.ErrEmailCodeCooldown):
		status, code, message = 429, "email_code_cooldown", "Wait before requesting another code"
	case errors.Is(err, auth.ErrEmailSendLimit):
		status, code, message = 429, "email_send_limit", "Too many requests for email codes"
	case errors.Is(err, domain.ErrInvalid):
		status, code, message = 400, "invalid_request", "Invalid request parameters"
	case errors.Is(err, domain.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "Invalid or expired credentials"
	case errors.Is(err, domain.ErrConflict):
		status, code, message = 409, "conflict", "Resource already exists"
	case errors.Is(err, domain.ErrNotFound):
		status, code, message = 404, "not_found", "Resource not found"
	case errors.Is(err, domain.ErrUnavailable), errors.Is(err, placeholder.ErrNotImplemented), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		status, code, message = 503, "unavailable", "Feature or dependency is unavailable"
	}
	if status == 401 {
		c.Header("WWW-Authenticate", "Bearer")
	}
	if status == 429 {
		retryAfter := auth.EmailCodeCooldown
		if errors.Is(err, auth.ErrEmailSendLimit) {
			retryAfter = auth.EmailSendWindow
		}
		c.Header("Retry-After", strconv.Itoa(int(retryAfter/time.Second)))
	}
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}, "request_id": c.GetString("request_id")})
}
func decode(c *gin.Context, out any) bool {
	if c.ContentType() != "application/json" {
		Failure(c, domain.ErrInvalid)
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || !json.Valid(body) {
		Failure(c, domain.ErrInvalid)
		return false
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if err := c.ShouldBindJSON(out); err != nil {
		Failure(c, domain.ErrInvalid)
		return false
	}
	return true
}
func sendEmailCode(c *gin.Context, send func(context.Context, string, string) error) {
	c.Header("Cache-Control", "no-store")
	var input struct {
		Email string `json:"email" binding:"required"`
	}
	if !decode(c, &input) {
		return
	}
	if err := send(c.Request.Context(), input.Email, c.RemoteIP()); err != nil {
		Failure(c, err)
		return
	}
	c.Status(204)
}

func (h *handlers) SendRegistrationCode(c *gin.Context) {
	sendEmailCode(c, h.auth.SendRegistrationCode)
}

func (h *handlers) RequestPasswordReset(c *gin.Context) {
	sendEmailCode(c, h.auth.RequestPasswordReset)
}

func (h *handlers) ResetPassword(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input auth.PasswordReset
	if !decode(c, &input) {
		return
	}
	if err := h.auth.ResetPassword(c.Request.Context(), input); err != nil {
		Failure(c, err)
		return
	}
	c.Status(204)
}

func (h *handlers) Register(c *gin.Context) {
	var input auth.Registration
	if !decode(c, &input) {
		return
	}
	user, err := h.auth.Register(c.Request.Context(), input)
	if err != nil {
		Failure(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond(c, 201, user)
}
func (h *handlers) Login(c *gin.Context) {
	var input auth.Credentials
	if !decode(c, &input) {
		return
	}
	pair, err := h.auth.Login(c.Request.Context(), input)
	if err != nil {
		Failure(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond(c, 200, pair)
}
func (h *handlers) Refresh(c *gin.Context) {
	var input struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if !decode(c, &input) {
		return
	}
	pair, err := h.auth.Refresh(c.Request.Context(), input.RefreshToken)
	if err != nil {
		Failure(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond(c, 200, pair)
}
func (h *handlers) Logout(c *gin.Context) {
	parts := strings.Fields(c.GetHeader("Authorization"))
	if len(parts) != 2 {
		Failure(c, domain.ErrUnauthorized)
		return
	}
	if err := h.auth.Logout(c.Request.Context(), parts[1]); err != nil {
		Failure(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Status(204)
}
func (h *handlers) Me(c *gin.Context) {
	value, ok := c.Get("user")
	if !ok {
		Failure(c, domain.ErrUnauthorized)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond(c, 200, value)
}
func (h *handlers) Assets(c *gin.Context) {
	assets, err := h.market.ListAssets(c.Request.Context())
	if err != nil {
		Failure(c, err)
		return
	}
	respond(c, 200, assets)
}
func queryRange(c *gin.Context) (time.Time, time.Time, error) {
	from, to := c.Query("from"), c.Query("to")
	if from == "" && to == "" {
		end := time.Now().UTC().Truncate(time.Second)
		return end.Add(-24 * time.Hour), end, nil
	}
	if from == "" || to == "" {
		return time.Time{}, time.Time{}, domain.ErrInvalid
	}
	begin, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return time.Time{}, time.Time{}, domain.ErrInvalid
	}
	end, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return time.Time{}, time.Time{}, domain.ErrInvalid
	}
	return begin, end, nil
}
func (h *handlers) Trends(c *gin.Context) {
	from, to, err := queryRange(c)
	if err != nil {
		Failure(c, err)
		return
	}
	interval, err := time.ParseDuration(c.DefaultQuery("interval", "1m"))
	if err != nil {
		Failure(c, domain.ErrInvalid)
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "500"))
	if err != nil || limit < 1 || limit > 2000 {
		Failure(c, domain.ErrInvalid)
		return
	}
	result, err := h.market.Trend(c.Request.Context(), market.Query{AssetID: c.Query("asset_id"), Source: c.Query("source"), From: from, To: to, Interval: interval, Limit: limit, Cursor: c.Query("cursor")})
	if err != nil {
		Failure(c, err)
		return
	}
	respond(c, 200, result)
}
func (h *handlers) Risks(c *gin.Context) {
	_, err := h.risks.Series(c.Request.Context(), risk.Query{AssetID: c.Query("asset_id")})
	if err == nil {
		err = placeholder.ErrNotImplemented
	}
	Failure(c, err)
}

func (h *handlers) Snapshots(c *gin.Context) {
	from, to, err := queryRange(c)
	if err != nil {
		Failure(c, err)
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "500"))
	if err != nil || limit < 1 || limit > 2000 {
		Failure(c, domain.ErrInvalid)
		return
	}
	result, err := h.market.Snapshots(c.Request.Context(), market.Query{AssetID: c.Query("asset_id"), Source: c.Query("source"), From: from, To: to, Limit: limit, Cursor: c.Query("cursor")})
	if err != nil {
		Failure(c, err)
		return
	}
	respond(c, 200, result)
}
func (h *handlers) Health(c *gin.Context) { respond(c, 200, gin.H{"status": "ok"}) }
func (h *handlers) Ready(c *gin.Context) {
	if err := h.database.Ping(c.Request.Context()); err != nil {
		Failure(c, domain.ErrUnavailable)
		return
	}
	respond(c, 200, gin.H{"status": "ready"})
}
