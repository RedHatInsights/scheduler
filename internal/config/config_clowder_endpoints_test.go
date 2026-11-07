package config

import (
	"testing"

	clowder "github.com/redhatinsights/app-common-go/pkg/api/v1"
)

const testDefaultExportBaseURL = "http://localhost:9000/api/export/v1"

// setPrivateEndpointsV2 replaces the Clowder V2 private endpoint globals for the
// duration of a test. app-common-go populates these globals from ACG_CONFIG at
// package init, so they are never set in unit tests.
func setPrivateEndpointsV2(t *testing.T, endpoints map[string]map[string]clowder.DependencyEndpointV2) {
	t.Helper()

	previous := clowder.PrivateDependencyEndpointsV2
	clowder.PrivateDependencyEndpointsV2 = endpoints
	t.Cleanup(func() {
		clowder.PrivateDependencyEndpointsV2 = previous
	})
}

func exportServiceWithDefaults() *Config {
	return &Config{
		ExportService: ExportServiceConfig{
			BaseURL: testDefaultExportBaseURL,
		},
	}
}

func exportServiceV2(uri string, authenticated bool) map[string]map[string]clowder.DependencyEndpointV2 {
	return map[string]map[string]clowder.DependencyEndpointV2{
		"export-service": {
			"service": {
				Uri:           uri,
				Authenticated: authenticated,
			},
		},
	}
}

func TestApplyClowderExportEndpointUsesV2PrivateEndpoint(t *testing.T) {
	setPrivateEndpointsV2(t, exportServiceV2("http://export-service-service:8000", false))

	config := exportServiceWithDefaults()
	applyClowderExportEndpoint(config)

	if config.ExportService.BaseURL != "http://export-service-service:8000/api/export/v1" {
		t.Errorf("Expected Clowder base URL, got %s", config.ExportService.BaseURL)
	}
	if config.ExportService.Authenticated {
		t.Error("Expected unauthenticated in-cluster endpoint")
	}
	if config.ExportService.CACertificate != "" {
		t.Errorf("Expected no CA certificate, got %s", config.ExportService.CACertificate)
	}
}

func TestApplyClowderExportEndpointHandlesURIPaths(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		expected string
	}{
		{
			name:     "appends API path to bare URI",
			uri:      "http://export-service-service:8000",
			expected: "http://export-service-service:8000/api/export/v1",
		},
		{
			name:     "trims trailing slash before appending",
			uri:      "http://export-service-service:8000/",
			expected: "http://export-service-service:8000/api/export/v1",
		},
		{
			name:     "keeps a base path supplied by Clowder",
			uri:      "https://export.example.com/export/v2",
			expected: "https://export.example.com/export/v2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setPrivateEndpointsV2(t, exportServiceV2(tc.uri, false))

			config := exportServiceWithDefaults()
			applyClowderExportEndpoint(config)

			if config.ExportService.BaseURL != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, config.ExportService.BaseURL)
			}
		})
	}
}

func TestApplyClowderExportEndpointRecordsV2Metadata(t *testing.T) {
	caPath := "/cdapp/certs/export-service-ca.crt"
	endpoints := exportServiceV2("https://export.remote.example:443", true)
	endpoint := endpoints["export-service"]["service"]
	endpoint.CaCertificate = &caPath
	endpoints["export-service"]["service"] = endpoint
	setPrivateEndpointsV2(t, endpoints)

	config := exportServiceWithDefaults()
	applyClowderExportEndpoint(config)

	if config.ExportService.BaseURL != "https://export.remote.example:443/api/export/v1" {
		t.Errorf("Expected Clowder base URL, got %s", config.ExportService.BaseURL)
	}
	if !config.ExportService.Authenticated {
		t.Error("Expected authenticated endpoint")
	}
	if config.ExportService.CACertificate != caPath {
		t.Errorf("Expected CA certificate %s, got %s", caPath, config.ExportService.CACertificate)
	}
}

func TestApplyClowderExportEndpointKeepsConfiguredURLWithoutV2Data(t *testing.T) {
	tests := []struct {
		name      string
		endpoints map[string]map[string]clowder.DependencyEndpointV2
	}{
		{
			name:      "no V2 map at all",
			endpoints: nil,
		},
		{
			name: "app not present",
			endpoints: map[string]map[string]clowder.DependencyEndpointV2{
				"rbac": {"service": {Uri: "http://rbac-service:8000"}},
			},
		},
		{
			name: "deployment not present",
			endpoints: map[string]map[string]clowder.DependencyEndpointV2{
				"export-service": {"other": {Uri: "http://export-other:8000"}},
			},
		},
		{
			name:      "blank URI",
			endpoints: exportServiceV2("   ", false),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setPrivateEndpointsV2(t, tc.endpoints)

			config := exportServiceWithDefaults()
			applyClowderExportEndpoint(config)

			if config.ExportService.BaseURL != testDefaultExportBaseURL {
				t.Errorf("Expected configured base URL to be kept, got %s", config.ExportService.BaseURL)
			}
		})
	}
}

func TestApplyClowderOverridesWiresExportEndpoint(t *testing.T) {
	setPrivateEndpointsV2(t, exportServiceV2("http://export-service-service:8000", false))

	config := exportServiceWithDefaults()
	applyClowderOverrides(config, &clowder.AppConfig{})

	if config.ExportService.BaseURL != "http://export-service-service:8000/api/export/v1" {
		t.Errorf("Expected Clowder base URL from overrides, got %s", config.ExportService.BaseURL)
	}
}

func TestApplyDerivedValuesPublicBaseURLFollowsClowderURL(t *testing.T) {
	setPrivateEndpointsV2(t, exportServiceV2("http://export-service-service:8000", false))

	config := exportServiceWithDefaults()
	applyClowderExportEndpoint(config)
	applyDerivedValues(config)

	if config.ExportService.PublicBaseURL != config.ExportService.BaseURL {
		t.Errorf("Expected public base URL %s to match base URL, got %s",
			config.ExportService.BaseURL, config.ExportService.PublicBaseURL)
	}
}

func TestApplyDerivedValuesKeepsExplicitPublicBaseURL(t *testing.T) {
	setPrivateEndpointsV2(t, exportServiceV2("http://export-service-service:8000", false))

	const publicURL = "https://console.redhat.com/api/export/v1"
	config := exportServiceWithDefaults()
	config.ExportService.PublicBaseURL = publicURL
	applyClowderExportEndpoint(config)
	applyDerivedValues(config)

	if config.ExportService.PublicBaseURL != publicURL {
		t.Errorf("Expected explicit public base URL %s to be preserved, got %s",
			publicURL, config.ExportService.PublicBaseURL)
	}
}
