package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// userRequest is the payload for creating or updating an account.
type userRequest struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	Role               string `json:"role"`
	Disabled           *bool  `json:"disabled,omitempty"`
	MustChangePassword *bool  `json:"must_change_password,omitempty"`
}

// userResponse is the client-facing account shape. The password hash never
// leaves the server.
type userResponse struct {
	ID                 string    `json:"id"`
	Username           string    `json:"username"`
	Role               string    `json:"role"`
	Disabled           bool      `json:"disabled"`
	MustChangePassword bool      `json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func toUserResponse(user *store.User) userResponse {
	return userResponse{
		ID:                 user.ID,
		Username:           user.Username,
		Role:               user.Role,
		Disabled:           user.Disabled,
		MustChangePassword: user.MustChangePassword,
		CreatedAt:          user.CreatedAt,
		UpdatedAt:          user.UpdatedAt,
	}
}

// listUsers returns every local account. Administrators only.
func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.store.ListUsers(r.Context())
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	out := make([]userResponse, 0, len(users))
	for _, user := range users {
		out = append(out, toUserResponse(user))
	}

	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// createUser adds an account. It also serves as the first-run bootstrap: while
// no account exists, the shared token may create the initial administrator.
func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var req userRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// The shared bootstrap token may create accounts only until the first one
	// exists. Afterwards account creation requires the administrator role.
	session, _ := sessionFromContext(r.Context())
	if session.UserID == "bootstrap" {
		users, err := a.store.ListUsers(r.Context())
		if err != nil {
			a.serviceError(w, r, err)
			return
		}
		if len(users) > 0 {
			writeError(w, http.StatusForbidden, "forbidden", "the bootstrap token cannot create more accounts")
			return
		}
	} else if !session.Role.CanAdmin() {
		writeError(w, http.StatusForbidden, "forbidden", "administrator role required")
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "username is required")
		return
	}

	role := auth.Role(strings.TrimSpace(req.Role))
	if role == "" {
		role = auth.RoleViewer
	}
	if !role.Valid() {
		writeError(w, http.StatusBadRequest, "invalid_request", "role must be admin, operator, or viewer")
		return
	}

	hash, err := a.auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}

	now := time.Now().UTC()
	user := &store.User{
		ID:           store.NewID(),
		Username:     strings.ToLower(username),
		PasswordHash: hash,
		Role:         string(role),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if req.Disabled != nil {
		user.Disabled = *req.Disabled
	}
	if req.MustChangePassword != nil {
		user.MustChangePassword = *req.MustChangePassword
	}

	if err := a.store.CreateUser(r.Context(), user); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "username already exists")
			return
		}
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, toUserResponse(user))
}

// updateUser changes an account's role, disabled flag, or password.
func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	var req userRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	id := r.PathValue("id")
	user, err := a.store.GetUser(r.Context(), id)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	if role := strings.TrimSpace(req.Role); role != "" {
		parsed := auth.Role(role)
		if !parsed.Valid() {
			writeError(w, http.StatusBadRequest, "invalid_request", "role must be admin, operator, or viewer")
			return
		}
		if user.Role == string(auth.RoleAdmin) && parsed != auth.RoleAdmin {
			if ok, err := a.wouldRemoveLastAdmin(r, user); err != nil {
				a.serviceError(w, r, err)
				return
			} else if ok {
				writeError(w, http.StatusConflict, "conflict", "cannot demote the last administrator")
				return
			}
		}
		user.Role = string(parsed)
	}

	if req.Disabled != nil {
		if *req.Disabled && user.Role == string(auth.RoleAdmin) {
			if ok, err := a.wouldRemoveLastAdmin(r, user); err != nil {
				a.serviceError(w, r, err)
				return
			} else if ok {
				writeError(w, http.StatusConflict, "conflict", "cannot disable the last administrator")
				return
			}
		}
		user.Disabled = *req.Disabled
	}

	if req.Password != "" {
		hash, err := a.auth.HashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, "weak_password", err.Error())
			return
		}
		user.PasswordHash = hash
		user.MustChangePassword = true
	}
	if req.MustChangePassword != nil {
		user.MustChangePassword = *req.MustChangePassword
	}

	// A role or disable change should not keep a stale lockout around.
	if req.Disabled != nil || req.Role != "" {
		user.FailedAttempts = 0
		user.LockedUntil = time.Time{}
	}

	if err := a.store.UpdateUser(r.Context(), user); err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, toUserResponse(user))
}

// deleteUser removes an account, refusing to remove the last administrator.
func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user, err := a.store.GetUser(r.Context(), id)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	if user.Role == string(auth.RoleAdmin) {
		if ok, err := a.wouldRemoveLastAdmin(r, user); err != nil {
			a.serviceError(w, r, err)
			return
		} else if ok {
			writeError(w, http.StatusConflict, "conflict", "cannot delete the last administrator")
			return
		}
	}

	if err := a.store.DeleteUser(r.Context(), id); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// wouldRemoveLastAdmin reports whether acting on user would leave no enabled
// administrator.
func (a *API) wouldRemoveLastAdmin(r *http.Request, user *store.User) (bool, error) {
	if user.Disabled {
		return false, nil
	}

	count, err := a.store.CountAdmins(r.Context())
	if err != nil {
		return false, err
	}

	return count <= 1, nil
}

// changePassword lets the signed-in user rotate their own password.
func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	session, ok := sessionFromContext(r.Context())
	if !ok || session.UserID == "bootstrap" {
		writeError(w, http.StatusBadRequest, "invalid_request", "bootstrap sessions have no password to change")
		return
	}

	user, err := a.store.GetUser(r.Context(), session.UserID)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	// Re-authenticate before a password change.
	if _, err := a.auth.Login(r.Context(), user.Username, req.Current); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "current password is incorrect")
		return
	}

	hash, err := a.auth.HashPassword(req.New)
	if err != nil {
		writeError(w, http.StatusBadRequest, "weak_password", err.Error())
		return
	}

	user.PasswordHash = hash
	user.MustChangePassword = false
	if err := a.store.UpdateUser(r.Context(), user); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// listSessions returns the caller's own sessions.
func (a *API) listSessions(w http.ResponseWriter, r *http.Request) {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": []map[string]any{{
			"id":         session.ID,
			"username":   session.Username,
			"role":       session.Role,
			"expires_at": session.ExpiresAt,
			"current":    true,
		}},
	})
}
