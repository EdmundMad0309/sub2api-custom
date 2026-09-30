package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AccountOpsExtraHandler 承载「智能运维」下的扩展子功能：
// 首字监控（按分组自动开关账号调度）与账号账单对账。
type AccountOpsExtraHandler struct {
	firstToken *service.FirstTokenMonitorService
	billing    *service.BillingReconcileService
}

func NewAccountOpsExtraHandler(firstToken *service.FirstTokenMonitorService, billing *service.BillingReconcileService) *AccountOpsExtraHandler {
	return &AccountOpsExtraHandler{firstToken: firstToken, billing: billing}
}

// GetFirstTokenMonitor GET /account-ops/first-token
func (h *AccountOpsExtraHandler) GetFirstTokenMonitor(c *gin.Context) {
	if h == nil || h.firstToken == nil {
		response.Error(c, http.StatusServiceUnavailable, "first token monitor unavailable")
		return
	}
	status, err := h.firstToken.Status(c.Request.Context(), true)
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "first token monitor unavailable")
		return
	}
	response.Success(c, status)
}

// SaveFirstTokenMonitorConfig PUT /account-ops/first-token/config
func (h *AccountOpsExtraHandler) SaveFirstTokenMonitorConfig(c *gin.Context) {
	if h == nil || h.firstToken == nil {
		response.Error(c, http.StatusServiceUnavailable, "first token monitor unavailable")
		return
	}
	var cfg service.FirstTokenMonitorConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		response.BadRequest(c, "invalid first token monitor config")
		return
	}
	if err := h.firstToken.SaveConfig(c.Request.Context(), cfg); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, h.firstToken.GetConfig())
}

// RunFirstTokenMonitor POST /account-ops/first-token/run
func (h *AccountOpsExtraHandler) RunFirstTokenMonitor(c *gin.Context) {
	if h == nil || h.firstToken == nil {
		response.Error(c, http.StatusServiceUnavailable, "first token monitor unavailable")
		return
	}
	events, samples, err := h.firstToken.RunOnce(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	response.Success(c, gin.H{"events": events, "samples": samples})
}

// GetBillingReconcile GET /account-ops/billing-reconcile?day=YYYY-MM-DD
func (h *AccountOpsExtraHandler) GetBillingReconcile(c *gin.Context) {
	if h == nil || h.billing == nil {
		response.Error(c, http.StatusServiceUnavailable, "billing reconcile unavailable")
		return
	}
	target, ok := parseBillingReconcileDay(c.Query("day"))
	if !ok {
		response.BadRequest(c, "invalid day, expected YYYY-MM-DD")
		return
	}
	status, err := h.billing.Status(c.Request.Context(), target)
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "billing reconcile unavailable")
		return
	}
	response.Success(c, status)
}

// RunBillingReconcile POST /account-ops/billing-reconcile/run
func (h *AccountOpsExtraHandler) RunBillingReconcile(c *gin.Context) {
	if h == nil || h.billing == nil {
		response.Error(c, http.StatusServiceUnavailable, "billing reconcile unavailable")
		return
	}
	target, ok := parseBillingReconcileDay(c.Query("day"))
	if !ok {
		response.BadRequest(c, "invalid day, expected YYYY-MM-DD")
		return
	}
	rows, err := h.billing.RunOnce(c.Request.Context(), target)
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, err.Error())
		return
	}
	response.Success(c, gin.H{"day": service.ResolveBillingReconcileDay(target).Format("2006-01-02"), "rows": rows})
}

func parseBillingReconcileDay(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, true
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
