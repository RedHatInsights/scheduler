package http

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/redhatinsights/platform-go-middlewares/identity"

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

	// Extract identity from middleware context
	ident := identity.Get(r.Context())
	if ident.Identity.OrgID == "" {
		http.Error(w, "Missing organization ID in identity", http.StatusBadRequest)
		return
	}

	log.Printf("[DEBUG] HTTP GetJobRuns called - job_id=%s, org_id=%s", jobID, ident.Identity.OrgID)

	// Only get runs for jobs belonging to the user's organization
	runs, err := h.jobRunService.GetJobRunsWithOrgCheck(jobID, ident.Identity.OrgID)
	if err != nil {
		if err == domain.ErrJobNotFound {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		log.Printf("[DEBUG] HTTP GetJobRuns failed - error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("[DEBUG] HTTP GetJobRuns success - found %d runs for job %s", len(runs), jobID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// GetJobRun retrieves a specific job run by ID
func (h *JobRunHandler) GetJobRun(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	runID := vars["run_id"]

	// Extract identity from middleware context
	ident := identity.Get(r.Context())
	if ident.Identity.OrgID == "" {
		http.Error(w, "Missing organization ID in identity", http.StatusBadRequest)
		return
	}

	log.Printf("[DEBUG] HTTP GetJobRun called - run_id=%s, org_id=%s", runID, ident.Identity.OrgID)

	// Only get run if it belongs to a job in the user's organization
	run, err := h.jobRunService.GetJobRunWithOrgCheck(runID, ident.Identity.OrgID)
	if err != nil {
		if err == domain.ErrJobRunNotFound {
			http.Error(w, "Job run not found", http.StatusNotFound)
			return
		}
		log.Printf("[DEBUG] HTTP GetJobRun failed - error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("[DEBUG] HTTP GetJobRun success - run_id=%s, status=%s", run.ID, run.Status)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}
