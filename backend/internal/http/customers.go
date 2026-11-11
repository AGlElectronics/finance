package http

import (
	"net/http"

	"github.com/AGlElectronics/finance/backend/internal/httperror"
	"github.com/AGlElectronics/finance/backend/internal/service"
)

type handler struct {
	svc *service.Service
}

func (h *handler) listCustomers(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	customers, err := h.svc.ListCustomers(r.Context(), includeArchived)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"customers": customers})
}

func (h *handler) createCustomer(w http.ResponseWriter, r *http.Request) {
	var in service.CustomerInput
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	c, err := h.svc.CreateCustomer(r.Context(), who(r), in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (h *handler) getCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	c, err := h.svc.GetCustomer(r.Context(), id)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *handler) updateCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	var in service.CustomerInput
	if err := decodeJSON(r, &in); err != nil {
		httperror.Render(w, r, err)
		return
	}
	c, err := h.svc.UpdateCustomer(r.Context(), who(r), id, in)
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *handler) deleteCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		httperror.Render(w, r, err)
		return
	}
	if err := h.svc.DeleteCustomer(r.Context(), who(r), id); err != nil {
		httperror.Render(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
