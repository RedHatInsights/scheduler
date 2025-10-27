package http

import (
	"github.com/gorilla/mux"
	"github.com/redhatinsights/platform-go-middlewares/identity"

	"insights-scheduler-part-2/internal/core/usecases"
)

func SetupRoutes(jobService *usecases.JobService) *mux.Router {
	router := mux.NewRouter()
	handler := NewJobHandler(jobService)

	// Apply identity middleware to all API routes
	api := router.PathPrefix("/api/v1").Subrouter()
	api.Use(identity.EnforceIdentity)

	// Job CRUD operations
	api.HandleFunc("/jobs", handler.CreateJob).Methods("POST")
	api.HandleFunc("/jobs", handler.GetAllJobs).Methods("GET")
	api.HandleFunc("/jobs/{id}", handler.GetJob).Methods("GET")
	api.HandleFunc("/jobs/{id}", handler.UpdateJob).Methods("PUT")
	api.HandleFunc("/jobs/{id}", handler.PatchJob).Methods("PATCH")
	api.HandleFunc("/jobs/{id}", handler.DeleteJob).Methods("DELETE")

	// Job control operations
	api.HandleFunc("/jobs/{id}/run", handler.RunJob).Methods("POST")
	api.HandleFunc("/jobs/{id}/pause", handler.PauseJob).Methods("POST")
	api.HandleFunc("/jobs/{id}/resume", handler.ResumeJob).Methods("POST")

	return router
}
