package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeTokenStore is an in-memory TokenStore.
type fakeTokenStore struct {
	byHash map[string]*APIToken
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{byHash: make(map[string]*APIToken)}
}

func (f *fakeTokenStore) CreateAPIToken(_ context.Context, token *APIToken) error {
	cp := *token
	f.byHash[token.TokenHash] = &cp

	return nil
}

func (f *fakeTokenStore) ListAPITokens(context.Context) ([]*APIToken, error) {
	out := make([]*APIToken, 0, len(f.byHash))
	for _, t := range f.byHash {
		cp := *t
		out = append(out, &cp)
	}

	return out, nil
}

func (f *fakeTokenStore) ListAPITokensForUser(_ context.Context, userID string) ([]*APIToken, error) {
	var out []*APIToken
	for _, t := range f.byHash {
		if t.UserID == userID {
			cp := *t
			out = append(out, &cp)
		}
	}

	return out, nil
}

func (f *fakeTokenStore) GetAPITokenByHash(_ context.Context, hash string) (*APIToken, error) {
	t, ok := f.byHash[hash]
	if !ok {
		return nil, ErrTokenNotFound
	}
	cp := *t

	return &cp, nil
}

func (f *fakeTokenStore) DeleteAPIToken(_ context.Context, id string) error {
	for hash, t := range f.byHash {
		if t.ID == id {
			delete(f.byHash, hash)

			return nil
		}
	}

	return ErrTokenNotFound
}

func (f *fakeTokenStore) TouchAPIToken(_ context.Context, id string, at time.Time) error {
	for _, t := range f.byHash {
		if t.ID == id {
			used := at
			t.LastUsedAt = &used

			return nil
		}
	}

	return ErrTokenNotFound
}

func newTokenAuthenticator(t *testing.T) (*Authenticator, *fakeTokenStore) {
	t.Helper()

	store := newFakeTokenStore()
	authn := New("bootstrap-token", time.Hour, WithTokenStore(store))

	return authn, store
}

func TestScopesParseAndContain(t *testing.T) {
	scopes := Scopes{"read:clusters", "write:instances"}

	if !scopes.Contains(ScopeReadClusters) {
		t.Error("Contains(read:clusters) = false")
	}
	if scopes.Contains("write:users") {
		t.Error("Contains(write:users) = true, want false")
	}
	if Scopes(nil).Contains("") {
		// An empty scope list grants nothing; this documents that.
		t.Error("nil Scopes unexpectedly contains the empty scope")
	}
}

func TestScopeValidation(t *testing.T) {
	for _, scope := range AllScopes() {
		if !scope.Valid() {
			t.Errorf("AllScopes() produced invalid scope %q", scope)
		}
	}

	if Scope("nonsense").Valid() {
		t.Error("Scope(nonsense).Valid() = true")
	}
	// write implies read for the same resource.
	if !ScopeWriteClusters.Implies(ScopeReadClusters) {
		t.Error("write:clusters should imply read:clusters")
	}
	if ScopeReadClusters.Implies(ScopeWriteClusters) {
		t.Error("read:clusters must not imply write:clusters")
	}
	if ScopeWriteClusters.Implies("read:agents") {
		t.Error("write:clusters must not imply an unrelated resource")
	}
}

func TestCreateAPITokenReturnsSecretOnce(t *testing.T) {
	authn, store := newTokenAuthenticator(t)

	token, secret, err := authn.CreateAPIToken(context.Background(), CreateTokenRequest{
		Name:   "hive install",
		UserID: "user-1",
		Scopes: []Scope{ScopeReadClusters, ScopeWriteInstances},
	})
	if err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}
	if secret == "" {
		t.Fatal("CreateAPIToken() returned an empty secret")
	}
	if strings.Contains(secret, token.TokenHash) {
		t.Fatal("secret must not embed the stored hash")
	}
	if token.ID == "" {
		t.Fatal("token ID is empty")
	}
	if token.LastUsedAt != nil {
		t.Fatal("new token has LastUsedAt set")
	}

	stored, err := store.GetAPITokenByHash(context.Background(), token.TokenHash)
	if err != nil {
		t.Fatalf("GetAPITokenByHash() error = %v", err)
	}
	if stored.TokenHash == secret {
		t.Fatal("store holds the plaintext secret, want a hash")
	}
}

func TestCreateAPITokenValidation(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  CreateTokenRequest
		want error
	}{
		{name: "missing name", req: CreateTokenRequest{UserID: "u"}, want: ErrInvalidTokenRequest},
		{name: "missing user", req: CreateTokenRequest{Name: "n"}, want: ErrInvalidTokenRequest},
		{name: "no scopes", req: CreateTokenRequest{Name: "n", UserID: "u"}, want: ErrInvalidTokenRequest},
		{
			name: "unknown scope",
			req:  CreateTokenRequest{Name: "n", UserID: "u", Scopes: []Scope{"admin:everything"}},
			want: ErrInvalidTokenRequest,
		},
		{
			name: "negative ttl",
			req: CreateTokenRequest{
				Name: "n", UserID: "u", Scopes: []Scope{ScopeReadClusters}, TTL: -time.Hour,
			},
			want: ErrInvalidTokenRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := authn.CreateAPIToken(ctx, tc.req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("CreateAPIToken() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAuthenticateAPITokenValid(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)
	ctx := context.Background()

	_, secret, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name:   "plugin token",
		UserID: "user-1",
		Scopes: []Scope{ScopeReadClusters, ScopeReadInstances},
	})
	if err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	principal, err := authn.AuthenticateAPIToken(ctx, secret)
	if err != nil {
		t.Fatalf("AuthenticateAPIToken() error = %v", err)
	}
	if principal.UserID != "user-1" {
		t.Errorf("UserID = %q", principal.UserID)
	}
	if !principal.HasScope(ScopeReadClusters) {
		t.Error("principal lacks read:clusters")
	}
	if principal.HasScope(ScopeWriteInstances) {
		t.Error("principal has undeclared write:instances")
	}
}

func TestAuthenticateAPITokenRejectsExpired(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)
	ctx := context.Background()

	_, secret, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name: "expired", UserID: "u", Scopes: []Scope{ScopeReadClusters}, TTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	// Move the clock past expiry.
	clock := time.Now().Add(2 * time.Minute)
	authn.now = func() time.Time { return clock }

	if _, err := authn.AuthenticateAPIToken(ctx, secret); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("AuthenticateAPIToken() error = %v, want ErrTokenExpired", err)
	}
}

func TestAuthenticateAPITokenRejectsUnknown(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)

	if _, err := authn.AuthenticateAPIToken(context.Background(), "garbage"); !errors.Is(err, ErrInvalidAPIToken) {
		t.Fatalf("error = %v, want ErrInvalidAPIToken", err)
	}
}

func TestAuthenticateAPITokenRejectsEmpty(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)

	if _, err := authn.AuthenticateAPIToken(context.Background(), ""); !errors.Is(err, ErrInvalidAPIToken) {
		t.Fatalf("error = %v, want ErrInvalidAPIToken", err)
	}
}

func TestAuthenticateAPITokenRecordsLastUse(t *testing.T) {
	authn, store := newTokenAuthenticator(t)
	ctx := context.Background()

	token, secret, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name: "n", UserID: "u", Scopes: []Scope{ScopeReadClusters},
	})
	if err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	if _, err := authn.AuthenticateAPIToken(ctx, secret); err != nil {
		t.Fatalf("AuthenticateAPIToken() error = %v", err)
	}

	stored, err := store.GetAPITokenByHash(ctx, token.TokenHash)
	if err != nil {
		t.Fatalf("GetAPITokenByHash() error = %v", err)
	}
	if stored.LastUsedAt == nil {
		t.Fatal("LastUsedAt not recorded")
	}
}

func TestRevokeAPIToken(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)
	ctx := context.Background()

	token, secret, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name: "n", UserID: "u", Scopes: []Scope{ScopeReadClusters},
	})
	if err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	if err := authn.RevokeAPIToken(ctx, token.ID); err != nil {
		t.Fatalf("RevokeAPIToken() error = %v", err)
	}
	if _, err := authn.AuthenticateAPIToken(ctx, secret); !errors.Is(err, ErrInvalidAPIToken) {
		t.Fatalf("error after revoke = %v, want ErrInvalidAPIToken", err)
	}
}

func TestRevokeAPITokenUnknown(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)

	if err := authn.RevokeAPIToken(context.Background(), "missing"); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("error = %v, want ErrTokenNotFound", err)
	}
}

func TestListAPITokensNeverExposesHash(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)
	ctx := context.Background()

	if _, _, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name: "n", UserID: "u", Scopes: []Scope{ScopeReadClusters},
	}); err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	tokens, err := authn.ListAPITokens(ctx)
	if err != nil {
		t.Fatalf("ListAPITokens() error = %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("len(tokens) = %d", len(tokens))
	}
	if tokens[0].TokenHash != "" {
		t.Fatal("ListAPITokens() exposed the token hash")
	}
}

func TestPluginScopedTokenBindsIdentity(t *testing.T) {
	authn, _ := newTokenAuthenticator(t)
	ctx := context.Background()

	_, secret, err := authn.CreateAPIToken(ctx, CreateTokenRequest{
		Name:       "hive plugin",
		UserID:     "user-1",
		Scopes:     []Scope{ScopeReadClusters, ScopeWriteInstances},
		PluginID:   "mibeehive",
		InstanceID: "inst-42",
	})
	if err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	principal, err := authn.AuthenticateAPIToken(ctx, secret)
	if err != nil {
		t.Fatalf("AuthenticateAPIToken() error = %v", err)
	}
	if principal.PluginID != "mibeehive" || principal.InstanceID != "inst-42" {
		t.Fatalf("principal = %+v, want plugin binding", principal)
	}

	// Clearing a plugin's tokens must revoke the whole install.
	if err := authn.RevokePluginTokens(ctx, "mibeehive"); err != nil {
		t.Fatalf("RevokePluginTokens() error = %v", err)
	}
	if _, err := authn.AuthenticateAPIToken(ctx, secret); !errors.Is(err, ErrInvalidAPIToken) {
		t.Fatalf("error after plugin revoke = %v", err)
	}
}

func TestTokenHashingIsStable(t *testing.T) {
	secret := "fleet" + "_pat_" + "example"
	first := HashAPIToken(secret)
	second := HashAPIToken(secret)

	if first != second {
		t.Fatal("HashAPIToken is not deterministic")
	}
	if first == secret {
		t.Fatal("HashAPIToken returned the plaintext")
	}
	if HashAPIToken("other") == first {
		t.Fatal("distinct secrets hashed identically")
	}
}
