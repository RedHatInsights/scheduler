package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/redhatinsights/platform-go-middlewares/v2/identity"

	"insights-scheduler/internal/core/domain"
)

func testSortIdentity() identity.XRHID {
	return identity.XRHID{
		Identity: identity.Identity{
			OrgID: "org-123",
			User: &identity.User{
				Username: "testuser",
				UserID:   "user-123",
			},
		},
	}
}

// TestGetAllJobs_SortBy_ForwardsSpec verifies that a valid sort_by parameter is
// parsed and forwarded to the service as a domain.SortSpec.
func TestGetAllJobs_SortBy_ForwardsSpec(t *testing.T) {
	capturing := &sortCapturingJobService{}
	handler := NewJobHandler(capturing)

	req := httptest.NewRequest("GET", "/api/scheduler/v1/jobs?sort_by=name:asc", nil)
	req = req.WithContext(identity.WithIdentity(req.Context(), testSortIdentity()))

	rr := httptest.NewRecorder()
	handler.GetAllJobs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if capturing.sort != (domain.SortSpec{Field: "name", Direction: domain.SortAsc}) {
		t.Fatalf("unexpected sort forwarded: %+v", capturing.sort)
	}
}

// TestGetAllJobs_SortBy_DefaultsWhenAbsent verifies the historical default sort
// is applied when sort_by is omitted.
func TestGetAllJobs_SortBy_DefaultsWhenAbsent(t *testing.T) {
	capturing := &sortCapturingJobService{}
	handler := NewJobHandler(capturing)

	req := httptest.NewRequest("GET", "/api/scheduler/v1/jobs", nil)
	req = req.WithContext(identity.WithIdentity(req.Context(), testSortIdentity()))

	rr := httptest.NewRecorder()
	handler.GetAllJobs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if capturing.sort != domain.DefaultJobSort {
		t.Fatalf("expected default sort %+v, got %+v", domain.DefaultJobSort, capturing.sort)
	}
}

// TestGetAllJobs_SortBy_InvalidReturns400 verifies bad sort input is rejected
// before touching the service.
func TestGetAllJobs_SortBy_InvalidReturns400(t *testing.T) {
	cases := []struct {
		name   string
		sortBy string
	}{
		{"unknown field", "org_id:asc"},
		{"invalid direction", "name:sideways"},
		{"injection attempt", "name);DROP TABLE jobs--"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capturing := &sortCapturingJobService{}
			handler := NewJobHandler(capturing)

			req := httptest.NewRequest("GET", "/api/scheduler/v1/jobs?sort_by="+url.QueryEscape(tc.sortBy), nil)
			req = req.WithContext(identity.WithIdentity(req.Context(), testSortIdentity()))

			rr := httptest.NewRecorder()
			handler.GetAllJobs(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (body: %s)", rr.Code, rr.Body.String())
			}
			if capturing.called {
				t.Fatal("service must not be called when sort is invalid")
			}

			var resp ErrorResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to parse error response: %v", err)
			}
			if len(resp.Errors) == 0 || resp.Errors[0].Title != "Invalid Sort Parameter" {
				t.Fatalf("expected Invalid Sort Parameter error, got %+v", resp.Errors)
			}
		})
	}
}

// TestGetAllJobs_Filter_ForwardsSpec verifies status/name filters are parsed into
// a JobFilter and forwarded to the service.
func TestGetAllJobs_Filter_ForwardsSpec(t *testing.T) {
	capturing := &sortCapturingJobService{}
	handler := NewJobHandler(capturing)

	req := httptest.NewRequest("GET", "/api/scheduler/v1/jobs?status=paused&name=report", nil)
	req = req.WithContext(identity.WithIdentity(req.Context(), testSortIdentity()))

	rr := httptest.NewRecorder()
	handler.GetAllJobs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	want := domain.JobFilter{Status: "paused", NameContains: "report"}
	if capturing.filter != want {
		t.Fatalf("unexpected filter forwarded: got %+v, want %+v", capturing.filter, want)
	}
}

// TestGetAllJobs_Filter_InvalidStatusReturns400 verifies an unknown status value
// is rejected before the service is called.
func TestGetAllJobs_Filter_InvalidStatusReturns400(t *testing.T) {
	capturing := &sortCapturingJobService{}
	handler := NewJobHandler(capturing)

	req := httptest.NewRequest("GET", "/api/scheduler/v1/jobs?status=bogus", nil)
	req = req.WithContext(identity.WithIdentity(req.Context(), testSortIdentity()))

	rr := httptest.NewRecorder()
	handler.GetAllJobs(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	if capturing.called {
		t.Fatal("service must not be called when the status filter is invalid")
	}

	var resp ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if len(resp.Errors) == 0 || resp.Errors[0].Title != "Invalid Status Filter" {
		t.Fatalf("expected Invalid Status Filter error, got %+v", resp.Errors)
	}
}

// sortCapturingJobService is a ports.AuthorizedJobService that records the sort
// spec and filter passed to ListJobs. Only ListJobs is exercised by these tests;
// the rest panic to catch accidental use.
type sortCapturingJobService struct {
	sort   domain.SortSpec
	filter domain.JobFilter
	called bool
}

func (s *sortCapturingJobService) ListJobs(ctx context.Context, ident identity.XRHID, filter domain.JobFilter, sort domain.SortSpec, offset, limit int) ([]domain.Job, int, error) {
	s.sort = sort
	s.filter = filter
	s.called = true
	return []domain.Job{}, 0, nil
}

func (s *sortCapturingJobService) CreateJob(ctx context.Context, ident identity.XRHID, name, schedule, timezone string, payloadType domain.PayloadType, payload interface{}) (domain.Job, error) {
	panic("not implemented")
}
func (s *sortCapturingJobService) GetJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
	panic("not implemented")
}
func (s *sortCapturingJobService) UpdateJob(ctx context.Context, ident identity.XRHID, id, name, schedule string, payloadType domain.PayloadType, payload interface{}, status string) (domain.Job, error) {
	panic("not implemented")
}
func (s *sortCapturingJobService) PatchJob(ctx context.Context, ident identity.XRHID, id string, updates map[string]interface{}) (domain.Job, error) {
	panic("not implemented")
}
func (s *sortCapturingJobService) DeleteJob(ctx context.Context, ident identity.XRHID, id string) error {
	panic("not implemented")
}
func (s *sortCapturingJobService) RunJob(ctx context.Context, ident identity.XRHID, id string) (string, error) {
	panic("not implemented")
}
func (s *sortCapturingJobService) PauseJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
	panic("not implemented")
}
func (s *sortCapturingJobService) ResumeJob(ctx context.Context, ident identity.XRHID, id string) (domain.Job, error) {
	panic("not implemented")
}
