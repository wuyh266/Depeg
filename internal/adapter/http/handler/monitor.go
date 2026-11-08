package handler

import (
	"context"
	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/auth"
	"depeg.local/platform/internal/domain/monitor"
	"github.com/gin-gonic/gin"
	"strconv"
	"time"
)

func accountID(c *gin.Context) string {
	value, exists := c.Get("user")
	if !exists {
		return ""
	}
	user, ok := value.(auth.User)
	if !ok {
		return ""
	}
	return user.ID
}

func (h *handlers) Overview(c *gin.Context) {
	result, err := h.monitors.Overview(c.Request.Context(), accountID(c))
	if err != nil {
		Failure(c, err)
		return
	}
	respond(c, 200, result)
}

func (h *handlers) SaveMonitorRule(c *gin.Context) {
	var rule monitor.Rule
	if !decode(c, &rule) {
		return
	}
	result, err := h.monitors.SaveRule(c.Request.Context(), accountID(c), rule)
	if err != nil {
		Failure(c, err)
		return
	}
	respond(c, 200, result)
}

func (h *handlers) ReplayMonitor(c *gin.Context) {
	var query monitor.ReplayQuery
	if !decode(c, &query) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	result, err := h.monitors.Replay(ctx, accountID(c), query)
	if err != nil {
		Failure(c, err)
		return
	}
	respond(c, 200, result)
}

func (h *handlers) MonitorEvents(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil {
		Failure(c, domain.ErrInvalid)
		return
	}
	result, err := h.monitors.Events(c.Request.Context(), monitor.EventQuery{UserID: accountID(c), AssetID: c.Query("asset_id"), Cursor: c.Query("cursor"), Limit: limit})
	if err != nil {
		Failure(c, err)
		return
	}
	respond(c, 200, result)
}
