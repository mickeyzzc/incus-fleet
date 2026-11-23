package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestScopesStrings(t *testing.T) {
	got := Scopes{ScopeReadClusters, ScopeWriteInstances}.Strings()
	if len(got) != 2 || got[0] != "read:clusters" || got[1] != "write:instances" {
		t.Fatalf("Strings() = %v", got)
	}
}

func TestAPITokensDisabledWithoutStore(t *testing.T) {
	authn := New("bootstrap-token", time.Hour)

	if authn.APITokensEnabled() {
		t.Fatal("APITokensEnabled() = true without a store")
	}
	if _, _, err := authn.CreateAPIToken(context.Background(), CreateTokenRequest{
		Name: "n", UserID: "u", Scopes: []Scope{ScopeReadClusters},
	}); !errors.Is(err, ErrInvalidTokenRequest) {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}
	if err := authn.RevokeAPIToken(context.Background(), "x"); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("RevokeAPIToken() error = %v", err)
	}
	if tokens, err := authn.ListAPITokens(context.Background()); err != nil || tokens != nil {
		t.Fatalf("ListAPITokens() = %v, %v", tokens, err)
	}
	if tokens, err := authn.ListAPITokensForUser(context.Background(), "u"); err != nil || tokens != nil {
		t.Fatalf("ListAPITokensForUser() = %v, %v", tokens, err)
	}
}

func TestAPITokensEnabledWithStore(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)

	if !authn.APITokensEnabled() {
		t.Fatal("APITokensEnabled() = false with a store")
	}
}

func TestListAPITokensForUserFilters(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)
	ctx := context.Background()

	for _, user := range []string{"alice", "bob", "alice"} {
		if _, _, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
			Name: "n", UserID: user, Scopes: []Scope{ScopeReadClusters},
		}); err != nil {
			t.Fatalf("CreateAPIToken(%s) error = %v", user, err)
		}
	}

	alice, err := authn.ListAPITokensForUser(ctx, "alice")
	if err != nil {
		t.Fatalf("ListAPITokensForUser() error = %v", err)
	}
	if len(alice) != 2 {
		t.Fatalf("alice tokens = %d, want 2", len(alice))
	}
	for _, token := range alice {
		if token.TokenHash != "" {
			t.Fatal("ListAPITokensForUser() exposed the hash")
		}
	}
}

// A store failure during the last-use update must not deny an otherwise valid
// request; authorization already succeeded.
func TestAuthenticateAPITokenToleratesTouchFailure(t *testing.T) {
	store := &failingTouchStore{fakeTokenStore: newFakeTokenStore()}
	authn := New("bootstrap-token", time.Hour, WithTokenStore(store))
	ctx := context.Background()

	if _, _, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name: "n", UserID: "u", Scopes: []Scope{ScopeReadClusters},
	}); err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}
	// Recreate through the same store so the secret hash is known.
	token, secret, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name: "n2", UserID: "u", Scopes: []Scope{ScopeReadClusters},
	})
	if err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}
	_ = token

	store.failTouch = true
	if _, err := authn.AuthenticateAPIToken(ctx, secret); err != nil {
		t.Fatalf("AuthenticateAPIToken() error = %v, want nil despite touch failure", err)
	}
}

type failingTouchStore struct {
	*fakeTokenStore
	failTouch bool
}

func (f *failingTouchStore) TouchAPIToken(ctx context.Context, id string, at time.Time) error {
	if f.failTouch {
		return errors.New("touch failed")
	}

	return f.fakeTokenStore.TouchAPIToken(ctx, id, at)
}

func TestAuthenticateAPITokenStoreErrorOtherThanNotFound(t *testing.T) {
	store := &erroringGetStore{newFakeTokenStore()}
	authn := New("bootstrap-token", time.Hour, WithTokenStore(store))

	if _, err := authn.AuthenticateAPIToken(context.Background(), "anything"); err == nil {
		t.Fatal("AuthenticateAPIToken() = nil, want propagated store error")
	}
}

type erroringGetStore struct{ *fakeTokenStore }

func (e *erroringGetStore) GetAPITokenByHash(context.Context, string) (*APIToken, error) {
	return nil, errors.New("store exploded")
}

func TestRevokePluginTokensPropagatesListError(t *testing.T) {
	authn := New("bootstrap-token", time.Hour, WithTokenStore(&erroringListStore{newFakeTokenStore()}))

	if err := authn.RevokePluginTokens(context.Background(), "p"); err == nil {
		t.Fatal("RevokePluginTokens() = nil, want list error")
	}
}

type erroringListStore struct{ *fakeTokenStore }

func (e *erroringListStore) ListAPITokens(context.Context) ([]*APIToken, error) {
	return nil, errors.New("list failed")
}

func TestListAPITokensPropagatesError(t *testing.T) {
	authn := New("bootstrap-token", time.Hour, WithTokenStore(&erroringListStore{newFakeTokenStore()}))

	if _, err := authn.ListAPITokens(context.Background()); err == nil {
		t.Fatal("ListAPITokens() = nil, want error")
	}
}

func TestCreateAPITokenPropagatesStoreError(t *testing.T) {
	authn := New("bootstrap-token", time.Hour, WithTokenStore(&createFailStore{newFakeTokenStore()}))

	_, _, err := authn.CreateAPIToken(context.Background(), CreateTokenRequest{
		Name: "n", UserID: "u", Scopes: []Scope{ScopeReadClusters},
	})
	if err == nil {
		t.Fatal("CreateAPIToken() = nil, want store error")
	}
}

type createFailStore struct{ *fakeTokenStore }

func (c *createFailStore) CreateAPIToken(context.Context, *APIToken) error {
	return errors.New("insert failed")
}

// The generated secret must be unique and prefixed for scanning.
func TestNewTokenSecretShape(t *testing.T) {
	seen := make(map[string]struct{}, 50)
	for range 50 {
		secret, err := newTokenSecret()
		if err != nil {
			t.Fatalf("newTokenSecret() error = %v", err)
		}
		if len(secret) <= len("fleet_pat_") {
			t.Fatalf("secret too short: %q", secret)
		}
		if secret[:len("fleet_pat_")] != "fleet_pat_" {
			t.Fatalf("secret missing prefix: %q", secret)
		}
		if _, dup := seen[secret]; dup {
			t.Fatal("newTokenSecret() produced a duplicate")
		}
		seen[secret] = struct{}{}
	}
}

// A token bound to a plugin is revocable in bulk, and unrelated tokens survive.
func TestRevokePluginTokensKeepsOthers(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)
	ctx := context.Background()

	if _, _, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name: "plugin", UserID: "u", Scopes: []Scope{ScopeReadClusters}, PluginID: "hive",
	}); err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}
	_, keepSecret, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name: "human", UserID: "u", Scopes: []Scope{ScopeReadClusters},
	})
	if err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	if err := authn.RevokePluginTokens(ctx, "hive"); err != nil {
		t.Fatalf("RevokePluginTokens() error = %v", err)
	}

	// The unbound token still authenticates.
	if _, err := authn.AuthenticateAPIToken(ctx, keepSecret); err != nil {
		t.Fatalf("unrelated token rejected: %v", err)
	}
}

// A delete failure other than "not found" must be reported, not swallowed.
func TestRevokePluginTokensReportsDeleteError(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)
	ctx := context.Background()

	if _, _, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name: "plugin", UserID: "u", Scopes: []Scope{ScopeReadClusters}, PluginID: "hive",
	}); err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	store := authn.tokens.(*fakeTokenStore)
	authn.tokens = &deleteFailStore{fakeTokenStore: store}

	if err := authn.RevokePluginTokens(ctx, "hive"); err == nil {
		t.Fatal("RevokePluginTokens() = nil, want delete error")
	}
}

type deleteFailStore struct{ *fakeTokenStore }

func (d *deleteFailStore) DeleteAPIToken(context.Context, string) error {
	return errors.New("delete failed")
}

// Subtle: a store that reports "not found" on delete is not an error here.
func TestRevokePluginTokensToleratesMissing(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)
	ctx := context.Background()

	if _, _, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name: "plugin", UserID: "u", Scopes: []Scope{ScopeReadClusters}, PluginID: "hive",
	}); err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	store := authn.tokens.(*fakeTokenStore)
	authn.tokens = &missingDeleteStore{fakeTokenStore: store}

	if err := authn.RevokePluginTokens(ctx, "hive"); err != nil {
		t.Fatalf("RevokePluginTokens() error = %v, want nil for a already-gone row", err)
	}
}

type missingDeleteStore struct{ *fakeTokenStore }

func (m *missingDeleteStore) DeleteAPIToken(context.Context, string) error {
	return ErrTokenNotFound
}

// Generating an id must never return an empty string.
func TestNewTokenIDShape(t *testing.T) {
	seen := make(map[string]struct{}, 20)
	for range 20 {
		id := newTokenID()
		if id == "" {
			t.Fatal("newTokenID() returned empty")
		}
		if _, dup := seen[id]; dup {
			t.Fatal("newTokenID() produced a duplicate")
		}
		seen[id] = struct{}{}
	}
}

// A TTL of zero means no expiry.
func TestCreateAPITokenWithoutTTLHasNoExpiry(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)

	token, _, err := authn.CreateAPIToken(context.Background(), CreateTokenRequest{
		Name: "forever", UserID: "u", Scopes: []Scope{ScopeReadClusters},
	})
	if err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}
	if token.ExpiresAt != nil {
		t.Fatalf("ExpiresAt = %v, want nil", token.ExpiresAt)
	}
}
