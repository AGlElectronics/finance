package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AGlElectronics/finance/backend/internal/access"
	"github.com/AGlElectronics/finance/backend/internal/actor"
	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
	"github.com/AGlElectronics/finance/backend/internal/service"
)

func (h *handler) listApprovals(w http.ResponseWriter, r *http.Request) {
	page, err := parsePage(r)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	out, err := h.svc.ListApprovals(r.Context(), service.ApprovalFilter{Status: r.URL.Query().Get("status"), Page: page})
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handler) getApproval(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	a, err := h.svc.GetApproval(r.Context(), id)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *handler) approve(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in service.DecisionInput
	if err := decodeOptional(w, r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	a, err := h.svc.Approve(r.Context(), who(r), id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// reject needs finance.approve, except that requesters may withdraw their
// own requests.
func (h *handler) reject(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	a, _ := actor.FromContext(r.Context())
	if !a.Can(access.Approve) {
		requester, err := h.svc.RequesterOf(r.Context(), id)
		if err != nil {
			httperror.Render(w, r, err)
			return
		}
		if !requester.SamePerson(a.Audit()) {
			httperror.Render(w, r, httperror.Forbidden("this needs the permission "+string(access.Approve)))
			return
		}
	}
	var in service.DecisionInput
	if err := decodeJSON(w, r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	out, err := h.svc.Reject(r.Context(), who(r), id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handler) listPeriods(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListPeriods(r.Context())
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"closed": out})
}

func (h *handler) closePeriod(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.ClosePeriod(r.Context(), who(r), chi.URLParam(r, "month"))
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *handler) reopenPeriod(w http.ResponseWriter, r *http.Request) {
	var in service.ReopenInput
	if err := decodeJSON(w, r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	if err := h.svc.ReopenPeriod(r.Context(), who(r), chi.URLParam(r, "month"), in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) getSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.GetSettings(r.Context())
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in service.SettingsInput
	if err := decodeJSON(w, r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	s, err := h.svc.UpdateSettings(r.Context(), who(r), in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *handler) auditLog(w http.ResponseWriter, r *http.Request) {
	page, err := parsePage(r)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	q := r.URL.Query()
	f := service.AuditFilter{EntityType: q.Get("entity_type"), Action: q.Get("action"), Actor: q.Get("actor"), Page: page}
	if raw := q.Get("entity_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httperror.Render(w, r, httperror.BadRequest("invalid entity_id"))
			return
		}
		f.EntityID = &id
	}
	out, err := h.svc.AuditLog(r.Context(), f)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// statusRecorder remembers the status a wrapped handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// audited records action in the audit log after next succeeded. It wraps
// the Den pairing handlers, which live in the shared denlink package and
// write no audit events themselves.
func audited(w *audit.Writer, action string, next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: rw, status: http.StatusOK}
		next(rec, r)
		if rec.status >= 300 {
			return
		}
		_ = w.RecordNow(r.Context(), who(r), audit.Event{Action: action, EntityType: "den_link", EntityID: uuid.Nil})
	}
}
