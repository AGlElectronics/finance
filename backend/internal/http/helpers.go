package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AGlElectronics/finance/backend/internal/actor"
	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/authbase"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
	"github.com/AGlElectronics/finance/backend/internal/service"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

var decodeOptions = authbase.DecodeOptions{DisallowUnknownFields: true}

// decodeJSON reads one JSON value (at most authbase.DefaultMaxBodyBytes)
// into dst, rejecting unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return asHTTPError(authbase.DecodeJSON(w, r, dst, decodeOptions))
}

// readBody reads the capped body and puts it back for DecodeJSON.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, authbase.DefaultMaxBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, httperror.New(httperror.CodeTooLarge, fmt.Sprintf("request body is larger than %d bytes", authbase.DefaultMaxBodyBytes))
		}
		return nil, httperror.BadRequest("could not read the request body")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return raw, nil
}

// decodeIdem reads the body into dst and builds the idempotency identity
// from the Idempotency-Key header, the method and path, and the body.
func decodeIdem(w http.ResponseWriter, r *http.Request, dst any) (service.Idem, error) {
	raw, err := readBody(w, r)
	if err != nil {
		return service.Idem{}, err
	}
	if err := decodeJSON(w, r, dst); err != nil {
		return service.Idem{}, err
	}
	return service.NewIdem(r.Header.Get("Idempotency-Key"), r.Method+" "+r.URL.Path, raw), nil
}

// decodeOptional decodes a body only when one was sent.
func decodeOptional(w http.ResponseWriter, r *http.Request, dst any) error {
	raw, err := readBody(w, r)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return decodeJSON(w, r, dst)
}

// asHTTPError turns an authbase decode error into the API error type.
func asHTTPError(err error) error {
	if err == nil {
		return nil
	}
	var de *authbase.DecodeError
	if errors.As(err, &de) {
		return httperror.New(httperror.Code(de.Code), de.Message).WithCause(err)
	}
	return httperror.BadRequest("invalid JSON body")
}

func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, httperror.BadRequest("invalid " + name)
	}
	return id, nil
}

func parsePage(r *http.Request) (service.Page, error) {
	var p service.Page
	q := r.URL.Query()
	for name, dst := range map[string]*int{"limit": &p.Limit, "offset": &p.Offset} {
		if raw := q.Get(name); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v < 0 {
				return p, httperror.BadRequest(name + " must be a whole number of 0 or more")
			}
			*dst = v
		}
	}
	return p, nil
}

func who(r *http.Request) audit.Actor {
	a, _ := actor.FromContext(r.Context())
	return a.Audit()
}

// pendingBody is the 202 answer when a request waits for a second person.
type pendingBody struct {
	Status   string           `json:"status"`
	Approval service.Approval `json:"approval"`
}

// writeResult renders a create or money request: 202 with the approval when
// it waits for a second person, else status with what render returns. A
// replayed Idempotency-Key gets the same answer plus Idempotent-Replayed.
func writeResult(w http.ResponseWriter, r *http.Request, svc *service.Service, res service.Result, status int, render func() (any, error)) {
	if res.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	if res.Pending {
		a, err := svc.GetApproval(r.Context(), res.ID)
		if err != nil {
			httperror.Render(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, pendingBody{Status: "pending_approval", Approval: a})
		return
	}
	body, err := render()
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, status, body)
}

func writePDF(w http.ResponseWriter, r *http.Request, name string, body []byte, err error) {
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
