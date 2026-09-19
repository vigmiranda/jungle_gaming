// Package migrations embarca os arquivos SQL versionados no binário.
//
// Embarcar evita que a aplicação dependa de arquivos ao lado do executável:
// a imagem distroless carrega apenas o binário, e as migrations vão junto.
package migrations

import "embed"

// FS contém os arquivos `NNNNNN_nome.{up,down}.sql`.
//
//go:embed *.sql
var FS embed.FS
