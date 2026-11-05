package http

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/AGlElectronics/finance/backend/internal/httperror"
	"github.com/AGlElectronics/finance/backend/internal/service"
)

type invoiceHandler struct {
	svc  *service.InvoiceService
	dash *service.DashboardService
}

func (h *invoiceHandler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var customerID *uuid.UUID
	if raw := q.Get("customer_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httperror.Render(w, r, httperror.BadRequest("invalid customer_id"))
			return
		}
		customerID = &id
	}
	invoices, err := h.svc.List(r.Context(), q.Get("status"), customerID)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invoices": invoices})
}

func (h *invoiceHandler) create(w http.ResponseWriter, r *http.Request) {
	var in service.InvoiceInput
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	inv, err := h.svc.Create(r.Context(), actorFrom(r), in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

func (h *invoiceHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	inv, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (h *invoiceHandler) update(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in service.InvoiceInput
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	inv, err := h.svc.UpdateDraft(r.Context(), actorFrom(r), id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (h *invoiceHandler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if err := h.svc.Delete(r.Context(), actorFrom(r), id); err != nil {
		httperror.Render(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *invoiceHandler) issue(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	in := service.IssueInput{}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &in); err != nil {
			httperror.Render(w, r, err)
			return
		}
	}
	inv, err := h.svc.Issue(r.Context(), actorFrom(r), id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (h *invoiceHandler) void(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	inv, err := h.svc.Void(r.Context(), actorFrom(r), id)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (h *invoiceHandler) addPayment(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in service.PaymentInput
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	inv, err := h.svc.AddPayment(r.Context(), actorFrom(r), id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

func (h *invoiceHandler) dashboard(w http.ResponseWriter, r *http.Request) {
	d, err := h.dash.Get(r.Context())
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
