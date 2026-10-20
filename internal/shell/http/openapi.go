package http

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.json
var openAPISpec []byte

// OpenAPIHandler serves the OpenAPI 3.0 specification (JSON).
// It is exposed without identity middleware so tools and docs can fetch the spec.
// Regenerate openapi.json after editing openapi.yaml: go run ./cmd/openapi-convert
func OpenAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(openAPISpec)
}
