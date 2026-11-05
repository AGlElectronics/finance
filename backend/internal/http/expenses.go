package http

import (
	"net/http"

	"github.com/AGlElectronics/finance/backend/internal/httperror"
	"github.com/AGlElectronics/finance/backend/internal/service"
)

type expenseHandler struct {
	svc    *service.ExpenseService
	ledger *service.LedgerService
}

func (h *expenseHandler) list(w http.ResponseWriter, r *http.Request) {
	expenses, err := h.svc.List(r.Context(), r.URL.Query().Get("category"))
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"expenses": expenses})
}

func (h *expenseHandler) create(w http.ResponseWriter, r *http.Request) {
	var in service.ExpenseInput
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	e, err := h.svc.Create(r.Context(), actorFrom(r), in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (h *expenseHandler) get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	e, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (h *expenseHandler) update(w http.ResponseWriter, r *http.Request) {
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
	e, err := h.svc.Update(r.Context(), actorFrom(r), id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (h *expenseHandler) remove(w http.ResponseWriter, r *http.Request) {
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

func (h *expenseHandler) ledgerGet(w http.ResponseWriter, r *http.Request) {
	l, err := h.ledger.Get(r.Context())
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}
