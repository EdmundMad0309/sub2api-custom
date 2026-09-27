package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

// excelBPSFallbackError 表示 BPS 在写出任何响应字节之前被上游拒绝或不可用。
// 它携带原始状态码与错误码，供调用方在同一请求内改走标准 Codex 端点；
// 「已经写出响应」的失败不走这里，由 forwardExcelBPS 内部带内收尾。
type excelBPSFallbackError struct {
	StatusCode int
	Code       string
	Message    string
	Detail     string
}

func (e *excelBPSFallbackError) Error() string {
	if e == nil {
		return "excel BPS fallback"
	}
	code := strings.TrimSpace(e.Code)
	if code == "" {
		code = "basispoints_error"
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		return "excel BPS fallback: " + code
	}
	return "excel BPS fallback: " + code + ": " + msg
}

func newExcelBPSFallbackError(status int, code, message, detail string) error {
	return &excelBPSFallbackError{
		StatusCode: status,
		Code:       strings.TrimSpace(code),
		Message:    message,
		Detail:     detail,
	}
}

// excelBPSFallbackCause 把包装过的错误还原成回退错误；不是回退错误时返回 nil。
func excelBPSFallbackCause(err error) *excelBPSFallbackError {
	var target *excelBPSFallbackError
	if errors.As(err, &target) {
		return target
	}
	return nil
}

// resetExcelBPSOpsUpstreamError 让回退后的标准 Codex 尝试重新记录自己的上游状态。
// BPS 那次尝试仍保留在 upstream_errors 数组里，只清空「最后一个上游错误」三项，
// 避免成功回退的请求被标成 BPS 403。
func resetExcelBPSOpsUpstreamError(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(OpsUpstreamStatusCodeKey, 0)
	c.Set(OpsUpstreamErrorMessageKey, "")
	c.Set(OpsUpstreamErrorDetailKey, "")
}

// excelBPSFallbackLogAt 按 (账号,模型) 给回退日志限流，避免每请求一行刷爆日志。
var excelBPSFallbackLogAt sync.Map

const excelBPSFallbackLogInterval = 60 * time.Second

func logExcelBPSFallback(account *Account, model string, fallback *excelBPSFallbackError, elapsed time.Duration) {
	if account == nil || fallback == nil {
		return
	}
	key := fmt.Sprintf("%d|%s", account.ID, model)
	now := time.Now()
	if raw, ok := excelBPSFallbackLogAt.Load(key); ok {
		if last, ok := raw.(time.Time); ok && now.Sub(last) < excelBPSFallbackLogInterval {
			return
		}
	}
	excelBPSFallbackLogAt.Store(key, now)
	logger.LegacyPrintf("service.openai_gateway",
		"excel_bps fallback account=%d model=%s upstream_status=%d code=%s elapsed_ms=%d",
		account.ID, model, fallback.StatusCode, fallback.Code, elapsed.Milliseconds())
}
