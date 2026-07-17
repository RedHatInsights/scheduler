package scheduler

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/robfig/cron/v3"
	"insights-scheduler/internal/config"
	"insights-scheduler/internal/core/domain"
)

// getDefaultParser returns the standard 5-field cron parser
func getDefaultParser() cron.Parser {
	return cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
}

// Mock JobExecutor for testing
type mockJobExecutor struct {
	executedJobs          []domain.Job
	executeFunc           func(job domain.Job) error
	executeWithJobRunFunc func(job domain.Job, jobRunID string) error
}

func (m *mockJobExecutor) Execute(job domain.Job) error {
	if m.executeFunc != nil {
		return m.executeFunc(job)
	}
	m.executedJobs = append(m.executedJobs, job)
	return nil
}

func (m *mockJobExecutor) ExecuteWithJobRun(job domain.Job, jobRunID string) error {
	if m.executeWithJobRunFunc != nil {
		return m.executeWithJobRunFunc(job, jobRunID)
	}
	m.executedJobs = append(m.executedJobs, job)
	return nil
}

func (m *mockJobExecutor) Wait() {
	// No-op for tests
}

// Mock JobRepository for testing
type mockJobRepository struct {
	jobs map[string]domain.Job
}

func (m *mockJobRepository) Save(job domain.Job) error {
	m.jobs[job.ID] = job
	return nil
}

func (m *mockJobRepository) FindByID(id string) (domain.Job, error) {
	job, ok := m.jobs[id]
	if !ok {
		return domain.Job{}, domain.ErrJobNotFound
	}
	return job, nil
}

// setupTestRedis creates a test Redis instance using miniredis
func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	return mr, client
}

func TestRedisScheduler_ScheduleJobImmediately(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()

	executor := &mockJobExecutor{}
	repo := &mockJobRepository{jobs: make(map[string]domain.Job)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler := &RedisScheduler{
		client:       client,
		executor:     executor,
		jobRepo:      repo,
		parser:       getDefaultParser(),
		ctx:          ctx,
		cancel:       cancel,
		pollInterval: 10 * time.Second,
	}

	// Create a test job
	job := domain.NewJob("Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{
		"format": "json",
	})

	testJobRunID := "test-job-run-123"

	// Schedule job immediately
	err := scheduler.ScheduleJobImmediately(job, testJobRunID)
	if err != nil {
		t.Fatalf("ScheduleJobImmediately() unexpected error: %v", err)
	}

	// Verify job was added to Redis sorted set
	score, err := client.ZScore(ctx, scheduledJobsKey, job.ID).Result()
	if err != nil {
		t.Fatalf("Job not found in Redis sorted set: %v", err)
	}

	// Verify the score (timestamp) is in the past (for immediate execution)
	jobTimestamp := int64(score)
	now := time.Now().Unix()
	if jobTimestamp > now {
		t.Errorf("Job scheduled for future (%d), expected past or current time (%d)", jobTimestamp, now)
	}

	// Verify timestamp is within last 10 seconds (5 seconds in the past + small buffer)
	timeDiff := now - jobTimestamp
	if timeDiff > 10 {
		t.Errorf("Job timestamp too far in the past: %d seconds ago", timeDiff)
	}

	// Verify job data was stored in Redis
	jobKey := jobDataKeyPrefix + job.ID
	jobData, err := client.Get(ctx, jobKey).Result()
	if err != nil {
		t.Fatalf("Job data not found in Redis: %v", err)
	}

	var scheduledJob ScheduledJob
	err = json.Unmarshal([]byte(jobData), &scheduledJob)
	if err != nil {
		t.Fatalf("Failed to unmarshal job data: %v", err)
	}

	// Verify the job content
	if scheduledJob.Job.ID != job.ID {
		t.Errorf("Job ID mismatch: got %s, want %s", scheduledJob.Job.ID, job.ID)
	}

	if scheduledJob.Job.Name != job.Name {
		t.Errorf("Job Name mismatch: got %s, want %s", scheduledJob.Job.Name, job.Name)
	}

	// Verify NextRun is in the past (for immediate execution)
	if scheduledJob.NextRun.After(time.Now()) {
		t.Errorf("NextRun should be in the past for immediate execution, got %s", scheduledJob.NextRun)
	}
}

func TestRedisScheduler_ScheduleJobImmediately_MultipleCalls(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()

	executor := &mockJobExecutor{}
	repo := &mockJobRepository{jobs: make(map[string]domain.Job)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler := &RedisScheduler{
		client:       client,
		executor:     executor,
		jobRepo:      repo,
		parser:       getDefaultParser(),
		ctx:          ctx,
		cancel:       cancel,
		pollInterval: 10 * time.Second,
	}

	job := domain.NewJob("Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})

	// Schedule job immediately multiple times
	err := scheduler.ScheduleJobImmediately(job, "run-1")
	if err != nil {
		t.Fatalf("First ScheduleJobImmediately() unexpected error: %v", err)
	}

	time.Sleep(10 * time.Millisecond) // Small delay to ensure different timestamps

	err = scheduler.ScheduleJobImmediately(job, "run-2")
	if err != nil {
		t.Fatalf("Second ScheduleJobImmediately() unexpected error: %v", err)
	}

	// Verify job still exists in sorted set (should be updated, not duplicated)
	count, err := client.ZCount(ctx, scheduledJobsKey, "-inf", "+inf").Result()
	if err != nil {
		t.Fatalf("Failed to count jobs in sorted set: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 job in sorted set after multiple ScheduleJobImmediately calls, got %d", count)
	}
}

func TestRedisScheduler_ScheduleJob_RegularScheduling(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()

	executor := &mockJobExecutor{}
	repo := &mockJobRepository{jobs: make(map[string]domain.Job)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler := &RedisScheduler{
		client:       client,
		executor:     executor,
		jobRepo:      repo,
		parser:       getDefaultParser(),
		ctx:          ctx,
		cancel:       cancel,
		pollInterval: 10 * time.Second,
	}

	job := domain.NewJob("Test Job", "org-123", "user-123", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})

	// Regular schedule (not immediate)
	err := scheduler.ScheduleJob(job)
	if err != nil {
		t.Fatalf("ScheduleJob() unexpected error: %v", err)
	}

	// Verify job was added to Redis sorted set
	score, err := client.ZScore(ctx, scheduledJobsKey, job.ID).Result()
	if err != nil {
		t.Fatalf("Job not found in Redis sorted set: %v", err)
	}

	// Verify the score (timestamp) is in the FUTURE (for regular scheduling)
	jobTimestamp := int64(score)
	now := time.Now().Unix()
	if jobTimestamp <= now {
		t.Errorf("Regular scheduled job should be in the future, got timestamp %d (now: %d)", jobTimestamp, now)
	}
}

func TestNewRedisScheduler_WithConfig(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	mr.RequireAuth("test-password")

	host, port := mr.Host(), mr.Server().Addr().Port

	cfg := config.RedisConfig{
		Enabled:  true,
		Host:     host,
		Port:     port,
		Password: "test-password",
		DB:       0,
		PoolSize: 5,
	}

	exec := &mockJobExecutor{}
	repo := &mockJobRepository{jobs: make(map[string]domain.Job)}

	rs, err := NewRedisScheduler(cfg, exec, repo, 10*time.Second)
	if err != nil {
		t.Fatalf("NewRedisScheduler() unexpected error: %v", err)
	}
	defer rs.Close()

	// Verify the connection works (Ping succeeded inside NewRedisScheduler)
	job := domain.NewJob("Config Test", "org-1", "user-1", "0 * * * *", "UTC", domain.PayloadExport, map[string]interface{}{})
	if err := rs.ScheduleJob(job); err != nil {
		t.Fatalf("ScheduleJob() unexpected error: %v", err)
	}
}

func TestNewRedisScheduler_WrongPassword(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	mr.RequireAuth("correct-password")

	host, port := mr.Host(), mr.Server().Addr().Port

	cfg := config.RedisConfig{
		Enabled:  true,
		Host:     host,
		Port:     port,
		Password: "wrong-password",
		DB:       0,
	}

	_, err := NewRedisScheduler(cfg, &mockJobExecutor{}, &mockJobRepository{jobs: make(map[string]domain.Job)}, 10*time.Second)
	if err == nil {
		t.Fatal("Expected error with wrong password, got nil")
	}
}

func TestBuildTLSConfig_Defaults(t *testing.T) {
	cfg := config.TLSConfig{
		Enabled: true,
	}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig() unexpected error: %v", err)
	}

	if tlsCfg == nil {
		t.Fatal("Expected non-nil TLS config")
	}

	if tlsCfg.InsecureSkipVerify {
		t.Error("Expected InsecureSkipVerify to be false by default")
	}

	if tlsCfg.RootCAs != nil {
		t.Error("Expected RootCAs to be nil when no CA file provided")
	}

	if len(tlsCfg.Certificates) != 0 {
		t.Error("Expected no client certificates when none provided")
	}
}

func TestBuildTLSConfig_InsecureSkipVerify(t *testing.T) {
	cfg := config.TLSConfig{
		Enabled:            true,
		InsecureSkipVerify: true,
	}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig() unexpected error: %v", err)
	}

	if !tlsCfg.InsecureSkipVerify {
		t.Error("Expected InsecureSkipVerify to be true")
	}
}

func TestBuildTLSConfig_WithCAFile(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	caPEM := generateSelfSignedCACert(t)
	if err := os.WriteFile(caFile, caPEM, 0644); err != nil {
		t.Fatalf("Failed to write CA file: %v", err)
	}

	cfg := config.TLSConfig{
		Enabled: true,
		CAFile:  caFile,
	}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig() unexpected error: %v", err)
	}

	if tlsCfg.RootCAs == nil {
		t.Fatal("Expected RootCAs to be set when CA file is provided")
	}
}

func TestBuildTLSConfig_CAFileNotFound(t *testing.T) {
	cfg := config.TLSConfig{
		Enabled: true,
		CAFile:  "/nonexistent/ca.pem",
	}

	_, err := buildTLSConfig(cfg)
	if err == nil {
		t.Fatal("Expected error for missing CA file, got nil")
	}
}

func TestBuildTLSConfig_InvalidCACert(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "bad-ca.pem")
	if err := os.WriteFile(caFile, []byte("not a valid cert"), 0644); err != nil {
		t.Fatalf("Failed to write bad CA file: %v", err)
	}

	cfg := config.TLSConfig{
		Enabled: true,
		CAFile:  caFile,
	}

	_, err := buildTLSConfig(cfg)
	if err == nil {
		t.Fatal("Expected error for invalid CA cert, got nil")
	}
}

func TestBuildTLSConfig_WithClientCerts(t *testing.T) {
	tmpDir := t.TempDir()
	certFile, keyFile := generateSelfSignedClientCert(t, tmpDir)

	cfg := config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig() unexpected error: %v", err)
	}

	if len(tlsCfg.Certificates) != 1 {
		t.Errorf("Expected 1 client certificate, got %d", len(tlsCfg.Certificates))
	}
}

func TestBuildTLSConfig_InvalidClientCerts(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	os.WriteFile(certFile, []byte("bad cert"), 0644)
	os.WriteFile(keyFile, []byte("bad key"), 0644)

	cfg := config.TLSConfig{
		Enabled:  true,
		CertFile: certFile,
		KeyFile:  keyFile,
	}

	_, err := buildTLSConfig(cfg)
	if err == nil {
		t.Fatal("Expected error for invalid client certs, got nil")
	}
}

func TestBuildTLSConfig_PartialClientCert_CertOnly(t *testing.T) {
	cfg := config.TLSConfig{
		Enabled:  true,
		CertFile: "/some/cert.pem",
	}

	_, err := buildTLSConfig(cfg)
	if err == nil {
		t.Fatal("Expected error when CertFile is set without KeyFile, got nil")
	}
	if !strings.Contains(err.Error(), "both cert_file and key_file must be provided") {
		t.Errorf("Expected partial cert error message, got: %v", err)
	}
}

func TestBuildTLSConfig_PartialClientCert_KeyOnly(t *testing.T) {
	cfg := config.TLSConfig{
		Enabled: true,
		KeyFile: "/some/key.pem",
	}

	_, err := buildTLSConfig(cfg)
	if err == nil {
		t.Fatal("Expected error when KeyFile is set without CertFile, got nil")
	}
	if !strings.Contains(err.Error(), "both cert_file and key_file must be provided") {
		t.Errorf("Expected partial cert error message, got: %v", err)
	}
}

// generateSelfSignedCACert creates a self-signed CA certificate in PEM format for testing.
func generateSelfSignedCACert(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(1 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

// generateSelfSignedClientCert creates a self-signed client cert+key pair for testing.
func generateSelfSignedClientCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Test Client"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("Failed to marshal private key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	certPath = filepath.Join(dir, "client.pem")
	keyPath = filepath.Join(dir, "client-key.pem")
	os.WriteFile(certPath, certPEM, 0644)
	os.WriteFile(keyPath, keyPEM, 0600)

	return certPath, keyPath
}
