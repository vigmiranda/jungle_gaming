package auth

import "go.uber.org/fx"

// Module registra o validador JWKS e o autenticador no grafo.
//
// O JWKS é buscado sob demanda na primeira validação (e renovado pelo
// intervalo configurado). Assim o processo sobe mesmo se o IdP ainda estiver
// aquecendo; a primeira chamada autenticada falha com 401 até as chaves
// ficarem disponíveis.
var Module = fx.Module("auth",
	fx.Provide(
		NewJWKSValidator,
		func(validator *JWKSValidator) Validator { return validator },
		NewAuthenticator,
	),
)
