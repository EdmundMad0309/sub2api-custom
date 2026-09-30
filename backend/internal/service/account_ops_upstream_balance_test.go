package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeUpstreamPanelSiteType(t *testing.T) {
	cases := map[string]string{
		"newapi":   UpstreamPanelSiteNewAPI,
		"NEW-API":  UpstreamPanelSiteNewAPI,
		" oneapi ": UpstreamPanelSiteNewAPI,
		"sub2api":  UpstreamPanelSiteSub2API,
		"Sub2":     UpstreamPanelSiteSub2API,
	}
	for input, expected := range cases {
		actual, err := NormalizeUpstreamPanelSiteType(input)
		require.NoError(t, err, input)
		require.Equal(t, expected, actual, input)
	}
	if _, err := NormalizeUpstreamPanelSiteType("cherry"); err == nil {
		t.Fatal("unsupported site type must be rejected")
	}
}

func TestNormalizeUpstreamPanelBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://relay.example.com":           "https://relay.example.com",
		"https://relay.example.com/":          "https://relay.example.com",
		"https://relay.example.com/v1":        "https://relay.example.com",
		"https://relay.example.com/api/v1/":   "https://relay.example.com",
		"http://relay.example.com:8080/panel": "http://relay.example.com:8080/panel",
	}
	for input, expected := range cases {
		actual, err := NormalizeUpstreamPanelBaseURL(input)
		require.NoError(t, err, input)
		require.Equal(t, expected, actual, input)
	}
	for _, invalid := range []string{"", "relay.example.com", "ftp://relay.example.com", "https://user:pass@relay.example.com", "https://relay.example.com/?a=1"} {
		if _, err := NormalizeUpstreamPanelBaseURL(invalid); err == nil {
			t.Fatalf("base_url %q must be rejected", invalid)
		}
	}
}

func TestProbeNewAPIPanelBalance(t *testing.T) {
	var selfCookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/login":
			require.Equal(t, http.MethodPost, r.Method)
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "panel-user", body["username"])
			require.Equal(t, "panel-pass", body["password"])
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "sess-1", Path: "/"})
			_, _ = w.Write([]byte(`{"success":true,"message":"","data":{"id":7}}`))
		case "/api/user/self":
			selfCookie = r.Header.Get("Cookie")
			require.Equal(t, "7", r.Header.Get("New-Api-User"))
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota":2500000,"used_quota":100000}}`))
		case "/api/status":
			_, _ = w.Write([]byte(`{"success":true,"data":{"quota_per_unit":500000}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	balance, status, err := probeNewAPIPanelBalance(context.Background(), server.Client(), server.URL, "panel-user", "panel-pass")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.NotNil(t, balance.Balance)
	require.InDelta(t, 5.0, *balance.Balance, 1e-9, "quota / quota_per_unit 应折算成金额")
	require.Equal(t, "USD", balance.Unit)
	require.Contains(t, selfCookie, "session=sess-1", "登录会话 cookie 必须带到余额请求")
	require.EqualValues(t, float64(2500000), balance.Detail["quota"])
	require.EqualValues(t, float64(500000), balance.Detail["quota_per_unit"])
}

func TestProbeNewAPIPanelRejectsBadCredentials(t *testing.T) {
	selfCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/user/self" {
			selfCalled = true
		}
		_, _ = w.Write([]byte(`{"success":false,"message":"用户名或密码错误"}`))
	}))
	defer server.Close()

	_, _, err := probeNewAPIPanelBalance(context.Background(), server.Client(), server.URL, "panel-user", "wrong")
	require.Error(t, err)
	require.Contains(t, err.Error(), "用户名或密码错误")
	require.False(t, selfCalled, "登录失败后不得继续请求余额接口")
}

func TestProbeSub2APIPanelBalance(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "owner@example.com", body["email"])
			require.Equal(t, "panel-pass", body["password"])
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"access_token":"tok-1"}}`))
		case "/api/v1/user/profile":
			authHeader = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"balance":42.5,"frozen_balance":1.25}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	balance, status, err := probeSub2APIPanelBalance(context.Background(), server.Client(), server.URL, "owner@example.com", "panel-pass")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.NotNil(t, balance.Balance)
	require.InDelta(t, 42.5, *balance.Balance, 1e-9)
	require.Equal(t, "USD", balance.Unit)
	require.Equal(t, "Bearer tok-1", authHeader)
	require.EqualValues(t, float64(1.25), balance.Detail["frozen_balance"])
}

func TestProbeSub2APIPanelRequires2FA(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"requires_2fa":true,"temp_token":"t"}}`))
	}))
	defer server.Close()

	_, _, err := probeSub2APIPanelBalance(context.Background(), server.Client(), server.URL, "owner@example.com", "panel-pass")
	require.Error(t, err)
	require.Contains(t, err.Error(), "2FA")
}

func TestUpstreamPanelProbeOutcomeReportsFailures(t *testing.T) {
	outcome := upstreamPanelProbeOutcomeFor(context.Background(), http.DefaultClient, UpstreamPanelCredential{
		SiteType: "unknown",
		BaseURL:  "https://relay.example.com",
		Username: "user",
	}, "pass", time.Now())
	require.Equal(t, UpstreamBillingProbeStatusFailed, outcome.Status)
	require.NotEmpty(t, outcome.Error)
	require.Nil(t, outcome.Balance)
}

type fakeUpstreamPanelRepo struct {
	rows map[int64]*UpstreamPanelCredential
}

func (f *fakeUpstreamPanelRepo) ListUpstreamPanelCredentials(context.Context) ([]UpstreamPanelCredential, error) {
	out := make([]UpstreamPanelCredential, 0, len(f.rows))
	for _, row := range f.rows {
		out = append(out, *row)
	}
	return out, nil
}

func (f *fakeUpstreamPanelRepo) GetUpstreamPanelCredential(_ context.Context, accountID int64) (*UpstreamPanelCredential, error) {
	row, ok := f.rows[accountID]
	if !ok {
		return nil, nil
	}
	copyRow := *row
	return &copyRow, nil
}

func (f *fakeUpstreamPanelRepo) UpsertUpstreamPanelCredential(_ context.Context, input UpstreamPanelCredentialInput, passwordCiphertext string) error {
	if f.rows == nil {
		f.rows = map[int64]*UpstreamPanelCredential{}
	}
	row, ok := f.rows[input.AccountID]
	if !ok {
		row = &UpstreamPanelCredential{AccountID: input.AccountID}
		f.rows[input.AccountID] = row
	}
	row.SiteType = input.SiteType
	row.BaseURL = input.BaseURL
	row.Username = input.Username
	row.Enabled = input.Enabled
	if strings.TrimSpace(passwordCiphertext) != "" {
		row.PasswordCiphertext = passwordCiphertext
	}
	row.PasswordConfigured = strings.TrimSpace(row.PasswordCiphertext) != ""
	return nil
}

func (f *fakeUpstreamPanelRepo) DeleteUpstreamPanelCredential(_ context.Context, accountID int64) error {
	delete(f.rows, accountID)
	return nil
}

func (f *fakeUpstreamPanelRepo) SaveUpstreamPanelProbeOutcome(_ context.Context, accountID int64, outcome UpstreamPanelProbeOutcome) error {
	row := f.rows[accountID]
	if row == nil {
		return nil
	}
	at := outcome.At
	row.LastStatus = outcome.Status
	row.LastError = outcome.Error
	row.LastHTTPStatus = outcome.HTTPStatus
	row.Balance = outcome.Balance
	row.BalanceUnit = outcome.Unit
	row.BalanceDetail = outcome.Detail
	row.LastProbeAt = &at
	return nil
}

type fakeUpstreamPanelEncryptor struct{}

func (fakeUpstreamPanelEncryptor) Encrypt(plaintext string) (string, error) {
	return "enc:" + plaintext, nil
}

func (fakeUpstreamPanelEncryptor) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "enc:"), nil
}

func TestUpstreamPanelSaveKeepsExistingPassword(t *testing.T) {
	repo := &fakeUpstreamPanelRepo{}
	svc := newUpstreamPanelBalanceServiceWithDoer(repo, nil, fakeUpstreamPanelEncryptor{}, http.DefaultClient)

	if _, err := svc.Save(context.Background(), UpstreamPanelCredentialInput{
		AccountID: 9,
		SiteType:  "newapi",
		BaseURL:   "https://relay.example.com/",
		Username:  "owner@example.com",
		Enabled:   true,
	}); err == nil {
		t.Fatal("首次保存缺少密码必须被拒绝")
	}

	saved, err := svc.Save(context.Background(), UpstreamPanelCredentialInput{
		AccountID: 9,
		SiteType:  "newapi",
		BaseURL:   "https://relay.example.com/",
		Username:  "owner@example.com",
		Password:  "panel-pass",
		Enabled:   true,
	})
	require.NoError(t, err)
	require.Equal(t, UpstreamPanelSiteNewAPI, saved.SiteType)
	require.Equal(t, "https://relay.example.com", saved.BaseURL)
	require.True(t, saved.PasswordConfigured)
	require.Equal(t, "enc:panel-pass", repo.rows[9].PasswordCiphertext)

	updated, err := svc.Save(context.Background(), UpstreamPanelCredentialInput{
		AccountID: 9,
		SiteType:  "sub2api",
		BaseURL:   "https://relay.example.com",
		Username:  "owner@example.com",
		Enabled:   false,
	})
	require.NoError(t, err)
	require.Equal(t, UpstreamPanelSiteSub2API, updated.SiteType)
	require.False(t, updated.Enabled)
	require.Equal(t, "enc:panel-pass", repo.rows[9].PasswordCiphertext, "未填写密码时必须沿用原密码")
}

func TestUpstreamPanelProbePersistsBalance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"tok-2"}}`))
		case "/api/v1/user/profile":
			_, _ = w.Write([]byte(`{"code":0,"data":{"balance":8.75}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo := &fakeUpstreamPanelRepo{rows: map[int64]*UpstreamPanelCredential{
		11: {
			AccountID:          11,
			SiteType:           UpstreamPanelSiteSub2API,
			BaseURL:            server.URL,
			Username:           "owner@example.com",
			PasswordCiphertext: "enc:panel-pass",
			PasswordConfigured: true,
			Enabled:            true,
		},
	}}
	svc := newUpstreamPanelBalanceServiceWithDoer(repo, nil, fakeUpstreamPanelEncryptor{}, server.Client())

	row, err := svc.Probe(context.Background(), 11)
	require.NoError(t, err)
	require.Equal(t, UpstreamBillingProbeStatusOK, row.LastStatus)
	require.NotNil(t, row.Balance)
	require.InDelta(t, 8.75, *row.Balance, 1e-9)
	require.Equal(t, "USD", row.BalanceUnit)
	require.NotNil(t, row.LastProbeAt)

	summary, err := svc.ProbeAll(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, summary.Total)
	require.Equal(t, 1, summary.Succeeded)
	require.Equal(t, 0, summary.Failed)
}
