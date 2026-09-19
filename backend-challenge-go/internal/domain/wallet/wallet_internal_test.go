package wallet

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// O saldo negativo é barrado na abertura e na reidratação, então a proteção
// contra estouro na subtração só é alcançável a partir de um agregado
// corrompido. O teste monta esse estado diretamente, sem afrouxar as validações
// da API pública, para provar que a defesa devolve erro em vez de estourar.
func TestDebitOnCorruptedBalanceFailsInsteadOfOverflowing(t *testing.T) {
	id, err := shared.ParseID("0192f291-27dd-7d3f-8071-5f8685deef37")
	if err != nil {
		t.Fatalf("ParseID devolveu erro: %v", err)
	}
	moment := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	corrupted := &Wallet{
		id:        id,
		playerID:  id,
		currency:  money.BRL,
		balance:   money.FromMinorUnits(math.MinInt64, money.BRL),
		version:   1,
		createdAt: moment,
		updatedAt: moment,
	}

	_, err = corrupted.Debit(money.FromMinorUnits(1, money.BRL), moment)

	if !errors.Is(err, money.ErrOverflow) {
		t.Errorf("erro = %v, esperado ErrOverflow", err)
	}
	if corrupted.version != 1 {
		t.Errorf("version = %d, o estouro não deveria alterar o agregado", corrupted.version)
	}
}
