package http

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/AGlElectronics/finance/backend/internal/httperror"
	"github.com/AGlElectronics/finance/backend/internal/service"
)

func (h *handler) listInvoices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, err := parsePage(r)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	f := service.InvoiceFilter{Status: q.Get("status"), Page: page}
	if raw := q.Get("customer_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httperror.Render(w, r, httperror.BadRequest("invalid customer_id"))
			return
		}
		f.CustomerID = &id
	}
	out, err := h.svc.ListInvoices(r.Context(), f)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handler) createInvoice(w http.ResponseWriter, r *http.Request) {
	var in service.InvoiceInput
	idem, err := decodeIdem(r, &in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	res, err := h.svc.CreateInvoice(r.Context(), who(r), idem, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeResult(w, r, h.svc, res, http.StatusCreated, func() (any, error) { return h.svc.GetInvoice(r.Context(), res.ID) })
}

func (h *handler) getInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	inv, err := h.svc.GetInvoice(r.Context(), id)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (h *handler) invoicePDF(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	name, body, err := h.svc.InvoicePDF(r.Context(), id)
	writePDF(w, r, name, body, err)
}

func (h *handler) updateInvoice(w http.ResponseWriter, r *http.Request) {
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
	inv, err := h.svc.UpdateDraft(r.Context(), who(r), id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (h *handler) deleteInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if err := h.svc.DeleteDraft(r.Context(), who(r), id); err != nil {
		httperror.Render(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) issueInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in service.IssueInput
	if err := decodeOptional(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	inv, err := h.svc.IssueInvoice(r.Context(), who(r), id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (h *handler) voidInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in service.VoidInput
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	res, err := h.svc.RequestVoid(r.Context(), who(r), id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeResult(w, r, h.svc, res, http.StatusOK, func() (any, error) { return h.svc.GetInvoice(r.Context(), id) })
}

func (h *handler) addPayment(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in service.PaymentInput
	idem, err := decodeIdem(r, &in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	res, err := h.svc.RecordPayment(r.Context(), who(r), idem, id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeResult(w, r, h.svc, res, http.StatusCreated, func() (any, error) { return h.svc.GetInvoice(r.Context(), id) })
}

func (h *handler) createCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in service.CreditNoteInput
	idem, err := decodeIdem(r, &in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	res, err := h.svc.RequestCreditNote(r.Context(), who(r), idem, id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeResult(w, r, h.svc, res, http.StatusCreated, func() (any, error) { return h.svc.GetCreditNote(r.Context(), res.ID) })
}

func (h *handler) getCreditNote(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	c, err := h.svc.GetCreditNote(r.Context(), id)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *handler) creditNotePDF(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	name, body, err := h.svc.CreditNotePDF(r.Context(), id)
	writePDF(w, r, name, body, err)
}

func (h *handler) createRefund(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in service.RefundInput
	idem, err := decodeIdem(r, &in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	res, err := h.svc.RequestRefund(r.Context(), who(r), idem, id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeResult(w, r, h.svc, res, http.StatusCreated, func() (any, error) { return h.svc.GetCreditNote(r.Context(), id) })
}

func (h *handler) dashboard(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.GetDashboard(r.Context())
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}
