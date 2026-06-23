package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/redhatinsights/platform-go-middlewares/v2/identity"

	"insights-scheduler/internal/core/domain"
	"insights-scheduler/internal/core/ports"
)

type JobHandler struct {
	jobService ports.AuthorizedJobService
}

func NewJobHandler(jobService ports.AuthorizedJobService) *JobHandler {
	return &JobHandler{
		jobService: jobService,
	}
}

func (h *JobHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	logger := GetLogger(r)
	logger.Debug("CreateJob called")

	var req struct {
		Name     string             `json:"name"`
		Schedule string             `json:"schedule"`
		Timezone string             `json:"timezone"` // Optional, defaults to UTC
		Type     domain.PayloadType `json:"type"`
		Payload  interface{}        `json:"payload"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Warn("Invalid JSON in request body", slog.Any("error", err))
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidJSON(err)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		logger.Warn("Invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	logger.Debug("CreateJob request parsed",
		slog.String("name", req.Name),
		slog.String("schedule", req.Schedule),
		slog.String("timezone", req.Timezone),
		slog.String("type", string(req.Type)),
	)

	if req.Name == "" || req.Schedule == "" || req.Type == "" || req.Payload == nil {
		logger.Warn("Missing required fields in request")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorMissingFields()})
		return
	}

	// Call service with identity - authorization is handled by the service
	job, err := h.jobService.CreateJob(r.Context(), ident, req.Name, req.Schedule, req.Timezone, req.Type, req.Payload)
	if err != nil {
		if err == domain.ErrInvalidSchedule || err == domain.ErrInvalidPayload || err == domain.ErrInvalidOrgID || err == domain.ErrInvalidTimezone {
			logger.Warn("Validation error creating job", slog.Any("error", err))
			respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorBadRequest()})
			return
		}
		logger.Error("Internal error creating job", slog.Any("error", err))
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	logger.Info("Job created successfully", slog.String("created_job_id", job.ID))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/api/scheduler/v1/jobs/"+job.ID)
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(ToJobResponse(job)); err != nil {
		logger.Error("Failed to encode response", slog.Any("error", err))
	}
}

func (h *JobHandler) GetAllJobs(w http.ResponseWriter, r *http.Request) {
	logger := GetLogger(r)

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		logger.Warn("GetAllJobs failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	statusFilter := r.URL.Query().Get("status")
	nameFilter := r.URL.Query().Get("name")
	offset, limit := parsePaginationParams(r.URL)

	// Service automatically filters by identity
	jobs, total, err := h.jobService.ListJobs(r.Context(), ident, statusFilter, nameFilter, offset, limit)
	if err != nil {
		logger.Error("Failed to retrieve jobs", slog.Any("error", err))
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	logger.Info("Retrieved jobs", slog.Int("count", len(jobs)), slog.Int("total", total))

	response := buildPaginatedResponse(r.URL, offset, limit, total, ToJobResponseList(jobs))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (h *JobHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	logger := GetLogger(r)
	vars := mux.Vars(r)
	id := vars["id"]

	// Validate UUID format
	if !validateUUID(id) {
		logger.Warn("Invalid job ID format", slog.String("id", id))
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidUUID("job ID", id)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		logger.Warn("GetJob failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	// Service handles authorization check
	job, err := h.jobService.GetJob(r.Context(), ident, id)
	if err != nil {
		if err == domain.ErrJobNotFound {
			logger.Info("Job not found")
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", id)})
			return
		}
		logger.Error("Failed to retrieve job", slog.Any("error", err))
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToJobResponse(job))
}

func (h *JobHandler) UpdateJob(w http.ResponseWriter, r *http.Request) {
	logger := GetLogger(r)
	vars := mux.Vars(r)
	id := vars["id"]

	// Validate UUID format
	if !validateUUID(id) {
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidUUID("job ID", id)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		logger.Warn("UpdateJob failed - invalid identity")
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

	// Service handles authorization check
	job, err := h.jobService.UpdateJob(r.Context(), ident, id, req.Name, req.Schedule, req.Type, req.Payload, req.Status)
	if err != nil {
		if err == domain.ErrJobNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", id)})
			return
		}
		if err == domain.ErrInvalidSchedule || err == domain.ErrInvalidPayload || err == domain.ErrInvalidStatus || err == domain.ErrInvalidStatusTransition || err == domain.ErrInvalidOrgID {
			logger.Warn("UpdateJob validation error", slog.Any("error", err))
			respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorBadRequest()})
			return
		}
		logger.Error("UpdateJob internal error", slog.Any("error", err))
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToJobResponse(job))
}

func (h *JobHandler) PatchJob(w http.ResponseWriter, r *http.Request) {
	logger := GetLogger(r)
	vars := mux.Vars(r)
	id := vars["id"]

	// Validate UUID format
	if !validateUUID(id) {
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidUUID("job ID", id)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		logger.Warn("PatchJob failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidJSON(err)})
		return
	}

	// Service handles authorization check
	job, err := h.jobService.PatchJob(r.Context(), ident, id, updates)
	if err != nil {
		if err == domain.ErrJobNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", id)})
			return
		}
		if err == domain.ErrInvalidSchedule || err == domain.ErrInvalidPayload || err == domain.ErrInvalidStatus || err == domain.ErrInvalidStatusTransition || err == domain.ErrInvalidOrgID {
			logger.Warn("PatchJob validation error", slog.Any("error", err))
			respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorBadRequest()})
			return
		}
		logger.Error("PatchJob internal error", slog.Any("error", err))
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToJobResponse(job))
}

func (h *JobHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Validate UUID format
	if !validateUUID(id) {
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidUUID("job ID", id)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		logger := GetLogger(r)
		logger.Warn("DeleteJob failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	// Service handles authorization check
	err := h.jobService.DeleteJob(r.Context(), ident, id)
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

	// Validate UUID format
	if !validateUUID(id) {
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidUUID("job ID", id)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		logger := GetLogger(r)
		logger.Warn("RunJob failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	// Service handles authorization check
	jobRunID, err := h.jobService.RunJob(r.Context(), ident, id)
	if err != nil {
		if err == domain.ErrJobNotFound {
			respondWithErrors(w, http.StatusNotFound, []ErrorObject{errorNotFound("job", id)})
			return
		}
		respondWithErrors(w, http.StatusInternalServerError, []ErrorObject{errorInternalServer()})
		return
	}

	// Return the job run ID in the response
	response := map[string]string{
		"run_id": jobRunID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(response)
}

func (h *JobHandler) PauseJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Validate UUID format
	if !validateUUID(id) {
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidUUID("job ID", id)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		logger := GetLogger(r)
		logger.Warn("PauseJob failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	// Service handles authorization check
	job, err := h.jobService.PauseJob(r.Context(), ident, id)
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

	// Validate UUID format
	if !validateUUID(id) {
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidUUID("job ID", id)})
		return
	}

	// Extract identity from middleware context
	ident := identity.Get(r.Context())

	if !isValidIdentity(ident) {
		logger := GetLogger(r)
		logger.Warn("ResumeJob failed - invalid identity")
		respondWithErrors(w, http.StatusBadRequest, []ErrorObject{errorInvalidIdentity()})
		return
	}

	// Service handles authorization check
	job, err := h.jobService.ResumeJob(r.Context(), ident, id)
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
