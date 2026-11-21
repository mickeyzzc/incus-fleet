package httpapi

import (
	"net/http"

	"github.com/mickeyzzc/incus-fleet/internal/answerprofiles"
)

// listAnswerProfiles returns every stored answer configuration.
func (a *API) listAnswerProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := a.answerProfiles.List(r.Context())
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"answer_profiles": profiles})
}

// createAnswerProfile stores a new answer configuration.
func (a *API) createAnswerProfile(w http.ResponseWriter, r *http.Request) {
	var p answerprofiles.AnswerProfile
	if err := decodeJSON(w, r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.answerProfiles.Create(r.Context(), &p); err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, p)
}

// updateAnswerProfile rewrites one answer configuration.
func (a *API) updateAnswerProfile(w http.ResponseWriter, r *http.Request) {
	var p answerprofiles.AnswerProfile
	if err := decodeJSON(w, r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	p.ID = r.PathValue("id")

	if err := a.answerProfiles.Update(r.Context(), &p); err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, p)
}

// deleteAnswerProfile removes one answer configuration.
func (a *API) deleteAnswerProfile(w http.ResponseWriter, r *http.Request) {
	if err := a.answerProfiles.Delete(r.Context(), r.PathValue("id")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
