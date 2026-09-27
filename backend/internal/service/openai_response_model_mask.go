package service

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// maskUpstreamModelName returns the (from, to) pair used to hide the
// upstream-reported model name from clients.
func maskUpstreamModelName(account *Account, originalModel, observedModel string) (string, string, bool) {
	if account == nil || !account.MasksUpstreamResponseModel() {
		return "", "", false
	}
	from := strings.TrimSpace(observedModel)
	to := strings.TrimSpace(originalModel)
	if from == "" || to == "" || from == to {
		return "", "", false
	}
	return from, to, true
}

// observedUpstreamModelName 读取当前观测到的上游响应模型名。没有观测上下文时
// 返回空串，调用方无需再做 nil 判断。
func observedUpstreamModelName(c *gin.Context) string {
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		return ""
	}
	return observer.Model()
}
