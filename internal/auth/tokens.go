package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Errors returned by the API token subsystem.
var (
	// ErrInvalidAPIToken is returned for a missing, unknown, or revoked token.
	ErrInvalidAPIToken = errors.New("auth: invalid api token")
	// ErrTokenExpired is returned when a token is past its expiry.
	ErrTokenExpired = errors.New("auth: api token expired")
	// ErrTokenNotFound is returned when a token id does not exist.
	ErrTokenNotFound = errors.New("auth: api token not found")
	// ErrInvalidTokenRequest is returned for malformed creation input.
	ErrInvalidTokenRequest = errors.New("auth: invalid api token request")
)

// Scope names one grantable capability on a resource.
type Scope string

// Granular scopes. Reads and writes are separate so a polling plugin never gets
// mutation rights by accident.
const (
	ScopeReadClusters    Scope = "read:clusters"
	ScopeWriteClusters   Scope = "write:clusters"
	ScopeReadInstances   Scope = "read:instances"
	ScopeWriteInstances  Scope = "write:instances"
	ScopeReadAgents      Scope = "read:agents"
	ScopeWriteAgents     Scope = "write:agents"
	ScopeReadMetrics     Scope = "read:metrics"
	ScopeReadAudit       Scope = "read:audit"
	ScopeReadPlugins     Scope = "read:plugins"
	ScopeManagePlugins   Scope = "manage:plugins"
	ScopeReadUsers       Scope = "read:users"
	ScopeReadOwnTokens   Scope = "read:own-tokens"
	ScopeWriteUsers      Scope = "write:users"
	ScopeReadAssets      Scope = "read:assets"
	ScopeWriteAssets     Scope = "write:assets"
	ScopeReadMigrations  Scope = "read:migrations"
	ScopeWriteMigrations Scope = "write:migrations"
)

// allScopes is the canonical scope list in a stable order.
var allScopes = []Scope{
	ScopeReadClusters, ScopeWriteClusters,
	ScopeReadInstances, ScopeWriteInstances,
	ScopeReadAgents, ScopeWriteAgents,
	ScopeReadMetrics, ScopeReadAudit,
	ScopeReadPlugins, ScopeManagePlugins,
	ScopeReadUsers, ScopeWriteUsers,
	ScopeReadOwnTokens,
	ScopeReadAssets, ScopeWriteAssets,
	ScopeReadMigrations, ScopeWriteMigrations,
}

// AllScopes returns every valid scope.
func AllScopes() []Scope {
	out := make([]Scope, len(allScopes))
	copy(out, allScopes)

	return out
}

// Valid reports whether the scope is one of the known values.
func (s Scope) Valid() bool {
	for _, known := range allScopes {
		if s == known {
			return true
		}
	}

	return false
}

// Implies reports whether holding s also grants want. A write grant implies the
// matching read grant on the same resource; nothing implies another resource.
func (s Scope) Implies(want Scope) bool {
	if s == want {
		return true
	}

	prefix, resource, ok := strings.Cut(string(s), ":")
	if !ok || prefix != "write" {
		return false
	}

	return want == Scope("read:"+resource)
}

// Scopes is a grantable scope set.
type Scopes []Scope

// Contains reports whether the set grants want, directly or by implication.
func (s Scopes) Contains(want Scope) bool {
	for _, have := range s {
		if have.Implies(want) {
			return true
		}
	}

	return false
}

// Strings renders the scopes for JSON responses.
func (s Scopes) Strings() []string {
	out := make([]string, 0, len(s))
	for _, scope := range s {
		out = append(out, string(scope))
	}

	return out
}

// SubsetOf reports whether every scope in s is granted by other. It is used to
// stop an account from minting a token more powerful than itself.
func (s Scopes) SubsetOf(other Scopes) bool {
	for _, want := range s {
		if !other.Contains(want) {
			return false
		}
	}

	return true
}

// APIToken is one issued token. The plaintext secret is never stored or
// returned after creation; TokenHash is what the store holds.
type APIToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	UserID     string     `json:"user_id"`
	Username   string     `json:"username,omitempty"`
	Scopes     Scopes     `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`

	// PluginID and InstanceID bind a token to one plugin install. Uninstall
	// revokes every token carrying that plugin id.
	PluginID   string `json:"plugin_id,omitempty"`
	InstanceID string `json:"instance_id,omitempty"`

	// TokenHash is the SHA-256 of the secret. It is never serialized.
	TokenHash string `json:"-"`
}

// TokenStore persists API tokens. It is optional: without it no API tokens can
// be issued or accepted.
type TokenStore interface {
	CreateAPIToken(ctx context.Context, token *APIToken) error
	ListAPITokens(ctx context.Context) ([]*APIToken, error)
	ListAPITokensForUser(ctx context.Context, userID string) ([]*APIToken, error)
	GetAPITokenByHash(ctx context.Context, hash string) (*APIToken, error)
	DeleteAPIToken(ctx context.Context, id string) error
	TouchAPIToken(ctx context.Context, id string, at time.Time) error
}

// WithTokenStore enables API tokens.
func WithTokenStore(store TokenStore) Option {
	return func(a *Authenticator) { a.tokens = store }
}

// CreateTokenRequest is the input for issuing a token.
type CreateTokenRequest struct {
	Name       string
	UserID     string
	Username   string
	Scopes     []Scope
	TTL        time.Duration
	PluginID   string
	InstanceID string
}

// Principal is an authenticated API token caller.
type Principal struct {
	UserID     string
	Username   string
	Scopes     Scopes
	PluginID   string
	InstanceID string
}

// HasScope reports whether the principal may perform a scoped action.
func (p Principal) HasScope(scope Scope) bool {
	return p.Scopes.Contains(scope)
}

// APITokensEnabled reports whether a token store is configured.
func (a *Authenticator) APITokensEnabled() bool { return a.tokens != nil }

// CreateAPIToken issues a token and returns the one-time plaintext secret.
func (a *Authenticator) CreateAPIToken(ctx context.Context, req CreateTokenRequest) (*APIToken, string, error) {
	if a.tokens == nil {
		return nil, "", fmt.Errorf("%w: api tokens are not configured", ErrInvalidTokenRequest)
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, "", fmt.Errorf("%w: name is required", ErrInvalidTokenRequest)
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, "", fmt.Errorf("%w: user id is required", ErrInvalidTokenRequest)
	}
	if len(req.Scopes) == 0 {
		return nil, "", fmt.Errorf("%w: at least one scope is required", ErrInvalidTokenRequest)
	}
	if req.TTL < 0 {
		return nil, "", fmt.Errorf("%w: ttl must not be negative", ErrInvalidTokenRequest)
	}
	for _, scope := range req.Scopes {
		if !scope.Valid() {
			return nil, "", fmt.Errorf("%w: unknown scope %q", ErrInvalidTokenRequest, scope)
		}
	}

	secret, err := newTokenSecret()
	if err != nil {
		return nil, "", err
	}

	now := a.now().UTC()
	token := &APIToken{
		ID:         newTokenID(),
		Name:       strings.TrimSpace(req.Name),
		UserID:     req.UserID,
		Username:   req.Username,
		Scopes:     append(Scopes(nil), req.Scopes...),
		CreatedAt:  now,
		PluginID:   req.PluginID,
		InstanceID: req.InstanceID,
		TokenHash:  HashAPIToken(secret),
	}
	if req.TTL > 0 {
		expires := now.Add(req.TTL)
		token.ExpiresAt = &expires
	}

	if err := a.tokens.CreateAPIToken(ctx, token); err != nil {
		return nil, "", err
	}

	return token, secret, nil
}

// AuthenticateAPIToken resolves a bearer secret to a principal. Expired and
// revoked tokens are rejected; last use is recorded for audit.
func (a *Authenticator) AuthenticateAPIToken(ctx context.Context, secret string) (Principal, error) {
	if a.tokens == nil || strings.TrimSpace(secret) == "" {
		return Principal{}, ErrInvalidAPIToken
	}

	token, err := a.tokens.GetAPITokenByHash(ctx, HashAPIToken(secret))
	if err != nil {
		if errors.Is(err, ErrTokenNotFound) {
			return Principal{}, ErrInvalidAPIToken
		}

		return Principal{}, err
	}

	now := a.now().UTC()
	if token.ExpiresAt != nil && !now.Before(*token.ExpiresAt) {
		return Principal{}, ErrTokenExpired
	}

	if err := a.tokens.TouchAPIToken(ctx, token.ID, now); err != nil {
		// A failed touch must not deny service; the request itself is authorized.
		_ = err
	}

	return Principal{
		UserID:     token.UserID,
		Username:   token.Username,
		Scopes:     token.Scopes,
		PluginID:   token.PluginID,
		InstanceID: token.InstanceID,
	}, nil
}

// RevokeAPIToken deletes one token by id.
func (a *Authenticator) RevokeAPIToken(ctx context.Context, id string) error {
	if a.tokens == nil {
		return ErrTokenNotFound
	}

	return a.tokens.DeleteAPIToken(ctx, id)
}

// RevokePluginTokens deletes every token bound to a plugin install.
func (a *Authenticator) RevokePluginTokens(ctx context.Context, pluginID string) error {
	tokens, err := a.tokens.ListAPITokens(ctx)
	if err != nil {
		return err
	}

	for _, token := range tokens {
		if token.PluginID == pluginID {
			if err := a.tokens.DeleteAPIToken(ctx, token.ID); err != nil && !errors.Is(err, ErrTokenNotFound) {
				return err
			}
		}
	}

	return nil
}

// ListAPITokens returns every token with the hash stripped so it is safe to
// serialize.
func (a *Authenticator) ListAPITokens(ctx context.Context) ([]*APIToken, error) {
	if a.tokens == nil {
		return nil, nil
	}

	tokens, err := a.tokens.ListAPITokens(ctx)
	if err != nil {
		return nil, err
	}
	for _, token := range tokens {
		token.TokenHash = ""
	}

	return tokens, nil
}

// ListAPITokensForUser returns one user's tokens with the hash stripped.
func (a *Authenticator) ListAPITokensForUser(ctx context.Context, userID string) ([]*APIToken, error) {
	if a.tokens == nil {
		return nil, nil
	}

	tokens, err := a.tokens.ListAPITokensForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, token := range tokens {
		token.TokenHash = ""
	}

	return tokens, nil
}

// HashAPIToken derives the stored form of a secret. SHA-256 is sufficient here:
// the secret is 256 bits of CSPRNG output, so there is nothing to brute-force.
func HashAPIToken(secret string) string {
	sum := sha256.Sum256([]byte(secret))

	return hex.EncodeToString(sum[:])
}

// newTokenSecret returns a URL-safe random secret with a recognisable prefix so
// leaked strings can be scanned for.
func newTokenSecret() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}

	return "fleet_pat_" + base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func newTokenID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failing means the process cannot mint identities at all.
		panic(fmt.Sprintf("auth: generate token id: %v", err))
	}

	return base64.RawURLEncoding.EncodeToString(buf[:])
}
