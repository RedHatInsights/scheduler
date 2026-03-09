package http

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openAPISpec []byte

// OpenAPIHandler serves the OpenAPI 3.0 specification (YAML).
// It is exposed without identity middleware so tools and docs can fetch the spec.
func OpenAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-yaml")
	w.WriteHeader(http.StatusOK)
	w.Write(openAPISpec)
}
