//go:build stress

package stress

import (
	"net/http"
	"sync"
	"testing"
)

// ST-04 — mesma operação por HTTP (várias cópias) em paralelo nas instâncias.
// A parte SQS permanece coberta pelos testes de integração; aqui o foco é
// multi-instância HTTP sob a mesma chave (deduplicação da aplicação).
func TestST04_HTTPBurstSameOperation(t *testing.T) {
	h := newHarness(t)
	walletID, playerID := h.openWallet(t, "1000.00")
	externalID := fmtRunExternal(h.runID, "st04-bet")
	idemKey := h.providerID + ":" + externalID

	const copies = 25
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(copies)
	results := make([]txResponse, copies)
	for i := 0; i < copies; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], _ = h.bet(t, h.nextURL(), walletID, playerID, externalID, "25.00", idemKey)
		}(i)
	}
	close(start)
	wg.Wait()

	var initial int
	for _, r := range results {
		if r.Status != "PROCESSED" {
			t.Fatalf("status=%s", r.Status)
		}
		if !r.IdempotentReplay {
			initial++
		}
	}
	if initial != 1 {
		t.Fatalf("efeitos iniciais=%d", initial)
	}
	if h.getWallet(t, walletID).Balance.Amount != "975.00" {
		t.Fatal("saldo")
	}
	if !h.reconcile(t, walletID).Consistent {
		t.Fatal("reconcile")
	}
}

// ST-17 — isolamento de provedor sob consultas concorrentes.
func TestST17_AuthorizationUnderConcurrency(t *testing.T) {
	h := newHarness(t)
	walletID, playerID := h.openWallet(t, "500.00")
	externalID := fmtRunExternal(h.runID, "st17-bet")
	tx, st := h.bet(t, h.baseURL, walletID, playerID, externalID, "10.00", h.providerID+":"+externalID)
	mustNo5xx(t, st)
	if tx.Status != "PROCESSED" {
		t.Fatalf("status=%s", tx.Status)
	}

	otherToken := fetchToken(t, h,
		envOr("OTHER_PROVIDER_CLIENT_ID", "provider-b"),
		envOr("OTHER_PROVIDER_CLIENT_SECRET", "provider-b-secret"),
	)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	codes := make([]int, 3)

	go func() {
		defer wg.Done()
		<-start
		// Replay autorizado.
		_, codes[0] = h.bet(t, h.baseURL, walletID, playerID, externalID, "10.00", h.providerID+":"+externalID)
	}()
	go func() {
		defer wg.Done()
		<-start
		codes[1] = h.getTransactionStatus(t, otherToken, tx.TransactionID)
	}()
	go func() {
		defer wg.Done()
		<-start
		codes[2] = h.getTransactionStatus(t, "", tx.TransactionID)
	}()
	close(start)
	wg.Wait()

	if codes[0] >= 500 {
		t.Fatalf("replay 5xx: %d", codes[0])
	}
	if codes[1] != http.StatusForbidden && codes[1] != http.StatusNotFound {
		t.Fatalf("provider-b deveria ser negado, got %d", codes[1])
	}
	if codes[2] != http.StatusUnauthorized {
		t.Fatalf("sem token deveria ser 401, got %d", codes[2])
	}
	if h.getWallet(t, walletID).Balance.Amount != "490.00" {
		t.Fatal("saldo alterado por acesso não autorizado")
	}
}

func (h *harness) getTransactionStatus(t *testing.T, token, txID string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.baseURL+"/wagering/transactions/"+txID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode
}
