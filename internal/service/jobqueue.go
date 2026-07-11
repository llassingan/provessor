package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/llassingan/provessor/internal/logger"
	"github.com/llassingan/provessor/internal/model"
	"github.com/llassingan/provessor/internal/repository"
)

// Job types (string constants)
const (
	JobProvisionNetwork = "provision_network"
	JobProvisionVPS     = "provision_vps"
	JobTerminateVPS     = "terminate_vps"
)

// Payload types — serialized to JSON and stored in jobs.payload
type NetworkJob struct {
	ID int64 `json:"id"`
}

type VPSJob struct {
	ID int64 `json:"id"`
}

type TerminateJob struct {
	ID         int64  `json:"id"`
	Region     string `json:"region"`
	InstanceID string `json:"instance_id"`
}

type JobQueue struct {
	db                  *sql.DB
	networkService      *NetworkService
	vpsProvisionService *VPSProvisionService
	log                 *logger.Logger
	audit               *repository.AuditLogRepository
	wg                  sync.WaitGroup
	stopCh              chan struct{}
}

func NewJobQueue(
	db *sql.DB,
	networkService *NetworkService,
	vpsProvisionService *VPSProvisionService,
	log *logger.Logger,
	audit *repository.AuditLogRepository,
) *JobQueue {
	return &JobQueue{
		db:                  db,
		networkService:      networkService,
		vpsProvisionService: vpsProvisionService,
		log:                 log,
		audit:               audit,
		stopCh:              make(chan struct{}),
	}
}

// Enqueue inserts a new job with status='pending'
func (q *JobQueue) Enqueue(ctx context.Context, jobType string, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	_, err = q.db.ExecContext(ctx,
		"INSERT INTO jobs (type, payload, status, max_attempts) VALUES (?, ?, 'pending', 3)",
		jobType, string(payloadBytes),
	)
	return err
}

// Start launches the worker goroutine
func (q *JobQueue) Start(ctx context.Context) {
	q.wg.Add(1)
	go q.workerLoop(ctx)
}

// Stop signals the worker to exit and waits for in-flight jobs (with timeout)
func (q *JobQueue) Stop() {
	close(q.stopCh)
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		q.log.Info("jobqueue_shutdown_complete")
	case <-time.After(60 * time.Second):
		q.log.Warn("jobqueue_shutdown_timeout")
	}
}

// ResumeOnStartup resets any jobs stuck in 'running' (interrupted by crash) back to 'pending'
func (q *JobQueue) ResumeOnStartup(ctx context.Context) error {
	result, err := q.db.ExecContext(ctx,
		"UPDATE jobs SET status='pending', updated_at=CURRENT_TIMESTAMP WHERE status='running'",
	)
	if err != nil {
		return fmt.Errorf("reset stuck jobs: %w", err)
	}
	n, _ := result.RowsAffected()
	if n > 0 {
		q.log.Info("jobqueue_reset_interrupted_jobs", "count", n)
	}
	return nil
}

// workerLoop polls every 2s for pending jobs; exits when stopCh closes
func (q *JobQueue) workerLoop(ctx context.Context) {
	defer q.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			q.log.Error("jobqueue_worker_panic", "error", fmt.Errorf("%v", r))
		}
	}()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-q.stopCh:
			q.log.Info("jobqueue_worker_stop_signal")
			return
		case <-ctx.Done():
			q.log.Info("jobqueue_worker_ctx_done")
			return
		case <-ticker.C:
			if err := q.processOneJob(ctx); err != nil {
				q.log.Error("jobqueue_process_job_failed", "error", err)
			}
		}
	}
}

// processOneJob claims one pending job atomically and executes it
func (q *JobQueue) processOneJob(ctx context.Context) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // safe to call after commit

	// Select oldest pending job whose updated_at is in the past (backoff support)
	row := tx.QueryRowContext(ctx,
		"SELECT id, type, payload, attempts FROM jobs WHERE status='pending' AND datetime(updated_at) <= datetime('now') ORDER BY id ASC LIMIT 1",
	)
	var (
		jobID    int64
		jobType  string
		payload  string
		attempts int
	)
	if err := row.Scan(&jobID, &jobType, &payload, &attempts); err != nil {
		if err == sql.ErrNoRows {
			return tx.Commit() // no jobs — nothing to do
		}
		return err
	}
	// Claim it (set running only if still pending — atomic)
	res, err := tx.ExecContext(ctx,
		"UPDATE jobs SET status='running', updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='pending'",
		jobID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return tx.Commit() // someone else claimed it — race, skip
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Execute the job — each job runs with panic recovery
	q.runJobWithRecovery(ctx, jobID, jobType, payload, attempts)
	return nil
}

// runJobWithRecovery executes the job, captures panics, and updates status
func (q *JobQueue) runJobWithRecovery(ctx context.Context, jobID int64, jobType, payload string, attempts int) {
	defer func() {
		if r := recover(); r != nil {
			q.log.Error("jobqueue_job_panic", "job_id", jobID, "job_type", jobType, "error", fmt.Errorf("%v", r))
			q.markFailed(ctx, jobID, fmt.Sprintf("panic: %v", r), attempts+1)
		}
	}()

	var jobErr error
	switch jobType {
	case JobProvisionNetwork:
		var p NetworkJob
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			jobErr = fmt.Errorf("unmarshal payload: %w", err)
			break
		}
		jobErr = q.networkService.ProvisionNetwork(ctx, p.ID)
	case JobProvisionVPS:
		var p VPSJob
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			jobErr = fmt.Errorf("unmarshal payload: %w", err)
			break
		}
		jobErr = q.vpsProvisionService.ProvisionVPS(ctx, p.ID)
	case JobTerminateVPS:
		var p TerminateJob
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			jobErr = fmt.Errorf("unmarshal payload: %w", err)
			break
		}
		jobErr = q.vpsProvisionService.TerminateInstance(ctx, p.ID, p.Region, p.InstanceID)
	default:
		jobErr = fmt.Errorf("unknown job type: %s", jobType)
	}

	if jobErr != nil {
		q.markFailed(ctx, jobID, jobErr.Error(), attempts+1)
		return
	}
	q.markComplete(ctx, jobID)
}

func (q *JobQueue) markComplete(ctx context.Context, jobID int64) {
	_, err := q.db.ExecContext(ctx,
		"UPDATE jobs SET status='complete', updated_at=CURRENT_TIMESTAMP WHERE id=?",
		jobID,
	)
	if err != nil {
		q.log.Warn("jobqueue_mark_complete_failed", "job_id", jobID, "error", err)
	}
}

func (q *JobQueue) markFailed(ctx context.Context, jobID int64, errMsg string, newAttempts int) {
	if newAttempts >= 3 {
		q.log.Error("jobqueue_final_failure_triggering_rollback", "job_id", jobID, "attempts", newAttempts, "error", errMsg)
		q.audit.Log(ctx, model.AuditLog{Operation: "job.failed", ResourceType: "job", ResourceID: jobID, Status: "failure", ErrorMessage: sanitize(errMsg)})
		q.triggerRollback(ctx, jobID)
		_, err := q.db.ExecContext(ctx,
			"UPDATE jobs SET status='failed', attempts=?, last_error=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
			newAttempts, errMsg, jobID,
		)
		if err != nil {
			q.log.Warn("jobqueue_mark_failed_db_error", "job_id", jobID, "error", err)
		}
		return
	}
	// Retry with exponential backoff: set updated_at in the future so worker skips until then
	var delay time.Duration
	switch newAttempts {
	case 1:
		delay = 1 * time.Minute
	case 2:
		delay = 5 * time.Minute
	default:
		delay = 15 * time.Minute
	}
	runAt := time.Now().UTC().Add(delay)
	_, err := q.db.ExecContext(ctx,
		"UPDATE jobs SET status='pending', attempts=?, last_error=?, updated_at=? WHERE id=?",
		newAttempts, errMsg, runAt.Format("2006-01-02 15:04:05"), jobID,
	)
	if err != nil {
		q.log.Warn("jobqueue_mark_retry_failed", "job_id", jobID, "error", err)
	}
	q.log.Info("jobqueue_retrying", "job_id", jobID, "delay", delay, "attempt", newAttempts)
}

// triggerRollback determines which rollback to invoke based on the job type
func (q *JobQueue) triggerRollback(ctx context.Context, jobID int64) {
	q.audit.Log(ctx, model.AuditLog{Operation: "job.rollback", ResourceType: "job", ResourceID: jobID, Status: "triggered"})
	var jobType, payload string
	err := q.db.QueryRowContext(ctx,
		"SELECT type, payload FROM jobs WHERE id=?", jobID,
	).Scan(&jobType, &payload)
	if err != nil {
		q.log.Error("jobqueue_rollback_lookup_failed", "job_id", jobID, "error", err)
		return
	}
	switch jobType {
	case JobProvisionNetwork:
		var p NetworkJob
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			q.log.Error("jobqueue_rollback_unmarshal_network_failed", "job_id", jobID, "error", err)
			return
		}
		settings, err := q.networkService.settingsRepo.Get(ctx)
		if err != nil || settings.Region == "" {
			q.log.Error("jobqueue_rollback_network_no_region", "network_id", p.ID)
			return
		}
		q.networkService.RollbackNetwork(ctx, p.ID, settings.Region)
	case JobProvisionVPS:
		var p VPSJob
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			q.log.Error("jobqueue_rollback_unmarshal_vps_failed", "job_id", jobID, "error", err)
			return
		}
		vps, err := q.vpsProvisionService.vpsRepo.Get(ctx, p.ID)
		if err != nil {
			q.log.Error("jobqueue_rollback_load_vps_failed", "vps_id", p.ID, "error", err)
			return
		}
		if !vps.NetworkID.Valid || !vps.OCIInstanceID.Valid {
			q.log.Warn("jobqueue_rollback_vps_no_instance", "vps_id", p.ID)
			return
		}
		network, err := q.vpsProvisionService.networkRepo.Get(ctx, vps.NetworkID.Int64)
		if err != nil {
			q.log.Error("jobqueue_rollback_load_network_failed", "network_id", vps.NetworkID.Int64, "error", err)
			return
		}
		q.vpsProvisionService.RollbackVPS(ctx, p.ID, network.Region, vps.OCIInstanceID.String)
	case JobTerminateVPS:
		q.log.Warn("jobqueue_terminate_final_failure", "job_id", jobID)
	}
}
