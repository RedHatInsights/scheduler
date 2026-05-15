package http

import (
	"encoding/json"
	"log"
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
	vars := mux.Vars(r)
	jobID := vars["id"]

	// Validate UUID format
	if !validateUUID(jobID) {
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidUUID("job ID", jobID)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] GetJobRuns failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	log.Printf("[DEBUG] HTTP GetJobRuns called - job_id=%s, user_id=%s", jobID, ident.Identity.User.UserID)

	offset, limit := parsePaginationParams(r.URL)

	// Only get runs for jobs belonging to the user
	runs, total, err := h.jobRunService.GetJobRunsWithUserCheck(jobID, ident, offset, limit)
	if err != nil {
		if err == domain.ErrJobNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", jobID)})
			return
		}
		log.Printf("[DEBUG] HTTP GetJobRuns failed - error: %v", err)
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	log.Printf("[DEBUG] HTTP GetJobRuns success - found %d runs for job %s", len(runs), jobID)

	response := buildPaginatedResponse(r.URL, offset, limit, total, runs)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetJobRun retrieves a specific job run by ID
func (h *JobRunHandler) GetJobRun(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	runID := vars["run_id"]

	// Validate UUID format
	if !validateUUID(runID) {
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidUUID("run ID", runID)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] GetJobRuns failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	log.Printf("[DEBUG] HTTP GetJobRun called - run_id=%s, user_id=%s", runID, ident.Identity.User.UserID)

	// Only get run if it belongs to a job owned by the user
	run, err := h.jobRunService.GetJobRunWithUserCheck(runID, ident)
	if err != nil {
		if err == domain.ErrJobRunNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job run", runID)})
			return
		}
		log.Printf("[DEBUG] HTTP GetJobRun failed - error: %v", err)
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	log.Printf("[DEBUG] HTTP GetJobRun success - run_id=%s, status=%s", run.ID, run.Status)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}
