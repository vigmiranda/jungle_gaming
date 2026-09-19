package config

import "go.uber.org/fx"

// Module expõe a configuração validada ao grafo de dependências.
//
// Load devolve erro quando o ambiente é inválido, o que faz o Fx abortar o
// start antes de abrir qualquer conexão.
var Module = fx.Module("config", fx.Provide(Load))
