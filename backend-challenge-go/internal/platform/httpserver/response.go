package httpserver

import (
	"encoding/json"
	"net/http"
)

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// O corpo já está montado em memória; um erro aqui significa cliente
	// desconectado, situação em que não há resposta alternativa a enviar.
	_ = json.NewEncoder(w).Encode(body)
}
