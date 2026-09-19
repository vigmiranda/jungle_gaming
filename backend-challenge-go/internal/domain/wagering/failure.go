package wagering

// FailureCode é o código estável devolvido ao provedor em rejeições e falhas.
//
// Os códigos distinguem entrada corrigível de resultado definitivo e são parte
// do contrato externo: mudá-los quebra integrações.
type FailureCode string

// Códigos de rejeição por regra de negócio.
const (
	// FailureInsufficientFunds: aposta sem saldo suficiente.
	FailureInsufficientFunds FailureCode = "INSUFFICIENT_FUNDS"
	// FailureReversalExceedsBalance: reversão que deixaria o saldo negativo.
	// Distinto de INSUFFICIENT_FUNDS porque a causa e a ação corretiva diferem.
	FailureReversalExceedsBalance FailureCode = "REVERSAL_EXCEEDS_BALANCE"
	// FailureReferenceNotFound: referência não chegou dentro do TTL.
	FailureReferenceNotFound FailureCode = "REFERENCE_NOT_FOUND"
	// FailureReferenceNotProcessed: referência existe mas não é elegível.
	FailureReferenceNotProcessed FailureCode = "REFERENCE_NOT_PROCESSED"
	// FailureDuplicateReversal: segunda reversão do mesmo tipo sobre a referência.
	FailureDuplicateReversal FailureCode = "DUPLICATE_REVERSAL"
	// FailureReferenceMismatch: referência que não concorda em provedor,
	// jogador, carteira, moeda, rodada ou valor.
	FailureReferenceMismatch FailureCode = "REFERENCE_MISMATCH"
	// FailureInvalidAmount: valor fora da política do tipo.
	FailureInvalidAmount FailureCode = "INVALID_AMOUNT"
	// FailureCurrencyMismatch: moeda diferente da carteira.
	FailureCurrencyMismatch FailureCode = "CURRENCY_MISMATCH"
	// FailureWalletPlayerMismatch: carteira informada não pertence ao jogador.
	FailureWalletPlayerMismatch FailureCode = "WALLET_PLAYER_MISMATCH"
	// FailureOpeningNotAllowed: abertura solicitada por canal externo.
	FailureOpeningNotAllowed FailureCode = "OPENING_NOT_ALLOWED"
	// FailureInfrastructure: falha permanente de infraestrutura, registrada para
	// auditoria junto com o estado FAILED (ADR-016).
	FailureInfrastructure FailureCode = "INFRASTRUCTURE_FAILURE"
)

// String devolve a representação textual.
func (c FailureCode) String() string { return string(c) }
