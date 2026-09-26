package service

import (
	"encoding/base64"
	"net/http"
	"os"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// codexNodeObserveEnabled 由 CODEX_NODE_OBSERVE=1 打开；只增加日志，不改变
// 任何请求/响应行为。用于把"满血 / 降智"定位到具体计算节点。
func codexNodeObserveEnabled() bool {
	return strings.TrimSpace(os.Getenv("CODEX_NODE_OBSERVE")) == "1"
}

// observeCodexUpstreamNode 记录上游计算节点（__oailb cookie 里的
// chat.gateway.unified-NNN）与 x-codex-turn-state 的形态。
func observeCodexUpstreamNode(c *gin.Context, requestID string, account *Account, upstream http.Header, source string) {
	if !codexNodeObserveEnabled() || upstream == nil {
		return
	}
	if strings.TrimSpace(requestID) == "" && c != nil {
		requestID = c.Writer.Header().Get("X-Request-Id")
	}
	node := ""
	for _, raw := range upstream.Values("Set-Cookie") {
		if !strings.HasPrefix(raw, "__oailb=") {
			continue
		}
		token := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0][len("__oailb="):])
		parts := strings.Split(token, ".")
		if len(parts) < 2 {
			continue
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			payload, err = base64.URLEncoding.DecodeString(parts[1])
		}
		if err != nil {
			continue
		}
		node = strings.TrimSpace(gjson.GetBytes(payload, "host").String())
		break
	}
	state := strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
	prefix := state
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	accountID := int64(0)
	if account != nil {
		accountID = account.ID
	}
	logger.L().Info("codex_node_observe",
		zap.String("source", source),
		zap.String("request_id", requestID),
		zap.Int64("account_id", accountID),
		zap.String("node", node),
		zap.Int("turn_state_len", len(state)),
		zap.String("turn_state_prefix", prefix),
		zap.String("upstream_request_id", upstream.Get("x-request-id")),
	)
}
