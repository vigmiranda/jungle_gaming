//go:build integration

package integration

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// assertConstraint confirma que o banco recusou a escrita pela constraint
// esperada. Comparar pelo nome evita que o teste passe por um motivo diferente
// do pretendido, como um NOT NULL acidental.
func assertConstraint(t *testing.T, err error, constraint string) {
	t.Helper()

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("esperava erro do PostgreSQL, obteve %T: %v", err, err)
	}
	if pgErr.ConstraintName != constraint {
		t.Errorf("constraint = %q, esperada %q (mensagem: %s)", pgErr.ConstraintName, constraint, pgErr.Message)
	}
}

// assertConstraintOneOf é usada quando um valor inválido viola mais de uma
// constraint ao mesmo tempo e o PostgreSQL reporta apenas a primeira avaliada.
// Um valor fora do domínio de `direction` ou de `origin`, por exemplo, quebra
// tanto o CHECK do próprio campo quanto a regra que depende dele.
func assertConstraintOneOf(t *testing.T, err error, constraints ...string) {
	t.Helper()

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("esperava erro do PostgreSQL, obteve %T: %v", err, err)
	}
	for _, candidate := range constraints {
		if pgErr.ConstraintName == candidate {
			return
		}
	}
	t.Errorf("constraint = %q, esperada uma de %s (mensagem: %s)",
		pgErr.ConstraintName, strings.Join(constraints, ", "), pgErr.Message)
}

// assertErrorCode confirma o SQLSTATE devolvido, usado onde a recusa vem de um
// gatilho e não de uma constraint nomeada.
func assertErrorCode(t *testing.T, err error, code, messageFragment string) {
	t.Helper()

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("esperava erro do PostgreSQL, obteve %T: %v", err, err)
	}
	if pgErr.Code != code {
		t.Errorf("SQLSTATE = %q, esperado %q (mensagem: %s)", pgErr.Code, code, pgErr.Message)
	}
	if messageFragment != "" && !strings.Contains(pgErr.Message, messageFragment) {
		t.Errorf("mensagem = %q, esperava conter %q", pgErr.Message, messageFragment)
	}
}
