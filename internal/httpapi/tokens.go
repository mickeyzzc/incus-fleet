package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
)

// apiTokenResponse never includes the hash or the plaintext secret.
type apiTokenResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	UserID     string     `json:"user_id"`
	Username   string     `json:"username,omitempty"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	PluginID   string     `json:"plugin_id,omitempty"`
	InstanceID string     `json:"instance_id,omitempty"`
}

// createdTokenResponse adds the one-time secret, shown exactly once.
type createdTokenResponse struct {
	apiTokenResponse
	Token string `json:"token"`
}

type createTokenRequest struct {
	Name       string   `json:"name"`
	UserID     string   `json:"user_id"`
	Scopes     []string `json:"scopes"`
	TTLSeconds int      `json:"ttl_seconds"`
	PluginID   string   `json:"plugin_id"`
	InstanceID string   `json:"instance_id"`
}

func toTokenResponse(token *auth.APIToken) apiTokenResponse {
	return apiTokenResponse{
		ID:         token.ID,
		Name:       token.Name,
		UserID:     token.UserID,
		Username:   token.Username,
		Scopes:     token.Scopes.Strings(),
		CreatedAt:  token.CreatedAt,
		ExpiresAt:  token.ExpiresAt,
		LastUsedAt: token.LastUsedAt,
		PluginID:   token.PluginID,
		InstanceID: token.InstanceID,
	}
}

// listAPITokens returns tokens. Administrators see every token; other accounts
// see only their own.
func (a *API) listAPITokens(w http.ResponseWriter, r *http.Request) {
	if !a.auth.APITokensEnabled() {
		writeError(w, http.StatusServiceUnavailable, "tokens_disabled", "api tokens are not configured")
		return
	}

	session, _ := sessionFromContext(r.Context())

	var (
		tokens []*auth.APIToken
		err    error
	)
	if session.Role.CanAdmin() {
		tokens, err = a.auth.ListAPITokens(r.Context())
	} else {
		tokens, err = a.auth.ListAPITokensForUser(r.Context(), session.UserID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not list tokens")
		return
	}

	out := make([]apiTokenResponse, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, toTokenResponse(token))
	}

	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

// createAPIToken issues a token and returns the secret once.
func (a *API) createAPIToken(w http.ResponseWriter, r *http.Request) {
	if !a.auth.APITokensEnabled() {
		writeError(w, http.StatusServiceUnavailable, "tokens_disabled", "api tokens are not configured")
		return
	}

	var req createTokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return
	}

	session, _ := sessionFromContext(r.Context())
	userID := session.UserID
	// Only an administrator may mint tokens for another account.
	if req.UserID != "" && req.UserID != session.UserID {
		if !session.Role.CanAdmin() {
			writeError(w, http.StatusForbidden, "forbidden", "administrator role required to issue tokens for other accounts")
			return
		}
		userID = req.UserID
	}

	scopes := make([]auth.Scope, 0, len(req.Scopes))
	for _, scope := range req.Scopes {
		scopes = append(scopes, auth.Scope(scope))
	}

	var ttl time.Duration
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}

	// Non-administrators may never mint a scope beyond what they hold.
	if !session.Role.CanAdmin() {
		granted := auth.Scopes(scopes)
		if !granted.SubsetOf(sessionScopes(session)) {
			writeError(w, http.StatusForbidden, "forbidden", "requested scopes exceed your own permissions")
			return
		}
	}

	token, secret, err := a.auth.CreateAPIToken(r.Context(), auth.CreateTokenRequest{
		Name:       req.Name,
		UserID:     userID,
		Username:   session.Username,
		Scopes:     scopes,
		TTL:        ttl,
		PluginID:   req.PluginID,
		InstanceID: req.InstanceID,
	})
	if err != nil {
		if errors.Is(err, auth.ErrInvalidTokenRequest) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "could not create token")

		return
	}

	writeJSON(w, http.StatusCreated, createdTokenResponse{
		apiTokenResponse: toTokenResponse(token),
		Token:            secret,
	})
}

// revokeAPIToken deletes a token. Administrators may revoke any token; other
// accounts only their own.
func (a *API) revokeAPIToken(w http.ResponseWriter, r *http.Request) {
	if !a.auth.APITokensEnabled() {
		writeError(w, http.StatusServiceUnavailable, "tokens_disabled", "api tokens are not configured")
		return
	}

	id := r.PathValue("id")
	session, _ := sessionFromContext(r.Context())

	if !session.Role.CanAdmin() {
		own, err := a.auth.ListAPITokensForUser(r.Context(), session.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "could not verify token ownership")
			return
		}
		found := false
		for _, token := range own {
			if token.ID == id {
				found = true

				break
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, "not_found", "token not found")
			return
		}
	}

	if err := a.auth.RevokeAPIToken(r.Context(), id); err != nil {
		if errors.Is(err, auth.ErrTokenNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "token not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "could not revoke token")

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// availableScopes reports the scope vocabulary so the UI can render it.
func (a *API) availableScopes(w http.ResponseWriter, _ *http.Request) {
	scopes := auth.AllScopes()
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, string(scope))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"scopes":  out,
		"enabled": a.auth.APITokensEnabled(),
	})
}

// sessionScopes is the scope set implied by a session role. A full role implies
// its whole slice of the vocabulary, which keeps role sessions and token
// sessions on one authorization model.
func sessionScopes(session auth.Session) auth.Scopes {
	switch session.Role {
	case auth.RoleAdmin:
		return auth.Scopes(auth.AllScopes())
	case auth.RoleOperator:
		return auth.Scopes{
			auth.ScopeReadClusters, auth.ScopeWriteClusters,
			auth.ScopeReadInstances, auth.ScopeWriteInstances,
			auth.ScopeReadAgents, auth.ScopeWriteAgents,
			auth.ScopeReadMetrics, auth.ScopeReadAudit,
			auth.ScopeReadPlugins,
			auth.ScopeReadAssets, auth.ScopeWriteAssets,
			auth.ScopeReadMigrations, auth.ScopeWriteMigrations,
			auth.ScopeReadOwnTokens,
		}
	default:
		return auth.Scopes{
			auth.ScopeReadClusters, auth.ScopeReadInstances, auth.ScopeReadAgents,
			auth.ScopeReadMetrics, auth.ScopeReadAudit, auth.ScopeReadPlugins,
			auth.ScopeReadAssets, auth.ScopeReadMigrations, auth.ScopeReadOwnTokens,
		}
	}
}

// requireScope authorizes a request by API token scope or, for browser
// sessions, by the scope implied by the account role.
func (a *API) requireScope(scope auth.Scope, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A bearer token takes precedence: it is the machine-caller path.
		if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			principal, err := a.auth.AuthenticateAPIToken(r.Context(), strings.TrimSpace(bearer))
			if err != nil {
				if errors.Is(err, auth.ErrTokenExpired) {
					writeError(w, http.StatusUnauthorized, "token_expired", "api token expired")
					return
				}
				// Fall through to the bootstrap token check below so an existing
				// bootstrap deployment keeps working.
				if !(a.auth.ValidateToken(strings.TrimSpace(bearer)) && a.auth.BootstrapAllowed(r.Context())) {
					writeError(w, http.StatusUnauthorized, "unauthorized", "invalid api token")
					return
				}
			} else {
				if !principal.HasScope(scope) {
					writeError(w, http.StatusForbidden, "forbidden", "token lacks scope "+string(scope))
					return
				}
				next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), principal)))

				return
			}
		}

		session, ok := a.authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		if !sessionScopes(session).Contains(scope) {
			writeError(w, http.StatusForbidden, "forbidden", "this account lacks scope "+string(scope))
			return
		}

		next.ServeHTTP(w, r.WithContext(withSession(r.Context(), session)))
	})
}

// ctxPrincipalKey carries an authenticated API token principal.
type ctxPrincipalKey struct{}

func withPrincipal(ctx context.Context, p auth.Principal) context.Context {
	return context.WithValue(ctx, ctxPrincipalKey{}, p)
}

func withSession(ctx context.Context, session auth.Session) context.Context {
	return context.WithValue(ctx, ctxSessionKey{}, session)
}
