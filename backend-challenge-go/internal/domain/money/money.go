// Package money implementa o valor monetário do domínio.
//
// O valor é guardado em `int64` de unidades mínimas (centavos), com escala fixa
// de duas casas. Nenhum ponto flutuante participa de parsing, cálculo,
// serialização ou persistência.
//
// Limites: o valor representável vai de -92.233.720.368.547.758,07 a
// 92.233.720.368.547.758,07. Operações que ultrapassem essa faixa devolvem erro
// em vez de estourar silenciosamente.
package money

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
)

// Erros do pacote. A comparação é por código, via `errors.Is`.
var (
	// ErrInvalidAmount cobre formato, escala e caracteres inválidos.
	ErrInvalidAmount = shared.Validation("INVALID_AMOUNT", "valor monetário inválido")
	// ErrInvalidCurrency cobre códigos de moeda fora do formato ISO 4217.
	ErrInvalidCurrency = shared.Validation("INVALID_CURRENCY", "código de moeda inválido")
	// ErrCurrencyMismatch cobre aritmética e comparação entre moedas distintas.
	ErrCurrencyMismatch = shared.Rejection("CURRENCY_MISMATCH", "moedas incompatíveis")
	// ErrOverflow cobre estouro na conversão e na aritmética.
	ErrOverflow = shared.Validation("AMOUNT_OVERFLOW", "valor monetário fora da faixa representável")
	// ErrUninitialized cobre o uso de um valor de domínio não inicializado.
	ErrUninitialized = shared.Invariant("UNINITIALIZED_MONEY", "valor monetário não inicializado")
)

const (
	// scale é a quantidade fixa de casas decimais no contrato externo.
	scale = 2
	// minorPerUnit é quantas unidades mínimas cabem em uma unidade da moeda.
	minorPerUnit = 100
)

// Money é um valor monetário imutável: valor e moeda.
//
// O valor zero do tipo é inválido; use Parse, Zero ou FromMinorUnits.
type Money struct {
	minor    int64
	currency Currency
}

// Zero devolve o valor nulo da moeda informada.
func Zero(currency Currency) Money {
	return Money{minor: 0, currency: currency}
}

// FromMinorUnits cria um valor a partir de unidades mínimas já validadas.
//
// Usado na reidratação a partir do banco, onde o valor foi persistido em
// `BIGINT`.
func FromMinorUnits(minor int64, currency Currency) Money {
	return Money{minor: minor, currency: currency}
}

// Parse cria um valor a partir da forma canônica do contrato externo.
//
// A política é estrita (ADR-021): apenas duas casas decimais, sem espaços, sem
// notação científica e sem zeros à esquerda. Formas equivalentes como "25.0" ou
// "025.00" são rejeitadas em vez de normalizadas, para que o hash de
// idempotência seja idêntico entre HTTP e SQS por construção.
func Parse(amount, currency string) (Money, error) {
	parsedCurrency, err := ParseCurrency(currency)
	if err != nil {
		return Money{}, err
	}
	minor, err := parseMinorUnits(amount)
	if err != nil {
		return Money{}, err
	}
	return Money{minor: minor, currency: parsedCurrency}, nil
}

func parseMinorUnits(raw string) (int64, error) {
	if raw == "" {
		return 0, ErrInvalidAmount.Messagef("valor monetário vazio")
	}

	body := raw
	negative := body[0] == '-'
	if negative {
		body = body[1:]
	}

	separator := strings.IndexByte(body, '.')
	if separator < 0 {
		return 0, ErrInvalidAmount.Messagef(
			"valor deve usar escala fixa de %d casas decimais, recebido %q", scale, raw)
	}

	integerPart, fractionPart := body[:separator], body[separator+1:]
	if len(fractionPart) != scale {
		return 0, ErrInvalidAmount.Messagef(
			"valor deve ter exatamente %d casas decimais, recebido %q", scale, raw)
	}
	if integerPart == "" {
		return 0, ErrInvalidAmount.Messagef("valor sem parte inteira, recebido %q", raw)
	}
	if len(integerPart) > 1 && integerPart[0] == '0' {
		return 0, ErrInvalidAmount.Messagef("valor com zeros à esquerda, recebido %q", raw)
	}
	if err := requireDigits(integerPart, raw); err != nil {
		return 0, err
	}
	if err := requireDigits(fractionPart, raw); err != nil {
		return 0, err
	}

	minor, err := digitsToMinorUnits(integerPart, fractionPart, raw)
	if err != nil {
		return 0, err
	}

	if !negative {
		return minor, nil
	}
	if minor == 0 {
		return 0, ErrInvalidAmount.Messagef("zero negativo não é uma forma canônica, recebido %q", raw)
	}
	return -minor, nil
}

// requireDigits rejeita sinais, espaços, notação científica e dígitos não ASCII.
func requireDigits(part, raw string) error {
	for i := 0; i < len(part); i++ {
		if part[i] < '0' || part[i] > '9' {
			return ErrInvalidAmount.Messagef(
				"valor deve conter apenas dígitos decimais, recebido %q", raw)
		}
	}
	return nil
}

func digitsToMinorUnits(integerPart, fractionPart, raw string) (int64, error) {
	var minor int64
	for i := 0; i < len(integerPart); i++ {
		digit := int64(integerPart[i] - '0')
		scaled, err := multiply(minor, 10)
		if err != nil {
			return 0, ErrOverflow.Messagef("valor excede a faixa representável, recebido %q", raw)
		}
		minor, err = add(scaled, digit)
		if err != nil {
			return 0, ErrOverflow.Messagef("valor excede a faixa representável, recebido %q", raw)
		}
	}

	scaled, err := multiply(minor, minorPerUnit)
	if err != nil {
		return 0, ErrOverflow.Messagef("valor excede a faixa representável, recebido %q", raw)
	}

	fraction := int64(fractionPart[0]-'0')*10 + int64(fractionPart[1]-'0')
	minor, err = add(scaled, fraction)
	if err != nil {
		return 0, ErrOverflow.Messagef("valor excede a faixa representável, recebido %q", raw)
	}
	return minor, nil
}

// MinorUnits devolve o valor em unidades mínimas, como persistido em `BIGINT`.
func (m Money) MinorUnits() int64 { return m.minor }

// Currency devolve a moeda do valor.
func (m Money) Currency() Currency { return m.currency }

// Validate rejeita um valor não inicializado.
func (m Money) Validate() error {
	if m.currency.IsZero() {
		return ErrUninitialized.Messagef("valor monetário sem moeda")
	}
	return nil
}

// IsZero indica valor igual a zero.
func (m Money) IsZero() bool { return m.minor == 0 }

// IsPositive indica valor maior que zero.
func (m Money) IsPositive() bool { return m.minor > 0 }

// IsNegative indica valor menor que zero.
func (m Money) IsNegative() bool { return m.minor < 0 }

// Add soma dois valores da mesma moeda.
func (m Money) Add(other Money) (Money, error) {
	if err := m.requireCompatible(other); err != nil {
		return Money{}, err
	}
	sum, err := add(m.minor, other.minor)
	if err != nil {
		return Money{}, ErrOverflow.Messagef("soma de %s e %s excede a faixa representável", m, other)
	}
	return Money{minor: sum, currency: m.currency}, nil
}

// Sub subtrai dois valores da mesma moeda. O resultado pode ser negativo: cabe
// ao agregado decidir se isso é aceitável.
func (m Money) Sub(other Money) (Money, error) {
	if err := m.requireCompatible(other); err != nil {
		return Money{}, err
	}
	difference, err := subtract(m.minor, other.minor)
	if err != nil {
		return Money{}, ErrOverflow.Messagef("diferença entre %s e %s excede a faixa representável", m, other)
	}
	return Money{minor: difference, currency: m.currency}, nil
}

// Negate inverte o sinal do valor.
func (m Money) Negate() (Money, error) {
	if err := m.Validate(); err != nil {
		return Money{}, err
	}
	if m.minor == math.MinInt64 {
		return Money{}, ErrOverflow.Messagef("negação de %s excede a faixa representável", m)
	}
	return Money{minor: -m.minor, currency: m.currency}, nil
}

// Compare devolve -1, 0 ou 1 conforme o valor seja menor, igual ou maior.
func (m Money) Compare(other Money) (int, error) {
	if err := m.requireCompatible(other); err != nil {
		return 0, err
	}
	switch {
	case m.minor < other.minor:
		return -1, nil
	case m.minor > other.minor:
		return 1, nil
	default:
		return 0, nil
	}
}

// Equal indica igualdade de valor e moeda. Diferente de Compare, não devolve
// erro: valores de moedas distintas simplesmente não são iguais.
func (m Money) Equal(other Money) bool {
	return m.minor == other.minor && m.currency == other.currency
}

// String devolve o valor na forma canônica de duas casas decimais, sem a moeda.
func (m Money) String() string {
	quotient := m.minor / minorPerUnit
	remainder := m.minor % minorPerUnit

	var builder strings.Builder
	if m.minor < 0 {
		builder.WriteByte('-')
		quotient, remainder = -quotient, -remainder
	}
	builder.WriteString(strconv.FormatInt(quotient, 10))
	builder.WriteByte('.')
	if remainder < 10 {
		builder.WriteByte('0')
	}
	builder.WriteString(strconv.FormatInt(remainder, 10))
	return builder.String()
}

func (m Money) requireCompatible(other Money) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := other.Validate(); err != nil {
		return err
	}
	if m.currency != other.currency {
		return ErrCurrencyMismatch.Messagef(
			"operação entre %s e %s", m.currency, other.currency)
	}
	return nil
}

// JSON é a representação do contrato externo.
type JSON struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// MarshalJSON serializa como {"amount":"25.00","currency":"BRL"}.
func (m Money) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(JSON{Amount: m.String(), Currency: m.currency.String()})
}

// UnmarshalJSON aplica o mesmo parsing estrito da entrada externa.
func (m *Money) UnmarshalJSON(data []byte) error {
	var raw JSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return ErrInvalidAmount.Messagef("valor monetário malformado").WithCause(err)
	}
	parsed, err := Parse(raw.Amount, raw.Currency)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

func add(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, ErrOverflow
	}
	return a + b, nil
}

func subtract(a, b int64) (int64, error) {
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return 0, ErrOverflow
	}
	return a - b, nil
}

func multiply(a, factor int64) (int64, error) {
	if a == 0 {
		return 0, nil
	}
	if a > math.MaxInt64/factor || a < math.MinInt64/factor {
		return 0, ErrOverflow
	}
	return a * factor, nil
}
