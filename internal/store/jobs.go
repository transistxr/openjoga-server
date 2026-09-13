package store

import (
	"context"
)

const (
	JobSendNotification = "send_notification"
	JobEventCutoff      = "event_cutoff"
	JobEventComplete    = "event_complete"
	JobReminder         = "reminder"
	JobCleanup          = "cleanup"
)

const (
	JobPending = "pending"
	JobClaimed = "claimed"
	JobDone    = "done"
	JobFailed  = "failed"
)

const MaxJobAttempts = 5

type Job struct {
	ID          int64
	Kind        string
	Payload     string
	RunAt       string
	Status      string
	Attempts    int
	ClaimedAt   *string
	CompletedAt *string
	LastError   *string
	CreatedAt   string
}

func (s *Store) Enqueue(ctx context.Context, kind, payload, runAt string) error {
	if runAt == "" {
		runAt = Now()
	}
	if payload == "" {
		payload = "{}"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (kind, payload, run_at, created_at) VALUES (?,?,?,?)`,
		kind, payload, runAt, Now())
	return err
}

func (s *Store) ClaimDueJobs(ctx context.Context, limit int) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `
		UPDATE jobs
		   SET status = ?, claimed_at = ?, attempts = attempts + 1
		 WHERE id IN (SELECT id FROM jobs
		               WHERE status = ? AND run_at <= ?
		               ORDER BY run_at LIMIT ?)
		RETURNING id, kind, payload, run_at, status, attempts, claimed_at,
		          completed_at, last_error, created_at`,
		JobClaimed, Now(), JobPending, Now(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.Kind, &j.Payload, &j.RunAt, &j.Status,
			&j.Attempts, &j.ClaimedAt, &j.CompletedAt, &j.LastError, &j.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (s *Store) CompleteJob(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, completed_at = ? WHERE id = ?`, JobDone, Now(), id)
	return err
}

func (s *Store) FailJob(ctx context.Context, id int64, attempts int, reason string, retryAt string) error {
	if attempts >= MaxJobAttempts {
		_, err := s.db.ExecContext(ctx,
			`UPDATE jobs SET status = ?, last_error = ?, completed_at = ? WHERE id = ?`,
			JobFailed, reason, Now(), id)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, last_error = ?, run_at = ? WHERE id = ?`,
		JobPending, reason, retryAt, id)
	return err
}

func (s *Store) ReclaimStuckJobs(ctx context.Context, olderThan string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = ? WHERE status = ? AND claimed_at < ?`,
		JobPending, JobClaimed, olderThan)
	return err
}

func (s *Store) DeleteFinishedJobsBefore(ctx context.Context, cutoff string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM jobs WHERE status = ? AND completed_at < ?`, JobDone, cutoff)
	return err
}
