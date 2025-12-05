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

type JobHandler struct {
	jobService *usecases.JobService
}

func NewJobHandler(jobService *usecases.JobService) *JobHandler {
	return &JobHandler{
		jobService: jobService,
	}
}

func (h *JobHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	log.Printf("[DEBUG] HTTP CreateJob called - method: %s, path: %s", r.Method, r.URL.Path)

	var req struct {
		Name     string             `json:"name"`
		Schedule string             `json:"schedule"`
		Type     domain.PayloadType `json:"type"`
		Payload  interface{}        `json:"payload"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[DEBUG] HTTP CreateJob failed - JSON decode error: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())
	if ident.Identity.OrgID == "" {
		log.Printf("[DEBUG] HTTP CreateJob failed - missing org_id in identity")
		http.Error(w, "Missing organization ID in identity", http.StatusBadRequest)
		return
	}

	username := ident.Identity.User.Username
	if username == "" {
		// Fallback to email if username is not available
		username = ident.Identity.User.Email
		if username == "" {
			log.Printf("[DEBUG] HTTP CreateJob failed - missing username/email in identity")
			http.Error(w, "Missing username in identity", http.StatusBadRequest)
			return
		}
	}

	userID := ident.Identity.User.UserID
	if userID == "" {
		log.Printf("[DEBUG] HTTP CreateJob failed - missing user_id in identity")
		http.Error(w, "Missing user ID in identity", http.StatusBadRequest)
		return
	}

	log.Printf("[DEBUG] HTTP CreateJob - parsed request: name=%s, org_id=%s, username=%s, user_id=%s, schedule=%s, type=%s", req.Name, ident.Identity.OrgID, username, userID, req.Schedule, req.Type)

	if req.Name == "" || req.Schedule == "" || req.Type == "" {
		log.Printf("[DEBUG] HTTP CreateJob failed - missing required fields: name=%s, schedule=%s, type=%s", req.Name, req.Schedule, req.Type)
		http.Error(w, "Missing required fields (name, schedule, type)", http.StatusBadRequest)
		return
	}

	if req.Payload == nil {
		log.Printf("[DEBUG] HTTP CreateJob failed - payload is required")
		http.Error(w, "Missing required field: payload", http.StatusBadRequest)
		return
	}

	log.Printf("[DEBUG] HTTP CreateJob - calling job service with validated request")

	job, err := h.jobService.CreateJob(req.Name, ident.Identity.OrgID, username, userID, req.Schedule, req.Type, req.Payload)
	if err != nil {
		if err == domain.ErrInvalidSchedule || err == domain.ErrInvalidPayload || err == domain.ErrInvalidOrgID {
			log.Printf("[DEBUG] HTTP CreateJob failed - validation error: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		log.Printf("[DEBUG] HTTP CreateJob failed - internal error: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	log.Printf("[DEBUG] HTTP CreateJob success - job created with ID: %s", job.ID)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/api/v1/jobs/"+job.ID)
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(ToJobResponse(job)); err != nil {
		log.Printf("[DEBUG] HTTP CreateJob - warning: failed to encode response: %v", err)
	} else {
		log.Printf("[DEBUG] HTTP CreateJob - response sent successfully")
	}
}

func (h *JobHandler) GetAllJobs(w http.ResponseWriter, r *http.Request) {
	// Extract identity from middleware context
	ident := identity.Get(r.Context())
	if ident.Identity.OrgID == "" {
		http.Error(w, "Missing organization ID in identity", http.StatusBadRequest)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	nameFilter := r.URL.Query().Get("name")

	// Only get jobs for the user's organization
	jobs, err := h.jobService.GetJobsByOrgID(ident.Identity.OrgID, statusFilter, nameFilter)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToJobResponseList(jobs))
}

func (h *JobHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Extract identity from middleware context
	ident := identity.Get(r.Context())
	if ident.Identity.OrgID == "" {
		http.Error(w, "Missing organization ID in identity", http.StatusBadRequest)
		return
	}

	// Only get job if it belongs to the user's organization
	job, err := h.jobService.GetJobWithOrgCheck(id, ident.Identity.OrgID)
	if err != nil {
		if err == domain.ErrJobNotFound {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToJobResponse(job))
}

func (h *JobHandler) UpdateJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Extract identity from middleware context
	ident := identity.Get(r.Context())
	if ident.Identity.OrgID == "" {
		http.Error(w, "Missing organization ID in identity", http.StatusBadRequest)
		return
	}

	username := ident.Identity.User.Username
	if username == "" {
		// Fallback to email if username is not available
		username = ident.Identity.User.Email
		if username == "" {
			http.Error(w, "Missing username in identity", http.StatusBadRequest)
			return
		}
	}

	userID := ident.Identity.User.UserID
	if userID == "" {
		http.Error(w, "Missing user ID in identity", http.StatusBadRequest)
		return
	}

	var req struct {
		Name     string             `json:"name"`
		Schedule string             `json:"schedule"`
		Type     domain.PayloadType `json:"type"`
		Payload  interface{}        `json:"payload"`
		Status   string             `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Schedule == "" || req.Type == "" || req.Status == "" {
		http.Error(w, "Missing required fields (name, schedule, type, status)", http.StatusBadRequest)
		return
	}

	if req.Payload == nil {
		http.Error(w, "Missing required field: payload", http.StatusBadRequest)
		return
	}

	job, err := h.jobService.UpdateJob(id, req.Name, ident.Identity.OrgID, username, userID, req.Schedule, req.Type, req.Payload, req.Status)
	if err != nil {
		if err == domain.ErrJobNotFound {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		if err == domain.ErrInvalidSchedule || err == domain.ErrInvalidPayload || err == domain.ErrInvalidStatus || err == domain.ErrInvalidOrgID {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToJobResponse(job))
}

func (h *JobHandler) PatchJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Extract identity from middleware context
	ident := identity.Get(r.Context())
	if ident.Identity.OrgID == "" {
		http.Error(w, "Missing organization ID in identity", http.StatusBadRequest)
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Only patch job if it belongs to the user's organization
	job, err := h.jobService.PatchJobWithOrgCheck(id, ident.Identity.OrgID, updates)
	if err != nil {
		if err == domain.ErrJobNotFound {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		if err == domain.ErrInvalidSchedule || err == domain.ErrInvalidPayload || err == domain.ErrInvalidStatus || err == domain.ErrInvalidOrgID {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToJobResponse(job))
}

func (h *JobHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Extract identity from middleware context
	ident := identity.Get(r.Context())
	if ident.Identity.OrgID == "" {
		http.Error(w, "Missing organization ID in identity", http.StatusBadRequest)
		return
	}

	// Only delete job if it belongs to the user's organization
	err := h.jobService.DeleteJobWithOrgCheck(id, ident.Identity.OrgID)
	if err != nil {
		if err == domain.ErrJobNotFound {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *JobHandler) RunJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Extract identity from middleware context
	ident := identity.Get(r.Context())
	if ident.Identity.OrgID == "" {
		http.Error(w, "Missing organization ID in identity", http.StatusBadRequest)
		return
	}

	// Only run job if it belongs to the user's organization
	err := h.jobService.RunJobWithOrgCheck(id, ident.Identity.OrgID)
	if err != nil {
		if err == domain.ErrJobNotFound {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *JobHandler) PauseJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Extract identity from middleware context
	ident := identity.Get(r.Context())
	if ident.Identity.OrgID == "" {
		http.Error(w, "Missing organization ID in identity", http.StatusBadRequest)
		return
	}

	// Only pause job if it belongs to the user's organization
	job, err := h.jobService.PauseJobWithOrgCheck(id, ident.Identity.OrgID)
	if err != nil {
		if err == domain.ErrJobNotFound {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		if err == domain.ErrJobAlreadyPaused {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToJobResponse(job))
}

func (h *JobHandler) ResumeJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Extract identity from middleware context
	ident := identity.Get(r.Context())
	if ident.Identity.OrgID == "" {
		http.Error(w, "Missing organization ID in identity", http.StatusBadRequest)
		return
	}

	// Only resume job if it belongs to the user's organization
	job, err := h.jobService.ResumeJobWithOrgCheck(id, ident.Identity.OrgID)
	if err != nil {
		if err == domain.ErrJobNotFound {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		if err == domain.ErrJobNotPaused {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToJobResponse(job))
}
