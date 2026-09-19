//go:build stress

// Package stress exercita cenários ST via HTTP autenticado contra o Compose
// multi-instância (perfil stress). Requer ambiente no ar:
//
//	docker compose --profile stress up -d --build
//	go test -tags=stress -count=1 ./tests/stress/...
package stress

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type harness struct {
	runID          string
	baseURL        string
	instances      []string
	keycloakURL    string
	providerToken  string
	internalToken  string
	providerID     string
	client         *http.Client
	roundRobin     int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		runID:       "run-" + uuid.NewString(),
		baseURL:     envOr("BASE_URL", "http://localhost:8090"),
		keycloakURL: envOr("KEYCLOAK_URL", "http://localhost:8088"),
		providerID:  envOr("TEST_PROVIDER_ID", "provider-a"),
		client:      &http.Client{Timeout: 30 * time.Second},
	}
	if raw := os.Getenv("INSTANCE_URLS"); raw != "" {
		h.instances = splitCSV(raw)
	} else {
		h.instances = []string{
			envOr("INSTANCE_1", "http://localhost:8081"),
			envOr("INSTANCE_2", "http://localhost:8082"),
			envOr("INSTANCE_3", "http://localhost:8083"),
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, u := range append([]string{h.baseURL}, h.instances...) {
		waitReady(t, ctx, h.client, u)
	}

	h.providerToken = fetchToken(t, h,
		envOr("PROVIDER_CLIENT_ID", "provider-a"),
		envOr("PROVIDER_CLIENT_SECRET", "provider-a-secret"),
	)
	h.internalToken = fetchToken(t, h,
		envOr("INTERNAL_CLIENT_ID", "internal-service"),
		envOr("INTERNAL_CLIENT_SECRET", "internal-service-secret"),
	)
	t.Logf("stress harness runId=%s base=%s instances=%v", h.runID, h.baseURL, h.instances)
	return h
}

func (h *harness) nextURL() string {
	if len(h.instances) == 0 {
		return h.baseURL
	}
	u := h.instances[h.roundRobin%len(h.instances)]
	h.roundRobin++
	return u
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func waitReady(t *testing.T, ctx context.Context, client *http.Client, base string) {
	t.Helper()
	url := strings.TrimRight(base, "/") + "/health/ready"
	Eventually(t, ctx, 200*time.Millisecond, func(ctx context.Context) (bool, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return false, err
		}
		res, err := client.Do(req)
		if err != nil {
			return false, nil
		}
		defer res.Body.Close()
		return res.StatusCode == http.StatusOK, nil
	})
}

func fetchToken(t *testing.T, h *harness, clientID, secret string) string {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", secret)
	endpoint := strings.TrimRight(h.keycloakURL, "/") + "/realms/wagering/protocol/openid-connect/token"
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("token %s: %v", clientID, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("token %s status=%d body=%s", clientID, res.StatusCode, body)
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.AccessToken == "" {
		t.Fatalf("token parse: %v body=%s", err, body)
	}
	return parsed.AccessToken
}

// Eventually faz polling até timeout do contexto (STRESS_TESTS §11).
func Eventually(t *testing.T, ctx context.Context, interval time.Duration, fn func(context.Context) (bool, error)) {
	t.Helper()
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	var last error
	for {
		ok, err := fn(ctx)
		if err != nil {
			last = err
		} else if ok {
			return
		}
		select {
		case <-ctx.Done():
			if last != nil {
				t.Fatalf("timeout waiting: %v (%v)", ctx.Err(), last)
			}
			t.Fatalf("timeout waiting: %v", ctx.Err())
		case <-time.After(interval):
		}
	}
}

type moneyBody struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type walletResponse struct {
	ID      string    `json:"id"`
	Balance moneyBody `json:"balance"`
	Version int64     `json:"version"`
}

type txResponse struct {
	TransactionID    string    `json:"transactionId"`
	Status           string    `json:"status"`
	FailureCode      string    `json:"failureCode"`
	IdempotentReplay bool      `json:"idempotentReplay"`
	Balance          moneyBody `json:"balance"`
}

type reconcileResponse struct {
	Consistent bool      `json:"consistent"`
	Difference moneyBody `json:"difference"`
}

func (h *harness) openWallet(t *testing.T, balance string) (walletID, playerID string) {
	t.Helper()
	playerID = uuid.NewString()
	payload := map[string]any{
		"playerId": playerID,
		"initialBalance": moneyBody{Amount: balance, Currency: "BRL"},
	}
	var out walletResponse
	status := h.doJSON(t, h.baseURL, http.MethodPost, "/wallets", h.internalToken, "", payload, &out)
	if status != http.StatusCreated {
		t.Fatalf("open wallet status=%d", status)
	}
	return out.ID, playerID
}

func (h *harness) bet(t *testing.T, base, walletID, playerID, externalID, amount, idemKey string) (txResponse, int) {
	t.Helper()
	payload := map[string]any{
		"providerId":            h.providerID,
		"externalTransactionId": externalID,
		"playerId":              playerID,
		"walletId":              walletID,
		"roundId":               "stress-round",
		"gameId":                "stress-game",
		"kind":                  "BET",
		"money":                 moneyBody{Amount: amount, Currency: "BRL"},
	}
	var out txResponse
	status := h.doJSON(t, base, http.MethodPost, "/wagering/transactions", h.providerToken, idemKey, payload, &out)
	return out, status
}

func (h *harness) reconcile(t *testing.T, walletID string) reconcileResponse {
	t.Helper()
	var out reconcileResponse
	status := h.doJSON(t, h.baseURL, http.MethodPost, "/wallets/"+walletID+"/reconciliation", h.internalToken, "", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("reconcile status=%d", status)
	}
	return out
}

func (h *harness) getWallet(t *testing.T, walletID string) walletResponse {
	t.Helper()
	var out walletResponse
	status := h.doJSON(t, h.baseURL, http.MethodGet, "/wallets/"+walletID, h.internalToken, "", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("get wallet status=%d", status)
	}
	return out
}

func (h *harness) doJSON(
	t *testing.T,
	base, method, path, token, idemKey string,
	payload any,
	out any,
) int {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Correlation-Id", h.runID+"-"+uuid.NewString())
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	res, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if out != nil && len(raw) > 0 && res.StatusCode < 500 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode %s: %v body=%s", path, err, raw)
		}
	}
	if res.StatusCode >= 500 {
		t.Logf("5xx %s %s: %s", method, path, raw)
	}
	return res.StatusCode
}

func mustNo5xx(t *testing.T, status int) {
	t.Helper()
	if status >= 500 {
		t.Fatalf("status permanente 5xx: %d", status)
	}
}

func fmtRunExternal(runID, name string) string {
	return fmt.Sprintf("%s-%s", runID, name)
}
