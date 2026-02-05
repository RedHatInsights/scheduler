package http

import (
	"net/url"
	"testing"
)

func TestParsePaginationParams(t *testing.T) {
	tests := []struct {
		name           string
		queryParams    map[string]string
		expectedOffset int
		expectedLimit  int
	}{
		{
			name:           "No parameters - should use defaults",
			queryParams:    map[string]string{},
			expectedOffset: 0,
			expectedLimit:  20,
		},
		{
			name:           "Valid offset and limit",
			queryParams:    map[string]string{"offset": "10", "limit": "50"},
			expectedOffset: 10,
			expectedLimit:  50,
		},
		{
			name:           "Limit exceeds max - should cap at maxLimit",
			queryParams:    map[string]string{"offset": "0", "limit": "200"},
			expectedOffset: 0,
			expectedLimit:  100,
		},
		{
			name:           "Negative offset - should use default",
			queryParams:    map[string]string{"offset": "-5", "limit": "20"},
			expectedOffset: 0,
			expectedLimit:  20,
		},
		{
			name:           "Invalid offset - should use default",
			queryParams:    map[string]string{"offset": "abc", "limit": "20"},
			expectedOffset: 0,
			expectedLimit:  20,
		},
		{
			name:           "Invalid limit - should use default",
			queryParams:    map[string]string{"offset": "10", "limit": "xyz"},
			expectedOffset: 10,
			expectedLimit:  20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock request with query parameters
			u, _ := url.Parse("http://example.com/jobs")
			q := u.Query()
			for k, v := range tt.queryParams {
				q.Set(k, v)
			}
			u.RawQuery = q.Encode()

			// Parse pagination params from URL
			offset, limit := parsePaginationParams(u)

			if offset != tt.expectedOffset {
				t.Errorf("Expected offset %d, got %d", tt.expectedOffset, offset)
			}
			if limit != tt.expectedLimit {
				t.Errorf("Expected limit %d, got %d", tt.expectedLimit, limit)
			}
		})
	}
}

func TestBuildNavigationLinks(t *testing.T) {
	tests := []struct {
		name     string
		offset   int
		limit    int
		total    int
		hasFirst bool
		hasLast  bool
		hasNext  bool
		hasPrev  bool
	}{
		{
			name:     "First page with results",
			offset:   0,
			limit:    20,
			total:    100,
			hasFirst: true,
			hasLast:  true,
			hasNext:  true,
			hasPrev:  false,
		},
		{
			name:     "Middle page with results",
			offset:   20,
			limit:    20,
			total:    100,
			hasFirst: true,
			hasLast:  true,
			hasNext:  true,
			hasPrev:  true,
		},
		{
			name:     "Last page with results",
			offset:   80,
			limit:    20,
			total:    100,
			hasFirst: true,
			hasLast:  true,
			hasNext:  false,
			hasPrev:  true,
		},
		{
			name:     "No results",
			offset:   0,
			limit:    20,
			total:    0,
			hasFirst: false,
			hasLast:  false,
			hasNext:  false,
			hasPrev:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, _ := url.Parse("http://example.com/jobs")
			links := buildNavigationLinks(u, tt.offset, tt.limit, tt.total)

			if tt.hasFirst && links.First == "" {
				t.Error("Expected First link to be present")
			}
			if !tt.hasFirst && links.First != "" {
				t.Error("Expected First link to be empty")
			}

			if tt.hasLast && links.Last == "" {
				t.Error("Expected Last link to be present")
			}
			if !tt.hasLast && links.Last != "" {
				t.Error("Expected Last link to be empty")
			}

			if tt.hasNext && links.Next == "" {
				t.Error("Expected Next link to be present")
			}
			if !tt.hasNext && links.Next != "" {
				t.Error("Expected Next link to be empty")
			}

			if tt.hasPrev && links.Prev == "" {
				t.Error("Expected Prev link to be present")
			}
			if !tt.hasPrev && links.Prev != "" {
				t.Error("Expected Prev link to be empty")
			}
		})
	}
}

func TestCalculateOffsetOfLastPage(t *testing.T) {
	tests := []struct {
		name     string
		total    int
		limit    int
		expected int
	}{
		{
			name:     "Exact multiple",
			total:    100,
			limit:    20,
			expected: 80,
		},
		{
			name:     "Not exact multiple",
			total:    105,
			limit:    20,
			expected: 100,
		},
		{
			name:     "Single page",
			total:    15,
			limit:    20,
			expected: 0,
		},
		{
			name:     "Empty results",
			total:    0,
			limit:    20,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateOffsetOfLastPage(tt.total, tt.limit)
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestBuildPaginatedResponse(t *testing.T) {
	u, _ := url.Parse("http://example.com/jobs")
	data := []string{"job1", "job2", "job3"}

	resp := buildPaginatedResponse(u, 0, 20, 100, data)

	if resp.Meta.Count != 100 {
		t.Errorf("Expected count 100, got %d", resp.Meta.Count)
	}

	if resp.Data == nil {
		t.Error("Expected data to be present")
	}

	if resp.Links.First == "" {
		t.Error("Expected First link to be present")
	}
}
