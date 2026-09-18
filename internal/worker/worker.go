// Package worker runs everything that happens without a client: closing RSVP
// windows, sending reminders, and tidying up.
package worker

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"openjoga-server/internal/auth"
	"openjoga-server/internal/notify"
	"openjoga-server/internal/service"
	"openjoga-server/internal/store"
)

const (
	pollInterval  = 10 * time.Second
	sweepInterval = 5 * time.Minute
	batchSize     = 20

	stuckAfter = 10 * time.Minute
)

type Worker struct {
	db       *sql.DB
	store    *store.Store
	svc      *service.Service
	notifier *notify.Notifier
	log      *slog.Logger
}

func New(db *sql.DB, s *store.Store, svc *service.Service, n *notify.Notifier, log *slog.Logger) *Worker {
	return &Worker{db: db, store: s, svc: svc, notifier: n, log: log}
}

func (w *Worker) Run(ctx context.Context) {
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()
	sweep := time.NewTicker(sweepInterval)
	defer sweep.Stop()

	w.log.Info("worker started", "poll", pollInterval)

	if err := w.store.ReclaimStuckJobs(ctx, store.FormatTime(time.Now().Add(-stuckAfter))); err != nil {
		w.log.Error("reclaiming stuck jobs", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			w.log.Info("worker stopped")
			return
		case <-poll.C:
			w.drain(ctx)
		case <-sweep.C:
			w.sweep(ctx)
		}
	}
}

func (w *Worker) drain(ctx context.Context) {
	jobs, err := w.store.ClaimDueJobs(ctx, batchSize)
	if err != nil {
		w.log.Error("claiming jobs", "error", err)
		return
	}

	for _, job := range jobs {
		if err := w.handle(ctx, job); err != nil {
			w.log.Error("job failed", "id", job.ID, "kind", job.Kind,
				"attempt", job.Attempts, "error", err)

			retryAt := store.FormatTime(time.Now().Add(
				time.Duration(job.Attempts) * time.Minute))
			if err := w.store.FailJob(ctx, job.ID, job.Attempts, err.Error(), retryAt); err != nil {
				w.log.Error("recording job failure", "id", job.ID, "error", err)
			}
			continue
		}
		if err := w.store.CompleteJob(ctx, job.ID); err != nil {
			w.log.Error("completing job", "id", job.ID, "error", err)
		}
	}
}

func (w *Worker) handle(ctx context.Context, job store.Job) error {
	switch job.Kind {
	case store.JobEventCutoff:
		return w.handleCutoff(ctx, job)
	case store.JobEventComplete:
		return w.handleComplete(ctx, job)
	case store.JobReminder:
		return w.handleReminder(ctx, job)
	case store.JobSendNotification:
		return w.handleNotification(ctx, job)
	case store.JobCleanup:
		return w.cleanup(ctx)
	default:
		w.log.Warn("unknown job kind, discarding", "kind", job.Kind, "id", job.ID)
		return nil
	}
}

func (w *Worker) sweep(ctx context.Context) {
	if err := w.store.ReclaimStuckJobs(ctx, store.FormatTime(time.Now().Add(-stuckAfter))); err != nil {
		w.log.Error("reclaiming stuck jobs", "error", err)
	}

	due, err := w.store.EventsDueForCutoff(ctx)
	if err != nil {
		w.log.Error("sweeping for cutoffs", "error", err)
	}
	for _, e := range due {
		w.log.Warn("cutoff missed its scheduled job, running it now", "event", e.ID)
		if err := w.runCutoff(ctx, e); err != nil {
			w.log.Error("running missed cutoff", "event", e.ID, "error", err)
		}
	}

	finished, err := w.store.EventsDueForCompletion(ctx)
	if err != nil {
		w.log.Error("sweeping for finished games", "error", err)
	}
	for _, e := range finished {
		if err := w.svc.Complete(ctx, e); err != nil {
			w.log.Error("completing event", "event", e.ID, "error", err)
		}
	}

	if err := w.cleanup(ctx); err != nil {
		w.log.Error("cleanup", "error", err)
	}
}

func (w *Worker) cleanup(ctx context.Context) error {
	if err := w.store.DeleteExpiredChallenges(ctx); err != nil {
		return err
	}
	if err := auth.DeleteExpiredSessions(w.db); err != nil {
		return err
	}
	return w.store.DeleteFinishedJobsBefore(ctx,
		store.FormatTime(time.Now().Add(-7*24*time.Hour)))
}
