package http

import (
	"encoding/json"
	"net/http"
)

// ErrorObject represents a simplified JSON:API error object
type ErrorObject struct {
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// ErrorResponse is the top-level JSON:API error response
type ErrorResponse struct {
	Errors []ErrorObject `json:"errors"`
}

// respondWithError sends a single JSON:API error response
func respondWithError(w http.ResponseWriter, statusCode int, title, detail string) {
	respondWithErrors(w, statusCode, []ErrorObject{
		{
			Status: http.StatusText(statusCode),
			Title:  title,
			Detail: detail,
		},
	})
}

// respondWithErrors sends multiple JSON:API errors
func respondWithErrors(w http.ResponseWriter, statusCode int, errors []ErrorObject) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	// Ensure status is set for all errors
	for i := range errors {
		if errors[i].Status == "" {
			errors[i].Status = http.StatusText(statusCode)
		}
	}

	response := ErrorResponse{
		Errors: errors,
	}

	json.NewEncoder(w).Encode(response)
}

// Common error constructors for convenience

func errorNotFound(resourceType, id string) ErrorObject {
	return ErrorObject{
		Status: "404",
		Title:  resourceType + " Not Found",
		Detail: "The " + resourceType + " with ID '" + id + "' could not be found",
	}
}

func errorInvalidIdentity() ErrorObject {
	return ErrorObject{
		Status: "400",
		Title:  "Invalid Identity",
		Detail: "The X-Rh-Identity header is missing or contains invalid data",
	}
}

func errorInvalidJSON(err error) ErrorObject {
	detail := "The request body contains invalid JSON"
	if err != nil {
		detail = "Invalid JSON: " + err.Error()
	}
	return ErrorObject{
		Status: "400",
		Title:  "Invalid JSON",
		Detail: detail,
	}
}

func errorMissingFields() ErrorObject {
	return ErrorObject{
		Status: "400",
		Title:  "Missing Required Fields",
		Detail: "Missing Required Fields",
	}
}

func errorInvalidField(field, reason string) ErrorObject {
	return ErrorObject{
		Status: "400",
		Title:  "Invalid Field",
		Detail: "The field '" + field + "' is invalid: " + reason,
	}
}

func errorInternalServer() ErrorObject {
	return ErrorObject{
		Status: "500",
		Title:  "Internal Server Error",
		Detail: "An unexpected error occurred while processing your request",
	}
}
