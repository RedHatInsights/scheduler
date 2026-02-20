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
		Timezone string             `json:"timezone"` // Optional, defaults to UTC
		Type     domain.PayloadType `json:"type"`
		Payload  interface{}        `json:"payload"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[DEBUG] HTTP CreateJob failed - JSON decode error: %v", err)
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidJSON(err)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		log.Printf("[DEBUG] CreateJob failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	log.Printf("[DEBUG] HTTP CreateJob - parsed request: name=%s, org_id=%s, username=%s, user_id=%s, schedule=%s, timezone=%s, type=%s", req.Name, ident.Identity.OrgID, ident.Identity.User.Username, ident.Identity.User.UserID, req.Schedule, req.Timezone, req.Type)

	if req.Name == "" || req.Schedule == "" || req.Type == "" || req.Payload == nil {
		log.Printf("[DEBUG] HTTP CreateJob failed - missing required fields")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorMissingFields()})
		return
	}

	log.Printf("[DEBUG] HTTP CreateJob - calling job service with validated request")

	job, err := h.jobService.CreateJob(req.Name, ident.Identity.OrgID, ident.Identity.User.Username, ident.Identity.User.UserID, req.Schedule, req.Timezone, req.Type, req.Payload)
	if err != nil {
		if err == domain.ErrInvalidSchedule || err == domain.ErrInvalidPayload || err == domain.ErrInvalidOrgID || err == domain.ErrInvalidTimezone {
			log.Printf("[DEBUG] HTTP CreateJob failed - validation error: %v", err)
			respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorBadRequest()})
			return
		}
		log.Printf("[DEBUG] HTTP CreateJob failed - internal error: %v", err)
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	log.Printf("[DEBUG] HTTP CreateJob success - job created with ID: %s", job.ID)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/api/scheduler/v1/jobs/"+job.ID)
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
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	statusFilter := r.URL.Query().Get("status")
	nameFilter := r.URL.Query().Get("name")
	offset, limit := parsePaginationParams(r.URL)

	// Only get jobs for the current user
	jobs, total, err := h.jobService.GetJobsByUserID(ident.Identity.User.UserID, statusFilter, nameFilter, offset, limit)
	if err != nil {
		log.Printf("[DEBUG] GetAllJobs failed - unable to retrieve jobs")
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
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
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	// Only get job if it belongs to the current user
	job, err := h.jobService.GetJobWithUserCheck(id, ident.Identity.User.UserID)
	if err != nil {
		if err == domain.ErrJobNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", id)})
			return
		}
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
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
		log.Printf("[DEBUG] UpdateJob failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
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
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidJSON(err)})
		return
	}

	if req.Name == "" || req.Schedule == "" || req.Type == "" || req.Status == "" || req.Payload == nil {
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorMissingFields()})
		return
	}

	job, err := h.jobService.UpdateJob(id, req.Name, ident.Identity.OrgID, ident.Identity.User.Username, ident.Identity.User.UserID, req.Schedule, req.Type, req.Payload, req.Status)
	if err != nil {
		if err == domain.ErrJobNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", id)})
			return
		}
		if err == domain.ErrInvalidSchedule || err == domain.ErrInvalidPayload || err == domain.ErrInvalidStatus || err == domain.ErrInvalidOrgID {
			log.Printf("[DEBUG] HTTP UpdateJob failed - validation error: %v", err)
			respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorBadRequest()})
			return
		}
		log.Printf("[DEBUG] HTTP UpdateJob failed - internal error: %v", err)
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
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
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidJSON(err)})
		return
	}

	// Only patch job if it belongs to the current user
	job, err := h.jobService.PatchJobWithUserCheck(id, ident.Identity.User.UserID, updates)
	if err != nil {
		if err == domain.ErrJobNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", id)})
			return
		}
		if err == domain.ErrInvalidSchedule || err == domain.ErrInvalidPayload || err == domain.ErrInvalidStatus || err == domain.ErrInvalidOrgID {
			log.Printf("[DEBUG] HTTP PatchJob failed - validation error: %v", err)
			respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorBadRequest()})
			return
		}
		log.Printf("[DEBUG] HTTP PatchJob failed - internal error: %v", err)
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
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
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	// Only delete job if it belongs to the current user
	err := h.jobService.DeleteJobWithUserCheck(id, ident.Identity.User.UserID)
	if err != nil {
		if err == domain.ErrJobNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", id)})
			return
		}
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
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
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	// Only run job if it belongs to the current user
	err := h.jobService.RunJobWithUserCheck(id, ident.Identity.User.UserID)
	if err != nil {
		if err == domain.ErrJobNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", id)})
			return
		}
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
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
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	// Only pause job if it belongs to the current user
	job, err := h.jobService.PauseJobWithUserCheck(id, ident.Identity.User.UserID)
	if err != nil {
		if err == domain.ErrJobNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", id)})
			return
		}
		if err == domain.ErrJobAlreadyPaused {
			respondWithError(w, http.StatusBadRequest, "Job Already Paused", "The job is already in a paused state")
			return
		}
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
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
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	// Only resume job if it belongs to the current user
	job, err := h.jobService.ResumeJobWithUserCheck(id, ident.Identity.User.UserID)
	if err != nil {
		if err == domain.ErrJobNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", id)})
			return
		}
		if err == domain.ErrJobNotPaused {
			respondWithError(w, http.StatusBadRequest, "Job Not Paused", "The job is not in a paused state and cannot be resumed")
			return
		}
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
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
