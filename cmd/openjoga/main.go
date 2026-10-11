// Command openjoga is the whole server: API and background worker in one
// binary, with one SQLite file beside it.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"openjoga-server/internal/auth"
	"openjoga-server/internal/config"
	openjogahttp "openjoga-server/internal/http"
	"openjoga-server/internal/notify"
	"openjoga-server/internal/service"
	"openjoga-server/internal/store"
	"openjoga-server/internal/worker"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "vapid" {
		private, public, err := notify.GenerateVAPIDKeys()
		if err != nil {
			fmt.Fprintln(os.Stderr, "generating keys:", err)
			os.Exit(1)
		}
		fmt.Printf("OPENJOGA_VAPID_PRIVATE=%s\nOPENJOGA_VAPID_PUBLIC=%s\n", private, public)
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	level := slog.LevelInfo
	if cfg.Dev {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := store.Migrate(db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	log.Info("database ready", "path", cfg.DBPath)

	s := store.New(db)
	if _, err := s.GetCommunity(context.Background(), "OpenJoga", cfg.Currency); err != nil {
		return fmt.Errorf("initialising community: %w", err)
	}

	var mailer notify.Mailer = &notify.LogMailer{Log: log}
	if cfg.SMTPEnabled() {
		mailer = &notify.SMTPMailer{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			User: cfg.SMTPUser, Pass: cfg.SMTPPass, From: cfg.EmailFrom,
		}
		log.Info("email enabled", "host", cfg.SMTPHost)
	} else {
		log.Warn("no SMTP configured: sign-in emails will be printed here instead")
	}

	var pusher *notify.Pusher
	if cfg.PushEnabled() {
		pusher = notify.NewPusher(s, cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject)
		log.Info("web push enabled")
	} else {
		log.Warn("web push not configured: players who RSVP with only a name " +
			"cannot be told if a game is cancelled (run `openjoga vapid`)")
	}

	svc := service.New(s, cfg.BaseURL)
	notifier := notify.New(s, mailer, pusher, log)
	sessions := auth.NewSessionManager(db, !cfg.Dev)
	challenges := auth.NewChallenges(s, mailer, cfg.CodePepper, cfg.BaseURL)

	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	go worker.New(db, s, svc, notifier, log).Run(ctx)

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: openjogahttp.NewServer(openjogahttp.Options{
			Store: s, Service: svc, Sessions: sessions, Challenges: challenges,
			Pusher: pusher, Log: log, WebOrigin: cfg.WebOrigin,
			Currency: cfg.Currency, Dev: cfg.Dev, TrustProxy: cfg.TrustProxy,
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errs := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.ListenAddr, "base_url", cfg.BaseURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
