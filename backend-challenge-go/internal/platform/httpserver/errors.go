package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/vigmi/backend-challenge-go/internal/application/port"
	"github.com/vigmi/backend-challenge-go/internal/domain/shared"
	"github.com/vigmi/backend-challenge-go/internal/platform/auth"
)

type errorBody struct {
	Error       string `json:"error"`
	Message     string `json:"message"`
	FailureCode string `json:"failureCode,omitempty"`
}

func writeError(w http.ResponseWriter, err error) {
	status, body := mapError(err)
	writeJSON(w, status, body)
}

func mapError(err error) (int, errorBody) {
	if errors.Is(err, auth.ErrUnauthenticated) {
		return http.StatusUnauthorized, errorBody{Error: "unauthorized", Message: "token ausente ou inválido"}
	}
	if errors.Is(err, auth.ErrForbidden) {
		return http.StatusForbidden, errorBody{Error: "forbidden", Message: "acesso negado"}
	}
	if errors.Is(err, port.ErrNotFound) {
		return http.StatusNotFound, errorBody{Error: "not_found", Message: err.Error()}
	}

	var domainErr *shared.Error
	if errors.As(err, &domainErr) {
		switch domainErr.Kind {
		case shared.KindValidation:
			return http.StatusBadRequest, errorBody{
				Error:   strings.ToLower(domainErr.Code),
				Message: domainErr.Message,
			}
		case shared.KindConflict:
			return http.StatusConflict, errorBody{
				Error:   strings.ToLower(domainErr.Code),
				Message: domainErr.Message,
			}
		case shared.KindRejection:
			return http.StatusUnprocessableEntity, errorBody{
				Error:       "business_rejection",
				Message:     domainErr.Message,
				FailureCode: domainErr.Code,
			}
		case shared.KindInvariant:
			return http.StatusInternalServerError, errorBody{
				Error:   "internal_error",
				Message: "erro interno ao processar a requisição",
			}
		}
	}

	return http.StatusServiceUnavailable, errorBody{
		Error:   "service_unavailable",
		Message: "dependência indisponível",
	}
}

func decodeJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return shared.Validation("INVALID_JSON", "corpo JSON inválido").WithCause(err)
	}
	return nil
}
