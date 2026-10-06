package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

// 「订单交易成功」Bark 推送。
//
// 配置存于 settings.payment_bark_notify_config_v1；若未单独配置 device key，
// 自动回退复用「凭证守护」里已配置的 bark_key，做到开箱即用。
const (
	paymentBarkNotifySettingsKey = "payment_bark_notify_config_v1"
	paymentBarkPushEndpoint      = "https://api.day.app/push"
	paymentBarkMaxBodyRunes      = 180
)

// PaymentBarkNotifyConfig 控制订单成交通知。
type PaymentBarkNotifyConfig struct {
	Enabled              bool   `json:"enabled"`
	BarkKey              string `json:"bark_key"`
	NotifyOnBalance      bool   `json:"notify_on_balance"`
	NotifyOnSubscription bool   `json:"notify_on_subscription"`
	Group                string `json:"group"`
	Sound                string `json:"sound"`
}

func defaultPaymentBarkNotifyConfig() PaymentBarkNotifyConfig {
	return PaymentBarkNotifyConfig{
		Enabled:              true,
		NotifyOnBalance:      true,
		NotifyOnSubscription: true,
		Group:                "payment",
		Sound:                "bell",
	}
}

// PaymentBarkNotifyService 负责读取配置并向 Bark 推送订单成交通知。
type PaymentBarkNotifyService struct {
	settings SettingRepository
	client   *http.Client

	mu     sync.RWMutex
	config PaymentBarkNotifyConfig
}

func NewPaymentBarkNotifyService(settings SettingRepository) *PaymentBarkNotifyService {
	svc := &PaymentBarkNotifyService{
		settings: settings,
		client:   &http.Client{Timeout: 10 * time.Second},
		config:   defaultPaymentBarkNotifyConfig(),
	}
	svc.loadConfig(context.Background())
	return svc
}

// ProvidePaymentBarkNotifyService 供 wire 使用。
func ProvidePaymentBarkNotifyService(settings SettingRepository) *PaymentBarkNotifyService {
	return NewPaymentBarkNotifyService(settings)
}

func (s *PaymentBarkNotifyService) loadConfig(ctx context.Context) {
	if s == nil || s.settings == nil {
		return
	}
	raw, err := s.settings.GetValue(ctx, paymentBarkNotifySettingsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return
	}
	cfg := defaultPaymentBarkNotifyConfig()
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.Warn("payment_bark_notify_config_decode_failed", "error", err)
		return
	}
	cfg.BarkKey = strings.TrimSpace(cfg.BarkKey)
	if strings.TrimSpace(cfg.Group) == "" {
		cfg.Group = "payment"
	}
	if strings.TrimSpace(cfg.Sound) == "" {
		cfg.Sound = "bell"
	}
	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
}

// GetConfig 返回当前配置副本（含兜底 key 是否可用的提示由前端自行展示）。
func (s *PaymentBarkNotifyService) GetConfig() PaymentBarkNotifyConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// SaveConfig 持久化配置。
func (s *PaymentBarkNotifyService) SaveConfig(ctx context.Context, cfg PaymentBarkNotifyConfig) error {
	if s == nil || s.settings == nil {
		return fmt.Errorf("payment bark notify service is unavailable")
	}
	cfg.BarkKey = strings.TrimSpace(cfg.BarkKey)
	cfg.Group = strings.TrimSpace(cfg.Group)
	if cfg.Group == "" {
		cfg.Group = "payment"
	}
	cfg.Sound = strings.TrimSpace(cfg.Sound)
	if cfg.Sound == "" {
		cfg.Sound = "bell"
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := s.settings.Set(ctx, paymentBarkNotifySettingsKey, string(raw)); err != nil {
		return err
	}
	s.mu.Lock()
	s.config = cfg
	s.mu.Unlock()
	return nil
}

// resolveBarkKey 优先使用订单通知自己的 key，未配置时回退到凭证守护的 bark_key。
func (s *PaymentBarkNotifyService) resolveBarkKey(ctx context.Context) string {
	cfg := s.GetConfig()
	if key := strings.TrimSpace(cfg.BarkKey); key != "" {
		return key
	}
	if s.settings == nil {
		return ""
	}
	raw, err := s.settings.GetValue(ctx, accountTokenGuardSettingsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return ""
	}
	var guard struct {
		BarkKey string `json:"bark_key"`
	}
	if err := json.Unmarshal([]byte(raw), &guard); err != nil {
		return ""
	}
	return strings.TrimSpace(guard.BarkKey)
}

// NotifyOrderFulfilled 在订单完成履约后推送 Bark 通知（异步、失败仅记日志）。
func (s *PaymentBarkNotifyService) NotifyOrderFulfilled(o *dbent.PaymentOrder, auditAction string) {
	if s == nil || o == nil {
		return
	}
	cfg := s.GetConfig()
	if !cfg.Enabled {
		return
	}
	switch auditAction {
	case "RECHARGE_SUCCESS":
		if !cfg.NotifyOnBalance {
			return
		}
	case "SUBSCRIPTION_SUCCESS":
		if !cfg.NotifyOnSubscription {
			return
		}
	default:
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		key := s.resolveBarkKey(ctx)
		if key == "" {
			return
		}
		title, body := buildPaymentBarkMessage(o, auditAction)
		payload := map[string]any{
			"device_key": key,
			"title":      title,
			"body":       body,
			"group":      cfg.Group,
			"level":      "timeSensitive",
			"sound":      cfg.Sound,
			"isArchive":  1,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, paymentBarkPushEndpoint, bytes.NewReader(data))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.client.Do(req)
		if err != nil {
			slog.Warn("payment_bark_notify_failed", "order_id", o.ID, "error", err.Error())
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			slog.Warn("payment_bark_notify_http_error", "order_id", o.ID, "status", resp.StatusCode)
		}
	}()
}

// SendTest 发送一条测试推送，便于后台验证配置。
func (s *PaymentBarkNotifyService) SendTest(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("payment bark notify service is unavailable")
	}
	key := s.resolveBarkKey(ctx)
	if key == "" {
		return fmt.Errorf("bark device key is not configured")
	}
	cfg := s.GetConfig()
	payload := map[string]any{
		"device_key": key,
		"title":      "💰 支付通知测试",
		"body":       "如果你看到这条消息，订单成交通知已经配置成功。",
		"group":      cfg.Group,
		"level":      "timeSensitive",
		"sound":      cfg.Sound,
		"isArchive":  1,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, paymentBarkPushEndpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("bark push failed with HTTP %d", resp.StatusCode)
	}
	return nil
}

func buildPaymentBarkMessage(o *dbent.PaymentOrder, auditAction string) (string, string) {
	kind := "余额充值"
	if auditAction == "SUBSCRIPTION_SUCCESS" {
		kind = "订阅购买"
	}
	title := fmt.Sprintf("💰 订单支付成功（%s）", kind)
	credited := o.Amount + o.BonusAmount
	pay := o.PayAmount
	provider := ""
	if o.ProviderKey != nil {
		provider = strings.TrimSpace(*o.ProviderKey)
	}
	lines := []string{
		fmt.Sprintf("到账 ¥%.2f｜实付 ¥%.2f", credited, pay),
		fmt.Sprintf("用户 %s", strings.TrimSpace(o.UserEmail)),
		fmt.Sprintf("订单 #%d · %s", o.ID, provider),
	}
	if o.BonusAmount > 0 {
		lines = append(lines, fmt.Sprintf("含赠送 ¥%.2f", o.BonusAmount))
	}
	if ts := o.CompletedAt; ts != nil {
		lines = append(lines, ts.Local().Format("2006-01-02 15:04:05"))
	} else if ts := o.PaidAt; ts != nil {
		lines = append(lines, ts.Local().Format("2006-01-02 15:04:05"))
	}
	if tradeNo := strings.TrimSpace(o.PaymentTradeNo); tradeNo != "" {
		lines = append(lines, "流水 "+tradeNo)
	}
	body := strings.Join(lines, "\n")
	runes := []rune(body)
	if len(runes) > paymentBarkMaxBodyRunes {
		body = string(runes[:paymentBarkMaxBodyRunes])
	}
	return title, body
}
