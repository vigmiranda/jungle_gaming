package money_test

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/vigmi/backend-challenge-go/internal/domain/money"
)

func mustParse(t *testing.T, amount, currency string) money.Money {
	t.Helper()
	parsed, err := money.Parse(amount, currency)
	if err != nil {
		t.Fatalf("Parse(%q, %q) devolveu erro: %v", amount, currency, err)
	}
	return parsed
}

func TestParseAcceptsCanonicalValues(t *testing.T) {
	tests := []struct {
		amount string
		minor  int64
	}{
		{"0.00", 0},
		{"0.01", 1},
		{"0.99", 99},
		{"1.00", 100},
		{"25.00", 2500},
		{"1000.00", 100000},
		{"-25.00", -2500},
		{"-0.01", -1},
		{"92233720368547758.07", math.MaxInt64},
		{"-92233720368547758.07", -math.MaxInt64},
	}

	for _, tt := range tests {
		t.Run(tt.amount, func(t *testing.T) {
			parsed := mustParse(t, tt.amount, "BRL")

			if parsed.MinorUnits() != tt.minor {
				t.Errorf("MinorUnits = %d, esperado %d", parsed.MinorUnits(), tt.minor)
			}
			if parsed.Currency() != money.BRL {
				t.Errorf("Currency = %q, esperado BRL", parsed.Currency())
			}
			if parsed.String() != tt.amount {
				t.Errorf("String = %q, esperado %q (ida e volta deve preservar a forma)", parsed.String(), tt.amount)
			}
		})
	}
}

// A política é estrita: nada é normalizado silenciosamente, para que o hash de
// idempotência seja idêntico entre HTTP e SQS por construção (ADR-021).
func TestParseRejectsNonCanonicalAmounts(t *testing.T) {
	tests := []struct {
		name   string
		amount string
	}{
		{"vazio", ""},
		{"sem casas decimais", "25"},
		{"uma casa decimal", "25.0"},
		{"três casas decimais", "25.000"},
		{"separador de milhar", "1,000.00"},
		{"vírgula como separador", "25,00"},
		{"espaço à esquerda", " 25.00"},
		{"espaço à direita", "25.00 "},
		{"espaço interno", "25. 00"},
		{"notação científica", "2.5e1"},
		{"expoente maiúsculo", "1.0E2"},
		{"NaN", "NaN"},
		{"Infinity", "Infinity"},
		{"sinal positivo explícito", "+25.00"},
		{"zeros à esquerda", "025.00"},
		{"zeros à esquerda negativo", "-025.00"},
		{"sem parte inteira", ".50"},
		{"apenas o ponto", "."},
		{"apenas o sinal", "-"},
		{"sinal sem dígitos", "-.00"},
		{"ponto duplicado", "25.00.00"},
		{"zero negativo", "-0.00"},
		{"letra na parte decimal", "25.0a"},
		{"espaço na parte decimal", "25. 0"},
		{"letras", "abc"},
		{"dígito não ASCII", "٢٥.٠٠"},
		{"hexadecimal", "0x19.00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := money.Parse(tt.amount, "BRL")

			if err == nil {
				t.Fatalf("Parse(%q) deveria falhar", tt.amount)
			}
			if !errors.Is(err, money.ErrInvalidAmount) {
				t.Errorf("erro = %v, esperado ErrInvalidAmount", err)
			}
		})
	}
}

// Cada entrada exercita um ponto de estouro diferente na conversão: a soma do
// dígito, a multiplicação por dez a cada dígito e a multiplicação pela escala.
func TestParseRejectsAmountsBeyondRange(t *testing.T) {
	tests := []string{
		"92233720368547758.08",
		"-92233720368547758.08",
		"9223372036854775808.00",
		"922337203685477581.00",
		"99999999999999999999.00",
	}

	for _, amount := range tests {
		t.Run(amount, func(t *testing.T) {
			_, err := money.Parse(amount, "BRL")

			if !errors.Is(err, money.ErrOverflow) {
				t.Errorf("erro = %v, esperado ErrOverflow", err)
			}
		})
	}
}

func TestParseCurrencyRejectsInvalidCodes(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{"vazio", ""},
		{"curto demais", "BR"},
		{"longo demais", "BRLX"},
		{"minúsculas", "brl"},
		{"minúsculas parciais", "Brl"},
		{"dígitos", "986"},
		{"com espaço", "BR "},
		{"não ASCII", "BRÇ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := money.Parse("25.00", tt.code)

			if !errors.Is(err, money.ErrInvalidCurrency) {
				t.Errorf("erro = %v, esperado ErrInvalidCurrency", err)
			}
		})
	}
}

func TestParseCurrencyAcceptsISOShape(t *testing.T) {
	for _, code := range []string{"BRL", "USD", "EUR"} {
		parsed, err := money.ParseCurrency(code)
		if err != nil {
			t.Fatalf("ParseCurrency(%q) devolveu erro: %v", code, err)
		}
		if parsed.String() != code {
			t.Errorf("String = %q, esperado %q", parsed.String(), code)
		}
		if parsed.IsZero() {
			t.Errorf("%q não deveria ser considerada não inicializada", code)
		}
		if err := parsed.Validate(); err != nil {
			t.Errorf("Validate devolveu erro para %q: %v", code, err)
		}
	}
}

func TestZeroCurrencyIsInvalid(t *testing.T) {
	var currency money.Currency

	if !currency.IsZero() {
		t.Error("a moeda vazia deveria ser considerada não inicializada")
	}
	if err := currency.Validate(); !errors.Is(err, money.ErrUninitialized) {
		t.Errorf("erro = %v, esperado ErrUninitialized", err)
	}
}

func TestZeroAndFromMinorUnits(t *testing.T) {
	zero := money.Zero(money.BRL)

	if !zero.IsZero() || zero.IsPositive() || zero.IsNegative() {
		t.Errorf("Zero = %s, esperado exatamente zero", zero)
	}
	if zero.String() != "0.00" {
		t.Errorf("String = %q, esperado \"0.00\"", zero.String())
	}

	rehydrated := money.FromMinorUnits(2500, money.BRL)

	if !rehydrated.Equal(mustParse(t, "25.00", "BRL")) {
		t.Errorf("FromMinorUnits = %s, esperado 25.00", rehydrated)
	}
}

func TestSignPredicates(t *testing.T) {
	positive := mustParse(t, "0.01", "BRL")
	negative := mustParse(t, "-0.01", "BRL")

	if !positive.IsPositive() || positive.IsNegative() || positive.IsZero() {
		t.Errorf("predicados incorretos para %s", positive)
	}
	if !negative.IsNegative() || negative.IsPositive() || negative.IsZero() {
		t.Errorf("predicados incorretos para %s", negative)
	}
}

func TestAddAndSub(t *testing.T) {
	tests := []struct {
		name     string
		left     string
		right    string
		sum      string
		differen string
	}{
		{"positivos", "100.00", "25.00", "125.00", "75.00"},
		{"resultado negativo", "25.00", "100.00", "125.00", "-75.00"},
		{"com zero", "25.00", "0.00", "25.00", "25.00"},
		{"centavos", "0.01", "0.02", "0.03", "-0.01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			left, right := mustParse(t, tt.left, "BRL"), mustParse(t, tt.right, "BRL")

			sum, err := left.Add(right)
			if err != nil {
				t.Fatalf("Add devolveu erro: %v", err)
			}
			if sum.String() != tt.sum {
				t.Errorf("Add = %s, esperado %s", sum, tt.sum)
			}

			difference, err := left.Sub(right)
			if err != nil {
				t.Fatalf("Sub devolveu erro: %v", err)
			}
			if difference.String() != tt.differen {
				t.Errorf("Sub = %s, esperado %s", difference, tt.differen)
			}
		})
	}
}

func TestArithmeticDetectsOverflow(t *testing.T) {
	maximum := money.FromMinorUnits(math.MaxInt64, money.BRL)
	minimum := money.FromMinorUnits(math.MinInt64, money.BRL)
	one := mustParse(t, "0.01", "BRL")

	if _, err := maximum.Add(one); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("Add no limite superior: erro = %v, esperado ErrOverflow", err)
	}
	if _, err := minimum.Sub(one); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("Sub no limite inferior: erro = %v, esperado ErrOverflow", err)
	}
	if _, err := minimum.Add(money.FromMinorUnits(-1, money.BRL)); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("Add negativo no limite inferior: erro = %v, esperado ErrOverflow", err)
	}
	if _, err := maximum.Sub(money.FromMinorUnits(-1, money.BRL)); !errors.Is(err, money.ErrOverflow) {
		t.Errorf("Sub negativo no limite superior: erro = %v, esperado ErrOverflow", err)
	}
}

func TestNegate(t *testing.T) {
	negated, err := mustParse(t, "25.00", "BRL").Negate()
	if err != nil {
		t.Fatalf("Negate devolveu erro: %v", err)
	}
	if negated.String() != "-25.00" {
		t.Errorf("Negate = %s, esperado -25.00", negated)
	}

	back, err := negated.Negate()
	if err != nil {
		t.Fatalf("Negate devolveu erro: %v", err)
	}
	if back.String() != "25.00" {
		t.Errorf("dupla negação = %s, esperado 25.00", back)
	}

	zero, err := money.Zero(money.BRL).Negate()
	if err != nil {
		t.Fatalf("Negate de zero devolveu erro: %v", err)
	}
	if zero.String() != "0.00" {
		t.Errorf("negação de zero = %s, esperado 0.00", zero)
	}
}

func TestNegateDetectsOverflow(t *testing.T) {
	_, err := money.FromMinorUnits(math.MinInt64, money.BRL).Negate()

	if !errors.Is(err, money.ErrOverflow) {
		t.Errorf("erro = %v, esperado ErrOverflow", err)
	}
}

func TestCompare(t *testing.T) {
	smaller := mustParse(t, "25.00", "BRL")
	bigger := mustParse(t, "80.00", "BRL")

	if result, err := smaller.Compare(bigger); err != nil || result != -1 {
		t.Errorf("Compare(menor, maior) = (%d, %v), esperado (-1, nil)", result, err)
	}
	if result, err := bigger.Compare(smaller); err != nil || result != 1 {
		t.Errorf("Compare(maior, menor) = (%d, %v), esperado (1, nil)", result, err)
	}
	if result, err := smaller.Compare(mustParse(t, "25.00", "BRL")); err != nil || result != 0 {
		t.Errorf("Compare(igual, igual) = (%d, %v), esperado (0, nil)", result, err)
	}
}

func TestEqual(t *testing.T) {
	value := mustParse(t, "25.00", "BRL")

	if !value.Equal(mustParse(t, "25.00", "BRL")) {
		t.Error("valores idênticos deveriam ser iguais")
	}
	if value.Equal(mustParse(t, "25.01", "BRL")) {
		t.Error("valores diferentes não deveriam ser iguais")
	}
	if value.Equal(mustParse(t, "25.00", "USD")) {
		t.Error("moedas diferentes não deveriam ser iguais")
	}
}

// Aritmética e comparação exigem moedas compatíveis; BRL é a moeda dos cenários
// principais, mas o tipo carrega a moeda e precisa recusar a mistura.
func TestOperationsRejectCurrencyMismatch(t *testing.T) {
	brl := mustParse(t, "25.00", "BRL")
	usd := mustParse(t, "25.00", "USD")

	if _, err := brl.Add(usd); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Errorf("Add: erro = %v, esperado ErrCurrencyMismatch", err)
	}
	if _, err := brl.Sub(usd); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Errorf("Sub: erro = %v, esperado ErrCurrencyMismatch", err)
	}
	if _, err := brl.Compare(usd); !errors.Is(err, money.ErrCurrencyMismatch) {
		t.Errorf("Compare: erro = %v, esperado ErrCurrencyMismatch", err)
	}
}

func TestOperationsRejectUninitializedValue(t *testing.T) {
	var uninitialized money.Money
	valid := mustParse(t, "25.00", "BRL")

	if err := uninitialized.Validate(); !errors.Is(err, money.ErrUninitialized) {
		t.Errorf("Validate: erro = %v, esperado ErrUninitialized", err)
	}
	if _, err := uninitialized.Add(valid); !errors.Is(err, money.ErrUninitialized) {
		t.Errorf("Add com receptor não inicializado: erro = %v", err)
	}
	if _, err := valid.Add(uninitialized); !errors.Is(err, money.ErrUninitialized) {
		t.Errorf("Add com argumento não inicializado: erro = %v", err)
	}
	if _, err := uninitialized.Sub(valid); !errors.Is(err, money.ErrUninitialized) {
		t.Errorf("Sub com receptor não inicializado: erro = %v", err)
	}
	if _, err := uninitialized.Compare(valid); !errors.Is(err, money.ErrUninitialized) {
		t.Errorf("Compare com receptor não inicializado: erro = %v", err)
	}
	if _, err := uninitialized.Negate(); !errors.Is(err, money.ErrUninitialized) {
		t.Errorf("Negate com receptor não inicializado: erro = %v", err)
	}
	if _, err := uninitialized.MarshalJSON(); !errors.Is(err, money.ErrUninitialized) {
		t.Errorf("MarshalJSON com valor não inicializado: erro = %v", err)
	}
}

func TestMarshalJSONUsesExternalContract(t *testing.T) {
	encoded, err := json.Marshal(mustParse(t, "25.00", "BRL"))
	if err != nil {
		t.Fatalf("Marshal devolveu erro: %v", err)
	}

	if got, want := string(encoded), `{"amount":"25.00","currency":"BRL"}`; got != want {
		t.Errorf("JSON = %s, esperado %s", got, want)
	}
}

func TestUnmarshalJSONAppliesStrictParsing(t *testing.T) {
	var decoded money.Money
	if err := json.Unmarshal([]byte(`{"amount":"25.00","currency":"BRL"}`), &decoded); err != nil {
		t.Fatalf("Unmarshal devolveu erro: %v", err)
	}
	if decoded.MinorUnits() != 2500 || decoded.Currency() != money.BRL {
		t.Errorf("decodificado = %s %s", decoded, decoded.Currency())
	}

	tests := []struct {
		name    string
		payload string
		want    error
	}{
		{"escala inválida", `{"amount":"25.0","currency":"BRL"}`, money.ErrInvalidAmount},
		{"moeda inválida", `{"amount":"25.00","currency":"brl"}`, money.ErrInvalidCurrency},
		{"amount numérico", `{"amount":25.00,"currency":"BRL"}`, money.ErrInvalidAmount},
		{"campos ausentes", `{}`, money.ErrInvalidCurrency},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var value money.Money
			err := json.Unmarshal([]byte(tt.payload), &value)

			if !errors.Is(err, tt.want) {
				t.Errorf("erro = %v, esperado %v", err, tt.want)
			}
		})
	}
}

// Um payload truncado nunca chega ao decodificador do tipo quando passa por
// json.Unmarshal, então a proteção interna é exercitada diretamente.
func TestUnmarshalJSONRejectsMalformedPayload(t *testing.T) {
	var value money.Money

	err := value.UnmarshalJSON([]byte(`{"amount":`))

	if !errors.Is(err, money.ErrInvalidAmount) {
		t.Errorf("erro = %v, esperado ErrInvalidAmount", err)
	}
	if errors.Unwrap(err) == nil {
		t.Error("o erro deveria encadear a causa de decodificação")
	}
}

func TestStringFormatsEdgeValues(t *testing.T) {
	tests := []struct {
		minor int64
		want  string
	}{
		{0, "0.00"},
		{5, "0.05"},
		{-5, "-0.05"},
		{100, "1.00"},
		{-100, "-1.00"},
		{math.MaxInt64, "92233720368547758.07"},
		{math.MinInt64, "-92233720368547758.08"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := money.FromMinorUnits(tt.minor, money.BRL).String(); got != tt.want {
				t.Errorf("String = %q, esperado %q", got, tt.want)
			}
		})
	}
}
