package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"insights-scheduler/internal/core/domain"
)

const baseURL = "http://localhost:5000/api/v1"

func main() {
	fmt.Println("Starting Go REST API tests...")
	fmt.Println("Make sure the service is running on localhost:5000")
	fmt.Println(strings.Repeat("-", 50))

	// Test job creation
	jobID, err := testCreateJob()
	if err != nil {
		log.Fatalf("Failed to create job: %v", err)
	}
	fmt.Println()

	// Test getting all jobs
	if err := testGetAllJobs(); err != nil {
		log.Printf("Failed to get all jobs: %v", err)
	}
	fmt.Println()

	// Test getting specific job
	if err := testGetJob(jobID); err != nil {
		log.Printf("Failed to get job: %v", err)
	}
	fmt.Println()

	// Test updating job
	if err := testUpdateJob(jobID); err != nil {
		log.Printf("Failed to update job: %v", err)
	}
	fmt.Println()

	// Test patching job
	if err := testPatchJob(jobID); err != nil {
		log.Printf("Failed to patch job: %v", err)
	}
	fmt.Println()

	// Test manual job run
	if err := testRunJob(jobID); err != nil {
		log.Printf("Failed to run job: %v", err)
	}
	fmt.Println()

	// Test job pause
	if err := testPauseJob(jobID); err != nil {
		log.Printf("Failed to pause job: %v", err)
	}
	fmt.Println()

	// Test job resume
	if err := testResumeJob(jobID); err != nil {
		log.Printf("Failed to resume job: %v", err)
	}
	fmt.Println()

	// Test job deletion
	if err := testDeleteJob(jobID); err != nil {
		log.Printf("Failed to delete job: %v", err)
	}
	fmt.Println()

	fmt.Println(strings.Repeat("-", 50))
	fmt.Println("All tests completed!")
}

func testCreateJob() (string, error) {
	fmt.Println("Testing job creation...")

	jobData := map[string]interface{}{
		"name":     "Test Job",
		"schedule": "0 */10 * * * *",
		"payload": map[string]interface{}{
			"type": "message",
			"details": map[string]interface{}{
				"message": "Hello, World!",
			},
		},
	}

	resp, err := makeRequest("POST", "/jobs", jobData)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("expected status 201, got %d", resp.StatusCode)
	}

	var job domain.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return "", err
	}

	fmt.Printf("✓ Job created successfully: %s\n", job.ID)
	return job.ID, nil
}

func testGetAllJobs() error {
	fmt.Println("Testing get all jobs...")

	resp, err := makeRequest("GET", "/jobs", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var jobs []domain.Job
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return err
	}

	fmt.Printf("✓ Retrieved %d jobs\n", len(jobs))
	return nil
}

func testGetJob(jobID string) error {
	fmt.Printf("Testing get job %s...\n", jobID)

	resp, err := makeRequest("GET", "/jobs/"+jobID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var job domain.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return err
	}

	fmt.Printf("✓ Retrieved job: %s\n", job.Name)
	return nil
}

func testUpdateJob(jobID string) error {
	fmt.Printf("Testing job update %s...\n", jobID)

	updateData := map[string]interface{}{
		"name":     "Updated Test Job",
		"schedule": "0 0 * * * *",
		"payload": map[string]interface{}{
			"type": "message",
			"details": map[string]interface{}{
				"message": "Updated message!",
			},
		},
		"status": "scheduled",
	}

	resp, err := makeRequest("PUT", "/jobs/"+jobID, updateData)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var job domain.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return err
	}

	fmt.Printf("✓ Job updated successfully: %s\n", job.Name)
	return nil
}

func testPatchJob(jobID string) error {
	fmt.Printf("Testing job patch %s...\n", jobID)

	patchData := map[string]interface{}{
		"name": "Patched Test Job",
	}

	resp, err := makeRequest("PATCH", "/jobs/"+jobID, patchData)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var job domain.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return err
	}

	fmt.Printf("✓ Job patched successfully: %s\n", job.Name)
	return nil
}

func testRunJob(jobID string) error {
	fmt.Printf("Testing manual job run %s...\n", jobID)

	resp, err := makeRequest("POST", "/jobs/"+jobID+"/run", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("expected status 202, got %d", resp.StatusCode)
	}

	fmt.Println("✓ Job run triggered successfully")
	return nil
}

func testPauseJob(jobID string) error {
	fmt.Printf("Testing job pause %s...\n", jobID)

	resp, err := makeRequest("POST", "/jobs/"+jobID+"/pause", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var job domain.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return err
	}

	fmt.Printf("✓ Job paused successfully: %s\n", job.Status)
	return nil
}

func testResumeJob(jobID string) error {
	fmt.Printf("Testing job resume %s...\n", jobID)

	resp, err := makeRequest("POST", "/jobs/"+jobID+"/resume", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var job domain.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return err
	}

	fmt.Printf("✓ Job resumed successfully: %s\n", job.Status)
	return nil
}

func testDeleteJob(jobID string) error {
	fmt.Printf("Testing job deletion %s...\n", jobID)

	resp, err := makeRequest("DELETE", "/jobs/"+jobID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("expected status 204, got %d", resp.StatusCode)
	}

	fmt.Println("✓ Job deleted successfully")
	return nil
}

func makeRequest(method, endpoint string, data interface{}) (*http.Response, error) {
	var body io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		body = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, baseURL+endpoint, body)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add mock identity headers for testing
	// In production, these would be set by the identity middleware
	// Base64 encoded identity: {"identity":{"account_number":"000001","org_id":"000001","user":{"username":"testuser","email":"test@example.com"},"type":"User"}}
	req.Header.Set("X-Rh-Identity", "eyJpZGVudGl0eSI6eyJhY2NvdW50X251bWJlciI6IjAwMDAwMSIsIm9yZ19pZCI6IjAwMDAwMSIsInVzZXIiOnsidXNlcm5hbWUiOiJ0ZXN0dXNlciIsImVtYWlsIjoidGVzdEBleGFtcGxlLmNvbSJ9LCJ0eXBlIjoiVXNlciJ9fQ==")

	client := &http.Client{Timeout: 10 * time.Second}
	return client.Do(req)
}
