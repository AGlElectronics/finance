package http

import (
	"net/http"

	"github.com/AGlElectronics/finance/backend/internal/denlink"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// portalTodos serves GET /api/portal/todos. Den's backend calls it with the
// pairing credential, never with the person's token. Not paired: 404. A
// wrong or revoked credential is 401 (denlink.RequireDen). Missing identity
// headers: 400. Finance has no personal to-dos yet, so a known caller gets
// an empty list.
func portalTodos(den *denlink.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if den == nil || !den.Linked() {
			httperror.Render(w, r, httperror.New(httperror.CodeNotFound, "Finance is not paired with Den"))
			return
		}
		den.RequireDen(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := denlink.PortalCallerFrom(r.Context()); !ok {
				httperror.Render(w, r, httperror.BadRequest("issuer and subject are required"))
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		})).ServeHTTP(w, r)
	}
}
