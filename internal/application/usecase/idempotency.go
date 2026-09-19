package usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// payloadHash calcula o hash determinístico dos campos de negócio.
//
// Algoritmo: SHA-256 sobre JSON canônico com chaves ordenadas, em hexadecimal
// minúsculo. A ordenação vem de graça ao serializar um `map[string]string`, que
// o `encoding/json` emite em ordem alfabética.
//
// Entram apenas os campos de negócio listados abaixo. Ficam de fora a chave de
// idempotência e qualquer metadado de transporte — cabeçalhos HTTP, envelope
// do SQS, instantes de recebimento —, de modo que a mesma operação enviada
// pelos dois canais produza o mesmo hash.
//
// Os valores entram exatamente como recebidos. Não há normalização: formas não
// canônicas como "25.0" são rejeitadas na validação, antes de chegar aqui
// (ADR-021), o que elimina a possibilidade de dois payloads equivalentes
// gerarem hashes diferentes.
func payloadHash(command ProcessTransactionCommand) string {
	fields := map[string]string{
		"providerId":                     command.ProviderID,
		"externalTransactionId":          command.ExternalTransactionID,
		"playerId":                       command.PlayerID,
		"walletId":                       command.WalletID,
		"roundId":                        command.RoundID,
		"gameId":                         command.GameID,
		"kind":                           command.Kind,
		"amount":                         command.Money.Amount,
		"currency":                       command.Money.Currency,
		"referenceExternalTransactionId": command.ReferenceExternalTransactionID,
	}

	// O map só contém strings, então a serialização não falha.
	canonical, _ := json.Marshal(fields)

	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}
