package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"openjoga-server/internal/auth"
	"openjoga-server/internal/service"
	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writing response", "error", err)
	}
}

func (s *Server) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	status, message := classify(err)
	if status == http.StatusInternalServerError {
		s.log.Error("request failed",
			"method", r.Method, "path", r.URL.Path,
			"request_id", requestID(r.Context()), "error", err)
	}
	writeJSON(w, status, errorBody{Error: message})
}

func classify(err error) (int, string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, text.ErrNotFound

	case errors.Is(err, store.ErrContactTaken):
		return http.StatusConflict, text.ErrContactTaken

	case errors.Is(err, auth.ErrNeedAccount):
		return http.StatusUnauthorized, auth.ErrNeedAccount.Error()
	case errors.Is(err, auth.ErrNeedContact):
		return http.StatusForbidden, auth.ErrNeedContact.Error()
	case errors.Is(err, auth.ErrForbidden), errors.Is(err, service.ErrNotAllowed):
		return http.StatusForbidden, text.ErrNotAllowed

	case errors.Is(err, auth.ErrBadCode), errors.Is(err, auth.ErrCodeExpired):
		return http.StatusUnauthorized, err.Error()
	case errors.Is(err, auth.ErrTooMany), errors.Is(err, auth.ErrRateLimited):
		return http.StatusTooManyRequests, err.Error()
	case errors.Is(err, auth.ErrBadEmail):
		return http.StatusBadRequest, err.Error()

	case errors.Is(err, service.ErrClosed), errors.Is(err, service.ErrFull):
		return http.StatusConflict, err.Error()

	case errors.Is(err, service.ErrGuestLimit),
		errors.Is(err, service.ErrSponsorCap),
		errors.Is(err, service.ErrNoSponsored),
		errors.Is(err, service.ErrInviteUnusable),
		errors.Is(err, service.ErrClaimLinkUnusable),
		errors.Is(err, service.ErrPaymentNeedsGate):
		return http.StatusUnprocessableEntity, err.Error()

	case errors.Is(err, service.ErrNeedReference), errors.Is(err, service.ErrBadReference):
		return http.StatusUnprocessableEntity, err.Error()

	case errors.Is(err, service.ErrInvalid):
		return http.StatusBadRequest, err.Error()

	default:
		return http.StatusInternalServerError, text.ErrServerFault
	}
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return service.ErrInvalid
	}
	return nil
}
