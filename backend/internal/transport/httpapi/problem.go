package httpapi

import (
	"encoding/json"
	"net/http"
)

const problemContentType = "application/problem+json"

// Problem is an RFC 9457 problem details object.
//
// Title is a fixed English summary for logs and debugging.
// Clients translate Code, and use Params to fill the translated message.
type Problem struct {
	Type   string         `json:"type"`
	Title  string         `json:"title"`
	Status int            `json:"status"`
	Code   string         `json:"code"`
	Params map[string]any `json:"params,omitempty"`
}

// writeProblem writes an RFC 9457 problem document with the given status and stable code.
func writeProblem(w http.ResponseWriter, status int, code string, params map[string]any) {
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Code:   code,
		Params: params,
	})
}
