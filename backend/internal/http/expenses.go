package http

import (
	"net/http"

	"github.com/AGlElectronics/finance/backend/internal/httperror"
	"github.com/AGlElectronics/finance/backend/internal/service"
)

func (h *handler) listExpenses(w http.ResponseWriter, r *http.Request) {
	page, err := parsePage(r)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	q := r.URL.Query()
	out, err := h.svc.ListExpenses(r.Context(), service.ExpenseFilter{
		Category: q.Get("category"), IncludeDeleted: q.Get("include_deleted") == "true", Page: page,
	})
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handler) createExpense(w http.ResponseWriter, r *http.Request) {
	var in service.ExpenseInput
	idem, err := decodeIdem(r, &in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	res, err := h.svc.CreateExpense(r.Context(), who(r), idem, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeResult(w, r, h.svc, res, http.StatusCreated, func() (any, error) { return h.svc.GetExpense(r.Context(), res.ID) })
}

func (h *handler) getExpense(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	e, err := h.svc.GetExpense(r.Context(), id)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (h *handler) updateExpense(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in service.ExpenseInput
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	e, err := h.svc.UpdateExpense(r.Context(), who(r), id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// deleteExpense takes the reason from a JSON body or ?reason=.
func (h *handler) deleteExpense(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	in := service.DeleteInput{Reason: r.URL.Query().Get("reason")}
	if err := decodeOptional(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	if err := h.svc.DeleteExpense(r.Context(), who(r), id, in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) ledger(w http.ResponseWriter, r *http.Request) {
	page, err := parsePage(r)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	l, err := h.svc.GetLedger(r.Context(), page)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}
