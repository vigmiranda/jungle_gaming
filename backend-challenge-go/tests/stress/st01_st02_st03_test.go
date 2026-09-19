//go:build stress

package stress

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
)

// ST-01 — mesma aposta 50× via balanceador / round-robin de instâncias.
func TestST01_SameBetFiftyTimesAcrossInstances(t *testing.T) {
	h := newHarness(t)
	walletID, playerID := h.openWallet(t, "1000.00")
	externalID := fmtRunExternal(h.runID, "st01-bet")
	idemKey := h.providerID + ":" + externalID

	const attempts = 50
	results := make([]txResponse, attempts)
	statuses := make([]int, attempts)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			base := h.baseURL
			if i%2 == 1 {
				base = h.nextURL()
			}
			results[i], statuses[i] = h.bet(t, base, walletID, playerID, externalID, "10.00", idemKey)
		}(i)
	}
	close(start)
	wg.Wait()

	var initial, replays int
	txID := ""
	for i := range results {
		mustNo5xx(t, statuses[i])
		if statuses[i] != 200 && statuses[i] != 201 {
			t.Fatalf("status[%d]=%d", i, statuses[i])
		}
		if results[i].Status != "PROCESSED" {
			t.Fatalf("status=%s", results[i].Status)
		}
		if txID == "" {
			txID = results[i].TransactionID
		} else if results[i].TransactionID != txID {
			t.Fatalf("transactionId divergiu: %s vs %s", txID, results[i].TransactionID)
		}
		if results[i].IdempotentReplay {
			replays++
		} else {
			initial++
		}
	}
	if initial != 1 || replays != attempts-1 {
		t.Fatalf("initial=%d replays=%d", initial, replays)
	}
	wallet := h.getWallet(t, walletID)
	if wallet.Balance.Amount != "990.00" {
		t.Fatalf("saldo=%s esperado 990.00", wallet.Balance.Amount)
	}
	if wallet.Version != 2 {
		t.Fatalf("version=%d esperado 2", wallet.Version)
	}
	rec := h.reconcile(t, walletID)
	if !rec.Consistent || rec.Difference.Amount != "0.00" {
		t.Fatalf("reconciliação: %+v", rec)
	}
}

// ST-02 — duas apostas de 80 sobre 100, repetido N vezes (STRESS_TESTS pede ≥100).
func TestST02_TwoCompetingBetsRepeated(t *testing.T) {
	h := newHarness(t)
	rounds := 20
	if v := os.Getenv("ST02_ROUNDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rounds = n
		}
	}

	for round := 0; round < rounds; round++ {
		walletID, playerID := h.openWallet(t, "100.00")
		extA := fmtRunExternal(h.runID, "st02-a-"+strconv.Itoa(round))
		extB := fmtRunExternal(h.runID, "st02-b-"+strconv.Itoa(round))

		type outcome struct {
			tx  txResponse
			st  int
		}
		out := make([]outcome, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			tx, st := h.bet(t, h.instances[0], walletID, playerID, extA, "80.00", h.providerID+":"+extA)
			out[0] = outcome{tx, st}
		}()
		go func() {
			defer wg.Done()
			<-start
			tx, st := h.bet(t, h.instances[1%len(h.instances)], walletID, playerID, extB, "80.00", h.providerID+":"+extB)
			out[1] = outcome{tx, st}
		}()
		close(start)
		wg.Wait()

		var processed, rejected int
		for _, o := range out {
			mustNo5xx(t, o.st)
			switch o.tx.Status {
			case "PROCESSED":
				if o.st != http.StatusOK {
					t.Fatalf("processed com status HTTP %d", o.st)
				}
				processed++
			case "REJECTED":
				if o.st != http.StatusUnprocessableEntity && o.st != http.StatusOK {
					t.Fatalf("rejected com status HTTP %d", o.st)
				}
				rejected++
				if o.tx.FailureCode != "INSUFFICIENT_FUNDS" {
					t.Fatalf("failureCode=%q", o.tx.FailureCode)
				}
			default:
				t.Fatalf("status inesperado: %s http=%d", o.tx.Status, o.st)
			}
		}
		if processed != 1 || rejected != 1 {
			t.Fatalf("round %d: processed=%d rejected=%d", round, processed, rejected)
		}
		wallet := h.getWallet(t, walletID)
		if wallet.Balance.Amount != "20.00" {
			t.Fatalf("round %d saldo=%s", round, wallet.Balance.Amount)
		}
		// Reenvios não alteram.
		for _, ext := range []string{extA, extB} {
			_, st := h.bet(t, h.baseURL, walletID, playerID, ext, "80.00", h.providerID+":"+ext)
			mustNo5xx(t, st)
		}
		if h.getWallet(t, walletID).Balance.Amount != "20.00" {
			t.Fatalf("round %d saldo após reenvio", round)
		}
		if !h.reconcile(t, walletID).Consistent {
			t.Fatalf("round %d inconsistente", round)
		}
	}
}

// ST-03 — carteiras independentes em paralelo nas três instâncias.
func TestST03_IndependentWalletsParallel(t *testing.T) {
	h := newHarness(t)
	const wallets = 20
	ids := make([]string, wallets)
	players := make([]string, wallets)
	for i := 0; i < wallets; i++ {
		ids[i], players[i] = h.openWallet(t, "1000.00")
	}

	start := make(chan struct{})
	errs := make([]error, wallets)
	var wg sync.WaitGroup
	wg.Add(wallets)
	for i := 0; i < wallets; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			ext := fmtRunExternal(h.runID, "st03-"+strconv.Itoa(i))
			tx, st := h.bet(t, h.nextURL(), ids[i], players[i], ext, "25.00", h.providerID+":"+ext)
			if st >= 500 {
				errs[i] = errStatus(st)
				return
			}
			if tx.Status != "PROCESSED" {
				errs[i] = errStatus(st)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("carteira %d: %v", i, err)
		}
		if h.getWallet(t, ids[i]).Balance.Amount != "975.00" {
			t.Fatalf("carteira %d saldo", i)
		}
		if !h.reconcile(t, ids[i]).Consistent {
			t.Fatalf("carteira %d reconcile", i)
		}
	}
}

type statusError int

func (e statusError) Error() string { return "bad status " + strconv.Itoa(int(e)) }
func errStatus(st int) error        { return statusError(st) }
