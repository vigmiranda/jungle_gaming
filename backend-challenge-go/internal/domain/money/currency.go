package money

// currencyCodeLength é o tamanho de um código ISO 4217 alfabético.
const currencyCodeLength = 3

// Currency é um código de moeda ISO 4217.
//
// A validação é de forma (três letras maiúsculas ASCII), não de pertencimento à
// lista oficial: manter a lista atualizada não agrega ao desafio e seria uma
// fonte de falso negativo.
type Currency string

// BRL é a moeda usada nos cenários principais.
const BRL Currency = "BRL"

// ParseCurrency valida e converte um código de moeda.
//
// A política é estrita: códigos em minúsculas são rejeitados, não normalizados,
// para que o hash de idempotência seja idêntico em qualquer canal de entrada.
func ParseCurrency(raw string) (Currency, error) {
	if raw == "" {
		return "", ErrInvalidCurrency.Messagef("código de moeda vazio")
	}
	if len(raw) != currencyCodeLength {
		return "", ErrInvalidCurrency.Messagef(
			"código de moeda deve ter %d letras maiúsculas, recebido %q", currencyCodeLength, raw)
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < 'A' || raw[i] > 'Z' {
			return "", ErrInvalidCurrency.Messagef(
				"código de moeda deve conter apenas letras maiúsculas ASCII, recebido %q", raw)
		}
	}
	return Currency(raw), nil
}

// String devolve o código da moeda.
func (c Currency) String() string { return string(c) }

// IsZero indica uma moeda não inicializada.
func (c Currency) IsZero() bool { return c == "" }

// Validate rejeita a moeda não inicializada.
func (c Currency) Validate() error {
	if c.IsZero() {
		return ErrUninitialized.Messagef("moeda não inicializada")
	}
	return nil
}
