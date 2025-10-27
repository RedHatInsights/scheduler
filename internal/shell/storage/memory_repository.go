package storage

import (
	"sync"

	"insights-scheduler-part-2/internal/core/domain"
)

type MemoryJobRepository struct {
	jobs map[string]domain.Job
	mu   sync.RWMutex
}

func NewMemoryJobRepository() *MemoryJobRepository {
	return &MemoryJobRepository{
		jobs: make(map[string]domain.Job),
		mu:   sync.RWMutex{},
	}
}

func (r *MemoryJobRepository) Save(job domain.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.jobs[job.ID] = job
	return nil
}

func (r *MemoryJobRepository) FindByID(id string) (domain.Job, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	job, exists := r.jobs[id]
	if !exists {
		return domain.Job{}, domain.ErrJobNotFound
	}

	return job, nil
}

func (r *MemoryJobRepository) FindAll() ([]domain.Job, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	jobs := make([]domain.Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		jobs = append(jobs, job)
	}

	return jobs, nil
}

func (r *MemoryJobRepository) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.jobs[id]; !exists {
		return domain.ErrJobNotFound
	}

	delete(r.jobs, id)
	return nil
}
