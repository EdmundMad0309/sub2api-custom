package admin

import (
	"errors"
	"net/http"
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AccountOpsUpstreamBalanceHandler 承载「智能运维 · 上游余额」：
// 为每个账号保存上游面板登录凭据（NewAPI / Sub2API）并读取对应余额。
type AccountOpsUpstreamBalanceHandler struct {
	service *service.UpstreamPanelBalanceService
}

func NewAccountOpsUpstreamBalanceHandler(balanceService *service.UpstreamPanelBalanceService) *AccountOpsUpstreamBalanceHandler {
	return &AccountOpsUpstreamBalanceHandler{service: balanceService}
}

// List GET /admin/account-ops/upstream-balance
func (h *AccountOpsUpstreamBalanceHandler) List(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "upstream panel balance service unavailable")
		return
	}
	rows, err := h.service.List(c.Request.Context())
	if err != nil {
		writeUpstreamBalanceError(c, err)
		return
	}
	response.Success(c, gin.H{"rows": rows})
}

// Save PUT /admin/account-ops/upstream-balance/accounts/:id
func (h *AccountOpsUpstreamBalanceHandler) Save(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "upstream panel balance service unavailable")
		return
	}
	accountID, ok := parseUpstreamBalanceAccountID(c)
	if !ok {
		return
	}
	var input service.UpstreamPanelCredentialInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "invalid request: "+err.Error())
		return
	}
	input.AccountID = accountID
	row, err := h.service.Save(c.Request.Context(), input)
	if err != nil {
		writeUpstreamBalanceError(c, err)
		return
	}
	response.Success(c, row)
}

// Delete DELETE /admin/account-ops/upstream-balance/accounts/:id
func (h *AccountOpsUpstreamBalanceHandler) Delete(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "upstream panel balance service unavailable")
		return
	}
	accountID, ok := parseUpstreamBalanceAccountID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), accountID); err != nil {
		writeUpstreamBalanceError(c, err)
		return
	}
	response.Success(c, gin.H{"account_id": accountID})
}

// Probe POST /admin/account-ops/upstream-balance/accounts/:id/probe
func (h *AccountOpsUpstreamBalanceHandler) Probe(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "upstream panel balance service unavailable")
		return
	}
	accountID, ok := parseUpstreamBalanceAccountID(c)
	if !ok {
		return
	}
	row, err := h.service.Probe(c.Request.Context(), accountID)
	if err != nil {
		writeUpstreamBalanceError(c, err)
		return
	}
	response.Success(c, row)
}

// ProbeAll POST /admin/account-ops/upstream-balance/probe
func (h *AccountOpsUpstreamBalanceHandler) ProbeAll(c *gin.Context) {
	if h == nil || h.service == nil {
		response.Error(c, http.StatusServiceUnavailable, "upstream panel balance service unavailable")
		return
	}
	summary, err := h.service.ProbeAll(c.Request.Context())
	if err != nil {
		writeUpstreamBalanceError(c, err)
		return
	}
	response.Success(c, summary)
}

func parseUpstreamBalanceAccountID(c *gin.Context) (int64, bool) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "invalid account id")
		return 0, false
	}
	return accountID, true
}

func writeUpstreamBalanceError(c *gin.Context, err error) {
	var appErr *infraerrors.ApplicationError
	if errors.As(err, &appErr) {
		response.ErrorFrom(c, err)
		return
	}
	response.BadRequest(c, err.Error())
}
