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

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] CreateJob failed - invalid identity")
		http.Error(w, "Invalid identity", http.StatusBadRequest)
		return
	}

	log.Printf("[DEBUG] HTTP CreateJob - parsed request: name=%s, org_id=%s, username=%s, user_id=%s, schedule=%s, type=%s", req.Name, ident.Identity.OrgID, ident.Identity.User.Username, ident.Identity.User.UserID, req.Schedule, req.Type)

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

	job, err := h.jobService.CreateJob(req.Name, ident.Identity.OrgID, ident.Identity.User.Username, ident.Identity.User.UserID, req.Schedule, req.Type, req.Payload)
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

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] GetAllJobs failed - invalid identity")
		http.Error(w, "Invalid identity", http.StatusBadRequest)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	nameFilter := r.URL.Query().Get("name")
	offset, limit := parsePaginationParams(r.URL)

	// Only get jobs for the current user
	jobs, total, err := h.jobService.GetJobsByUserID(ident.Identity.User.UserID, statusFilter, nameFilter, offset, limit)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := buildPaginatedResponse(r.URL, offset, limit, total, ToJobResponseList(jobs))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *JobHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] GetJob failed - invalid identity")
		http.Error(w, "Invalid identity", http.StatusBadRequest)
		return
	}

	// Only get job if it belongs to the current user
	job, err := h.jobService.GetJobWithUserCheck(id, ident.Identity.User.UserID)
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

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] GetJob failed - invalid identity")
		http.Error(w, "Invalid identity", http.StatusBadRequest)
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

	job, err := h.jobService.UpdateJob(id, req.Name, ident.Identity.OrgID, ident.Identity.User.Username, ident.Identity.User.UserID, req.Schedule, req.Type, req.Payload, req.Status)
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

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] PatchJob failed - invalid identity")
		http.Error(w, "Invalid identity", http.StatusBadRequest)
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Only patch job if it belongs to the current user
	job, err := h.jobService.PatchJobWithUserCheck(id, ident.Identity.User.UserID, updates)
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

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] DeleteJob failed - invalid identity")
		http.Error(w, "Invalid identity", http.StatusBadRequest)
		return
	}

	// Only delete job if it belongs to the current user
	err := h.jobService.DeleteJobWithUserCheck(id, ident.Identity.User.UserID)
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

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] RunJob failed - invalid identity")
		http.Error(w, "Invalid identity", http.StatusBadRequest)
		return
	}

	// Only run job if it belongs to the current user
	err := h.jobService.RunJobWithUserCheck(id, ident.Identity.User.UserID)
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

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] PauseJob failed - invalid identity")
		http.Error(w, "Invalid identity", http.StatusBadRequest)
		return
	}

	// Only pause job if it belongs to the current user
	job, err := h.jobService.PauseJobWithUserCheck(id, ident.Identity.User.UserID)
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

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] ResumeJob failed - invalid identity")
		http.Error(w, "Invalid identity", http.StatusBadRequest)
		return
	}

	// Only resume job if it belongs to the current user
	job, err := h.jobService.ResumeJobWithUserCheck(id, ident.Identity.User.UserID)
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

func isValidIdentity(ident identity.XRHID) bool {
	if ident.Identity.User == nil || ident.Identity.User.Username == "" || ident.Identity.User.UserID == "" {
		return false
	}

	return true
}
