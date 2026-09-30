package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/sync/errgroup"
)

// 上游面板余额：用「上游站点账号 + 密码」登录上游面板，读取该账号对应的余额。
// 支持两类面板：
//   - newapi：NewAPI / one-api 系列，POST /api/user/login 建立会话后读取
//     GET /api/user/self 的 quota（再用 /api/status 的 quota_per_unit 折算成金额）；
//   - sub2api：Sub2API 面板，POST /api/v1/auth/login 取 access_token 后读取
//     GET /api/v1/user/profile 的 balance。
const (
	UpstreamPanelSiteNewAPI  = "newapi"
	UpstreamPanelSiteSub2API = "sub2api"

	upstreamPanelProbeTimeout          = 20 * time.Second
	upstreamPanelMaxBodyBytes          = 128 * 1024
	upstreamPanelMaxMessageRunes       = 200
	upstreamPanelMaxUsernameLength     = 256
	upstreamPanelMaxPasswordLength     = 1024
	upstreamPanelMaxBaseURLLength      = 512
	upstreamPanelProbeConcurrency      = 4
	upstreamPanelMaxBatchSize          = 50
	upstreamPanelNewAPIDefaultQuotaPer = 500000.0
)

// UpstreamPanelCredential 是一个账号对应的上游面板登录配置与最近一次余额探测结果。
// PasswordCiphertext 只在服务端与仓储之间流转，禁止下发给前端。
type UpstreamPanelCredential struct {
	AccountID          int64          `json:"account_id"`
	AccountName        string         `json:"account_name"`
	AccountStatus      string         `json:"account_status"`
	SiteType           string         `json:"site_type"`
	BaseURL            string         `json:"base_url"`
	Username           string         `json:"username"`
	PasswordConfigured bool           `json:"password_configured"`
	Enabled            bool           `json:"enabled"`
	LastProbeAt        *time.Time     `json:"last_probe_at,omitempty"`
	LastStatus         string         `json:"last_status,omitempty"`
	LastError          string         `json:"last_error,omitempty"`
	LastHTTPStatus     int            `json:"last_http_status,omitempty"`
	Balance            *float64       `json:"balance,omitempty"`
	BalanceUnit        string         `json:"balance_unit,omitempty"`
	BalanceDetail      map[string]any `json:"balance_detail,omitempty"`
	UpdatedAt          *time.Time     `json:"updated_at,omitempty"`

	PasswordCiphertext string `json:"-"`
}

// UpstreamPanelCredentialInput 是保存上游面板登录配置的入参。
type UpstreamPanelCredentialInput struct {
	AccountID int64  `json:"account_id"`
	SiteType  string `json:"site_type"`
	BaseURL   string `json:"base_url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Enabled   bool   `json:"enabled"`
}

// UpstreamPanelProbeOutcome 是一次余额探测的结果，写库时使用。
type UpstreamPanelProbeOutcome struct {
	Status     string
	Error      string
	HTTPStatus int
	Balance    *float64
	Unit       string
	Detail     map[string]any
	At         time.Time
}

// UpstreamPanelCredentialRepository 上游面板凭据与余额快照的仓储能力。
type UpstreamPanelCredentialRepository interface {
	ListUpstreamPanelCredentials(ctx context.Context) ([]UpstreamPanelCredential, error)
	GetUpstreamPanelCredential(ctx context.Context, accountID int64) (*UpstreamPanelCredential, error)
	UpsertUpstreamPanelCredential(ctx context.Context, input UpstreamPanelCredentialInput, passwordCiphertext string) error
	DeleteUpstreamPanelCredential(ctx context.Context, accountID int64) error
	SaveUpstreamPanelProbeOutcome(ctx context.Context, accountID int64, outcome UpstreamPanelProbeOutcome) error
}

// upstreamPanelHTTPDoer 抽象 HTTP 客户端，便于单测注入 httptest。
type upstreamPanelHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type upstreamPanelBalance struct {
	Balance *float64
	Unit    string
	Detail  map[string]any
}

type upstreamPanelEnvelope struct {
	Success *bool          `json:"success"`
	Code    *int           `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data"`
}

// NormalizeUpstreamPanelSiteType 规范化面板类型，仅接受 newapi / sub2api。
func NormalizeUpstreamPanelSiteType(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case UpstreamPanelSiteNewAPI, "new-api", "new_api", "oneapi", "one-api":
		return UpstreamPanelSiteNewAPI, nil
	case UpstreamPanelSiteSub2API, "sub-api", "sub_api", "sub2":
		return UpstreamPanelSiteSub2API, nil
	default:
		return "", fmt.Errorf("unsupported site_type %q: expected %s or %s", raw, UpstreamPanelSiteNewAPI, UpstreamPanelSiteSub2API)
	}
}

// NormalizeUpstreamPanelBaseURL 校验并规范化上游站点地址：
// 只允许 http/https、必须带主机名、不接受用户信息/查询串，并去掉
// 末尾的 "/"、"/v1"、"api/v1"（这些是接口前缀，由探测逻辑自己拼接）。
func NormalizeUpstreamPanelBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("base_url is required")
	}
	if len(trimmed) > upstreamPanelMaxBaseURLLength {
		return "", fmt.Errorf("base_url is too long (max %d characters)", upstreamPanelMaxBaseURLLength)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid base_url: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("base_url must start with http:// or https://")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("base_url must include a host")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("base_url must not include credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("base_url must not include a query string or fragment")
	}
	path := strings.TrimRight(parsed.Path, "/")
	lowerPath := strings.ToLower(path)
	for _, suffix := range []string{"/api/v1", "/v1"} {
		if strings.HasSuffix(lowerPath, suffix) {
			path = path[:len(path)-len(suffix)]
			lowerPath = strings.ToLower(path)
		}
	}
	path = strings.TrimRight(path, "/")
	return scheme + "://" + parsed.Host + path, nil
}

// UpstreamPanelBalanceService 负责保存上游面板凭据并探测余额。
type UpstreamPanelBalanceService struct {
	repo      UpstreamPanelCredentialRepository
	accounts  AccountRepository
	encryptor SecretEncryptor
	doer      upstreamPanelHTTPDoer
	now       func() time.Time
}

func NewUpstreamPanelBalanceService(
	repo UpstreamPanelCredentialRepository,
	accounts AccountRepository,
	encryptor SecretEncryptor,
) *UpstreamPanelBalanceService {
	return &UpstreamPanelBalanceService{
		repo:      repo,
		accounts:  accounts,
		encryptor: encryptor,
		doer:      &http.Client{Timeout: upstreamPanelProbeTimeout},
		now:       time.Now,
	}
}

// ProvideUpstreamPanelBalanceService 供 wire 使用的构造函数。
func ProvideUpstreamPanelBalanceService(
	repo UpstreamPanelCredentialRepository,
	accounts AccountRepository,
	encryptor SecretEncryptor,
) *UpstreamPanelBalanceService {
	return NewUpstreamPanelBalanceService(repo, accounts, encryptor)
}

func newUpstreamPanelBalanceServiceWithDoer(
	repo UpstreamPanelCredentialRepository,
	accounts AccountRepository,
	encryptor SecretEncryptor,
	doer upstreamPanelHTTPDoer,
) *UpstreamPanelBalanceService {
	svc := NewUpstreamPanelBalanceService(repo, accounts, encryptor)
	if doer != nil {
		svc.doer = doer
	}
	return svc
}

// List 返回所有已配置的上游面板凭据（不含密码）。
func (s *UpstreamPanelBalanceService) List(ctx context.Context) ([]UpstreamPanelCredential, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("upstream panel balance service is unavailable")
	}
	return s.repo.ListUpstreamPanelCredentials(ctx)
}

// Save 校验并保存某个账号的上游面板凭据；password 为空表示沿用已保存的密码。
func (s *UpstreamPanelBalanceService) Save(ctx context.Context, input UpstreamPanelCredentialInput) (*UpstreamPanelCredential, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("upstream panel balance service is unavailable")
	}
	if input.AccountID <= 0 {
		return nil, fmt.Errorf("account_id is required")
	}
	siteType, err := NormalizeUpstreamPanelSiteType(input.SiteType)
	if err != nil {
		return nil, err
	}
	baseURL, err := NormalizeUpstreamPanelBaseURL(input.BaseURL)
	if err != nil {
		return nil, err
	}
	username := strings.TrimSpace(input.Username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if len(username) > upstreamPanelMaxUsernameLength {
		return nil, fmt.Errorf("username is too long (max %d characters)", upstreamPanelMaxUsernameLength)
	}
	password := input.Password
	if len(password) > upstreamPanelMaxPasswordLength {
		return nil, fmt.Errorf("password is too long (max %d characters)", upstreamPanelMaxPasswordLength)
	}
	if s.accounts != nil {
		if _, err := s.accounts.GetByID(ctx, input.AccountID); err != nil {
			return nil, fmt.Errorf("account %d not found: %w", input.AccountID, err)
		}
	}

	existing, err := s.repo.GetUpstreamPanelCredential(ctx, input.AccountID)
	if err != nil {
		return nil, err
	}
	ciphertext := ""
	if existing != nil {
		ciphertext = existing.PasswordCiphertext
	}
	if password != "" {
		if s.encryptor == nil {
			return nil, fmt.Errorf("credential encryption is unavailable")
		}
		encrypted, encryptErr := s.encryptor.Encrypt(password)
		if encryptErr != nil {
			return nil, fmt.Errorf("failed to encrypt upstream panel password: %w", encryptErr)
		}
		ciphertext = encrypted
	}
	if strings.TrimSpace(ciphertext) == "" {
		return nil, fmt.Errorf("password is required")
	}

	input.SiteType = siteType
	input.BaseURL = baseURL
	input.Username = username
	input.Password = ""
	if err := s.repo.UpsertUpstreamPanelCredential(ctx, input, ciphertext); err != nil {
		return nil, err
	}
	return s.repo.GetUpstreamPanelCredential(ctx, input.AccountID)
}

// Delete 删除某个账号的上游面板凭据。
func (s *UpstreamPanelBalanceService) Delete(ctx context.Context, accountID int64) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("upstream panel balance service is unavailable")
	}
	if accountID <= 0 {
		return fmt.Errorf("account_id is required")
	}
	return s.repo.DeleteUpstreamPanelCredential(ctx, accountID)
}

// Probe 探测单个账号的上游余额并落库，返回更新后的行。
func (s *UpstreamPanelBalanceService) Probe(ctx context.Context, accountID int64) (*UpstreamPanelCredential, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("upstream panel balance service is unavailable")
	}
	credential, err := s.repo.GetUpstreamPanelCredential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if credential == nil {
		return nil, fmt.Errorf("upstream panel credential for account %d is not configured", accountID)
	}
	if credential.SiteType != UpstreamPanelSiteNewAPI && credential.SiteType != UpstreamPanelSiteSub2API {
		return nil, fmt.Errorf("unsupported site_type %q", credential.SiteType)
	}
	password, err := s.decryptPassword(credential)
	if err != nil {
		return nil, err
	}

	outcome := upstreamPanelProbeOutcomeFor(ctx, s.doer, *credential, password, s.currentTime())
	if err := s.repo.SaveUpstreamPanelProbeOutcome(ctx, accountID, outcome); err != nil {
		return nil, err
	}
	return s.repo.GetUpstreamPanelCredential(ctx, accountID)
}

// ProbeAll 批量探测已启用的面板凭据，最多 upstreamPanelMaxBatchSize 个。
func (s *UpstreamPanelBalanceService) ProbeAll(ctx context.Context) (*UpstreamPanelProbeSummary, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("upstream panel balance service is unavailable")
	}
	rows, err := s.repo.ListUpstreamPanelCredentials(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]int64, 0, len(rows))
	for _, row := range rows {
		if !row.Enabled || !row.PasswordConfigured {
			continue
		}
		targets = append(targets, row.AccountID)
		if len(targets) == upstreamPanelMaxBatchSize {
			break
		}
	}

	summary := &UpstreamPanelProbeSummary{Total: len(targets), Results: make([]UpstreamPanelProbeResult, 0, len(targets))}
	if len(targets) == 0 {
		return summary, nil
	}
	results := make([]UpstreamPanelProbeResult, len(targets))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(upstreamPanelProbeConcurrency)
	for index, accountID := range targets {
		index, accountID := index, accountID
		group.Go(func() error {
			row, probeErr := s.Probe(groupCtx, accountID)
			result := UpstreamPanelProbeResult{AccountID: accountID}
			if probeErr != nil {
				result.Status = UpstreamBillingProbeStatusFailed
				result.Error = sanitizeUpstreamPanelMessage(probeErr.Error())
				results[index] = result
				return nil
			}
			result.Status = row.LastStatus
			result.Error = row.LastError
			result.Balance = row.Balance
			result.Unit = row.BalanceUnit
			results[index] = result
			return nil
		})
	}
	_ = group.Wait()
	for _, result := range results {
		if result.Status == UpstreamBillingProbeStatusOK {
			summary.Succeeded++
		} else {
			summary.Failed++
		}
		summary.Results = append(summary.Results, result)
	}
	return summary, nil
}

// UpstreamPanelProbeSummary 批量探测汇总。
type UpstreamPanelProbeSummary struct {
	Total     int                        `json:"total"`
	Succeeded int                        `json:"succeeded"`
	Failed    int                        `json:"failed"`
	Results   []UpstreamPanelProbeResult `json:"results"`
}

// UpstreamPanelProbeResult 单个账号的探测结果。
type UpstreamPanelProbeResult struct {
	AccountID int64    `json:"account_id"`
	Status    string   `json:"status"`
	Error     string   `json:"error,omitempty"`
	Balance   *float64 `json:"balance,omitempty"`
	Unit      string   `json:"unit,omitempty"`
}

func (s *UpstreamPanelBalanceService) decryptPassword(credential *UpstreamPanelCredential) (string, error) {
	ciphertext := strings.TrimSpace(credential.PasswordCiphertext)
	if ciphertext == "" {
		return "", fmt.Errorf("upstream panel password is not configured")
	}
	if s.encryptor == nil {
		return "", fmt.Errorf("credential encryption is unavailable")
	}
	plaintext, err := s.encryptor.Decrypt(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt upstream panel password: %w", err)
	}
	return plaintext, nil
}

func (s *UpstreamPanelBalanceService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func upstreamPanelProbeOutcomeFor(
	ctx context.Context,
	doer upstreamPanelHTTPDoer,
	credential UpstreamPanelCredential,
	password string,
	now time.Time,
) UpstreamPanelProbeOutcome {
	outcome := UpstreamPanelProbeOutcome{Status: UpstreamBillingProbeStatusFailed, At: now}
	var (
		balance  upstreamPanelBalance
		status   int
		probeErr error
	)
	switch credential.SiteType {
	case UpstreamPanelSiteNewAPI:
		balance, status, probeErr = probeNewAPIPanelBalance(ctx, doer, credential.BaseURL, credential.Username, password)
	case UpstreamPanelSiteSub2API:
		balance, status, probeErr = probeSub2APIPanelBalance(ctx, doer, credential.BaseURL, credential.Username, password)
	default:
		probeErr = fmt.Errorf("unsupported site_type %q", credential.SiteType)
	}
	outcome.HTTPStatus = status
	if probeErr != nil {
		outcome.Error = sanitizeUpstreamPanelMessage(probeErr.Error())
		return outcome
	}
	outcome.Status = UpstreamBillingProbeStatusOK
	outcome.Balance = balance.Balance
	outcome.Unit = balance.Unit
	outcome.Detail = balance.Detail
	return outcome
}

// probeNewAPIPanelBalance 登录 NewAPI / one-api 面板并读取 quota。
func probeNewAPIPanelBalance(
	ctx context.Context,
	doer upstreamPanelHTTPDoer,
	baseURL string,
	username string,
	password string,
) (upstreamPanelBalance, int, error) {
	loginStatus, cookies, loginData, err := newAPILogin(ctx, doer, baseURL, username, password)
	if err != nil {
		return upstreamPanelBalance{}, loginStatus, err
	}
	statusCode, data, err := newAPIGetJSON(ctx, doer, baseURL+"/api/user/self", cookies, loginData)
	if err != nil {
		return upstreamPanelBalance{}, statusCode, err
	}
	detail := map[string]any{
		"site_type": UpstreamPanelSiteNewAPI,
		"username":  username,
	}
	quota, hasQuota := upstreamPanelNumber(data["quota"])
	usedQuota, hasUsedQuota := upstreamPanelNumber(data["used_quota"])
	if hasUsedQuota {
		detail["used_quota"] = usedQuota
	}
	quotaPerUnit, hasQuotaPerUnit := newAPIQuotaPerUnit(ctx, doer, baseURL, cookies, loginData)
	if hasQuotaPerUnit {
		detail["quota_per_unit"] = quotaPerUnit
	}
	if hasQuota {
		detail["quota"] = quota
		unit := "quota"
		balance := quota
		if hasQuotaPerUnit && quotaPerUnit > 0 {
			balance = quota / quotaPerUnit
			unit = "USD"
		}
		return upstreamPanelBalance{Balance: &balance, Unit: unit, Detail: detail}, statusCode, nil
	}
	if direct, ok := upstreamPanelNumber(data["balance"]); ok {
		detail["balance"] = direct
		unit := "USD"
		if rawUnit, ok := data["unit"].(string); ok && strings.TrimSpace(rawUnit) != "" {
			unit = sanitizeUpstreamPanelMessage(rawUnit)
		}
		return upstreamPanelBalance{Balance: &direct, Unit: unit, Detail: detail}, statusCode, nil
	}
	return upstreamPanelBalance{}, statusCode, fmt.Errorf("upstream response does not contain a quota or balance field")
}

// probeSub2APIPanelBalance 登录 Sub2API 面板并读取 profile 余额。
func probeSub2APIPanelBalance(
	ctx context.Context,
	doer upstreamPanelHTTPDoer,
	baseURL string,
	username string,
	password string,
) (upstreamPanelBalance, int, error) {
	loginStatus, token, err := sub2APILogin(ctx, doer, baseURL, username, password)
	if err != nil {
		return upstreamPanelBalance{}, loginStatus, err
	}
	statusCode, data, err := sub2APIGetJSON(ctx, doer, baseURL+"/api/v1/user/profile", token)
	if err != nil {
		return upstreamPanelBalance{}, statusCode, err
	}
	detail := map[string]any{
		"site_type": UpstreamPanelSiteSub2API,
		"username":  username,
	}
	balance, hasBalance := upstreamPanelNumber(data["balance"])
	if !hasBalance {
		for _, key := range []string{"quota", "remaining"} {
			if value, ok := upstreamPanelNumber(data[key]); ok {
				balance, hasBalance = value, true
				break
			}
		}
	}
	if !hasBalance {
		return upstreamPanelBalance{}, statusCode, fmt.Errorf("upstream response does not contain a balance field")
	}
	if frozen, ok := upstreamPanelNumber(data["frozen_balance"]); ok {
		detail["frozen_balance"] = frozen
	}
	unit := "USD"
	if rawUnit, ok := data["unit"].(string); ok && strings.TrimSpace(rawUnit) != "" {
		unit = sanitizeUpstreamPanelMessage(rawUnit)
	}
	detail["balance"] = balance
	return upstreamPanelBalance{Balance: &balance, Unit: unit, Detail: detail}, statusCode, nil
}

func newAPILogin(
	ctx context.Context,
	doer upstreamPanelHTTPDoer,
	baseURL string,
	username string,
	password string,
) (int, []*http.Cookie, map[string]any, error) {
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return 0, nil, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/user/login", bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := doer.Do(request)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("login request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := readUpstreamPanelBody(response.Body)
	if err != nil {
		return response.StatusCode, nil, nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, nil, nil, fmt.Errorf("login failed with HTTP %d: %s", response.StatusCode, upstreamPanelBodyMessage(raw))
	}
	envelope, err := decodeUpstreamPanelEnvelope(raw)
	if err != nil {
		return response.StatusCode, nil, nil, err
	}
	if envelope.Success != nil && !*envelope.Success {
		return response.StatusCode, nil, nil, fmt.Errorf("upstream rejected the credentials: %s", upstreamPanelEnvelopeMessage(envelope))
	}
	return response.StatusCode, response.Cookies(), envelope.Data, nil
}

func newAPIGetJSON(
	ctx context.Context,
	doer upstreamPanelHTTPDoer,
	endpoint string,
	cookies []*http.Cookie,
	loginData map[string]any,
) (int, map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Accept", "application/json")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	if loginData != nil {
		if token, ok := loginData["access_token"].(string); ok && strings.TrimSpace(token) != "" {
			request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
		}
		if id, ok := upstreamPanelNumber(loginData["id"]); ok && id > 0 {
			request.Header.Set("New-Api-User", strconv.FormatInt(int64(id), 10))
		}
	}
	response, err := doer.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := readUpstreamPanelBody(response.Body)
	if err != nil {
		return response.StatusCode, nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, nil, fmt.Errorf("upstream returned HTTP %d: %s", response.StatusCode, upstreamPanelBodyMessage(raw))
	}
	envelope, err := decodeUpstreamPanelEnvelope(raw)
	if err != nil {
		return response.StatusCode, nil, err
	}
	if envelope.Success != nil && !*envelope.Success {
		return response.StatusCode, nil, fmt.Errorf("upstream rejected the session: %s", upstreamPanelEnvelopeMessage(envelope))
	}
	return response.StatusCode, envelope.Data, nil
}

func newAPIQuotaPerUnit(
	ctx context.Context,
	doer upstreamPanelHTTPDoer,
	baseURL string,
	cookies []*http.Cookie,
	loginData map[string]any,
) (float64, bool) {
	statusCode, data, err := newAPIGetJSON(ctx, doer, baseURL+"/api/status", cookies, loginData)
	if err != nil || statusCode < 200 || statusCode >= 300 {
		return upstreamPanelNewAPIDefaultQuotaPer, false
	}
	if value, ok := upstreamPanelNumber(data["quota_per_unit"]); ok && value > 0 {
		return value, true
	}
	return upstreamPanelNewAPIDefaultQuotaPer, false
}

func sub2APILogin(
	ctx context.Context,
	doer upstreamPanelHTTPDoer,
	baseURL string,
	username string,
	password string,
) (int, string, error) {
	body, err := json.Marshal(map[string]string{"email": username, "password": password})
	if err != nil {
		return 0, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/auth/login", bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := doer.Do(request)
	if err != nil {
		return 0, "", fmt.Errorf("login request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := readUpstreamPanelBody(response.Body)
	if err != nil {
		return response.StatusCode, "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, "", fmt.Errorf("login failed with HTTP %d: %s", response.StatusCode, upstreamPanelBodyMessage(raw))
	}
	envelope, err := decodeUpstreamPanelEnvelope(raw)
	if err != nil {
		return response.StatusCode, "", err
	}
	if envelope.Success != nil && !*envelope.Success {
		return response.StatusCode, "", fmt.Errorf("upstream rejected the credentials: %s", upstreamPanelEnvelopeMessage(envelope))
	}
	if requires, ok := envelope.Data["requires_2fa"].(bool); ok && requires {
		return response.StatusCode, "", fmt.Errorf("upstream account requires 2FA; this probe cannot complete the second step")
	}
	token, _ := envelope.Data["access_token"].(string)
	token = strings.TrimSpace(token)
	if token == "" {
		if nested, ok := envelope.Data["tokens"].(map[string]any); ok {
			if value, ok := nested["access_token"].(string); ok {
				token = strings.TrimSpace(value)
			}
		}
	}
	if token == "" {
		return response.StatusCode, "", fmt.Errorf("upstream login response does not contain an access token")
	}
	return response.StatusCode, token, nil
}

func sub2APIGetJSON(
	ctx context.Context,
	doer upstreamPanelHTTPDoer,
	endpoint string,
	token string,
) (int, map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := doer.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := readUpstreamPanelBody(response.Body)
	if err != nil {
		return response.StatusCode, nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, nil, fmt.Errorf("upstream returned HTTP %d: %s", response.StatusCode, upstreamPanelBodyMessage(raw))
	}
	envelope, err := decodeUpstreamPanelEnvelope(raw)
	if err != nil {
		return response.StatusCode, nil, err
	}
	if envelope.Success != nil && !*envelope.Success {
		return response.StatusCode, nil, fmt.Errorf("upstream rejected the session: %s", upstreamPanelEnvelopeMessage(envelope))
	}
	if envelope.Code != nil && *envelope.Code != 0 {
		return response.StatusCode, nil, fmt.Errorf("upstream rejected the session: %s", upstreamPanelEnvelopeMessage(envelope))
	}
	return response.StatusCode, envelope.Data, nil
}

func decodeUpstreamPanelEnvelope(raw []byte) (upstreamPanelEnvelope, error) {
	var envelope upstreamPanelEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return upstreamPanelEnvelope{}, fmt.Errorf("upstream returned an invalid JSON response")
	}
	if envelope.Data == nil {
		envelope.Data = map[string]any{}
	}
	return envelope, nil
}

func upstreamPanelEnvelopeMessage(envelope upstreamPanelEnvelope) string {
	if message := sanitizeUpstreamPanelMessage(envelope.Message); message != "" {
		return message
	}
	if envelope.Code != nil {
		return fmt.Sprintf("code=%d", *envelope.Code)
	}
	return "no message"
}

func upstreamPanelBodyMessage(raw []byte) string {
	message := sanitizeUpstreamPanelMessage(string(raw))
	if message == "" {
		return "empty response"
	}
	return message
}

func readUpstreamPanelBody(body io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, upstreamPanelMaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read upstream response: %w", err)
	}
	if len(raw) > upstreamPanelMaxBodyBytes {
		return nil, fmt.Errorf("upstream response exceeds %d bytes", upstreamPanelMaxBodyBytes)
	}
	return raw, nil
}

func upstreamPanelNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0, false
		}
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// sanitizeUpstreamPanelMessage 去掉控制字符并限制长度，避免上游返回内容污染页面与日志。
func sanitizeUpstreamPanelMessage(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	kept := make([]rune, 0, upstreamPanelMaxMessageRunes)
	for _, r := range trimmed {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if len(kept) == upstreamPanelMaxMessageRunes {
			break
		}
		kept = append(kept, r)
	}
	return strings.TrimSpace(string(kept))
}
