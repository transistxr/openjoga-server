// Package config reads every variable from the environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Dev bool

	TrustProxy bool

	ListenAddr string
	BaseURL    string // for links we email out
	DBPath     string
	WebOrigin  string // for CORS

	// CodePepper keys the HMAC over short verification codes.
	CodePepper []byte

	SMTPHost  string
	SMTPPort  int
	SMTPUser  string
	SMTPPass  string
	EmailFrom string

	// TODO: Push notifications over web
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string

	Currency string
}

func (c Config) PushEnabled() bool {
	return c.VAPIDPublicKey != "" && c.VAPIDPrivateKey != ""
}

func (c Config) SMTPEnabled() bool {
	return c.SMTPHost != ""
}

func Load() (Config, error) {
	c := Config{
		Dev:        env("OPENJOGA_DEV", "") != "",
		TrustProxy: env("OPENJOGA_TRUST_PROXY", "") != "",
		ListenAddr: env("OPENJOGA_LISTEN", ":8080"),
		BaseURL:    strings.TrimRight(env("OPENJOGA_BASE_URL", "http://localhost:8080"), "/"),
		DBPath:     env("OPENJOGA_DB", "openjoga.db"),
		WebOrigin:  env("OPENJOGA_WEB_ORIGIN", "http://localhost:5173"),

		SMTPHost:  env("OPENJOGA_SMTP_HOST", ""),
		SMTPUser:  env("OPENJOGA_SMTP_USER", ""),
		SMTPPass:  env("OPENJOGA_SMTP_PASS", ""),
		EmailFrom: env("OPENJOGA_EMAIL_FROM", "openjoga@localhost"),

		VAPIDPublicKey:  env("OPENJOGA_VAPID_PUBLIC", ""),
		VAPIDPrivateKey: env("OPENJOGA_VAPID_PRIVATE", ""),
		VAPIDSubject:    env("OPENJOGA_VAPID_SUBJECT", "mailto:admin@localhost"),

		Currency: env("OPENJOGA_CURRENCY", "GBP"),
	}

	port, err := strconv.Atoi(env("OPENJOGA_SMTP_PORT", "587"))
	if err != nil {
		return c, fmt.Errorf("OPENJOGA_SMTP_PORT: %w", err)
	}
	c.SMTPPort = port

	pepper := env("OPENJOGA_CODE_PEPPER", "")
	switch {
	case pepper != "":
		c.CodePepper = []byte(pepper)
	case c.Dev:
		c.CodePepper = []byte("dev-pepper-not-for-production")
	default:
		return c, errors.New("OPENJOGA_CODE_PEPPER is required (set OPENJOGA_DEV=1 for local development)")
	}

	return c, nil
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
