package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/redhatinsights/platform-go-middlewares/v2/identity"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/usecases"
)

type JobRunHandler struct {
	jobRunService *usecases.JobRunService
}

func NewJobRunHandler(jobRunService *usecases.JobRunService) *JobRunHandler {
	return &JobRunHandler{
		jobRunService: jobRunService,
	}
}

// GetJobRuns retrieves all runs for a specific job
func (h *JobRunHandler) GetJobRuns(w http.ResponseWriter, r *http.Request) {
	logger := GetLogger(r)
	vars := mux.Vars(r)
	jobID := vars["id"]

	// Validate UUID format
	if !validateUUID(jobID) {
		logger.Warn("Invalid job ID format", slog.String("id", jobID))
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidUUID("job ID", jobID)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		logger.Warn("GetJobRuns failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	logger.Debug("GetJobRuns called")

	offset, limit := parsePaginationParams(r.URL)

	// Only get runs for jobs belonging to the user
	runs, total, err := h.jobRunService.GetJobRunsWithUserCheck(jobID, ident, offset, limit)
	if err != nil {
		if err == domain.ErrJobNotFound {
			logger.Info("Job not found")
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", jobID)})
			return
		}
		logger.Error("Failed to retrieve job runs", slog.Any("error", err))
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	logger.Info("Job runs retrieved", slog.Int("count", len(runs)), slog.Int("total", total))

	response := buildPaginatedResponse(r.URL, offset, limit, total, ToJobRunResponseList(runs))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetJobRun retrieves a specific job run by ID
func (h *JobRunHandler) GetJobRun(w http.ResponseWriter, r *http.Request) {
	logger := GetLogger(r)
	vars := mux.Vars(r)
	runID := vars["run_id"]

	// Validate UUID format
	if !validateUUID(runID) {
		logger.Warn("Invalid run ID format", slog.String("id", runID))
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidUUID("run ID", runID)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		logger.Warn("GetJobRun failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	logger.Debug("GetJobRun called")

	// Only get run if it belongs to a job owned by the user
	run, err := h.jobRunService.GetJobRunWithUserCheck(runID, ident)
	if err != nil {
		if err == domain.ErrJobRunNotFound {
			logger.Info("Job run not found")
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job run", runID)})
			return
		}
		logger.Error("Failed to retrieve job run", slog.Any("error", err))
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	logger.Info("Job run retrieved", slog.String("status", string(run.Status)))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToJobRunResponse(run))
}

// GetAllRuns retrieves all job runs for the authenticated user
func (h *JobRunHandler) GetAllRuns(w http.ResponseWriter, r *http.Request) {
	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] GetAllRuns failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	log.Printf("[DEBUG] HTTP GetAllRuns called - user_id=%s", ident.Identity.User.UserID)

	offset, limit := parsePaginationParams(r.URL)

	// Get all runs for the authenticated user
	runs, total, err := h.jobRunService.GetAllRunsForUser(ident, offset, limit)
	if err != nil {
		log.Printf("[DEBUG] HTTP GetAllRuns failed - error: %v", err)
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	log.Printf("[DEBUG] HTTP GetAllRuns success - found %d runs for user %s", len(runs), ident.Identity.User.UserID)

	response := buildPaginatedResponse(r.URL, offset, limit, total, ToJobRunResponseList(runs))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
