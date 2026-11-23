package store

import (
	"context"
	"testing"
	"time"
)

func newTokenTestStore(t *testing.T) *SQLStore {
	t.Helper()

	st, err := Open(context.Background(), "sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return st
}

func TestAPITokenRoundTrip(t *testing.T) {
	st := newTokenTestStore(t)
	ctx := context.Background()

	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	token := &APIToken{
		ID:         "tok-1",
		Name:       "hive install",
		UserID:     "user-1",
		Username:   "admin",
		Scopes:     []string{"read:clusters", "read:instances"},
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
		ExpiresAt:  &expires,
		PluginID:   "mibeehive",
		InstanceID: "inst-9",
		TokenHash:  "hash-abc",
	}
	if err := st.CreateAPIToken(ctx, token); err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	got, err := st.GetAPITokenByHash(ctx, "hash-abc")
	if err != nil {
		t.Fatalf("GetAPITokenByHash() error = %v", err)
	}
	if got.ID != token.ID || got.Name != token.Name || got.UserID != token.UserID {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if len(got.Scopes) != 2 || got.Scopes[0] != "read:clusters" {
		t.Fatalf("scopes = %v", got.Scopes)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("expires_at = %v, want %v", got.ExpiresAt, expires)
	}
	if got.PluginID != "mibeehive" || got.InstanceID != "inst-9" {
		t.Fatalf("plugin binding = %q/%q", got.PluginID, got.InstanceID)
	}
	if got.LastUsedAt != nil {
		t.Fatalf("last_used_at = %v, want nil", got.LastUsedAt)
	}
}

func TestAPITokenHashUnique(t *testing.T) {
	st := newTokenTestStore(t)
	ctx := context.Background()

	base := APIToken{
		Name: "n", UserID: "u", Scopes: []string{"read:clusters"},
		CreatedAt: time.Now().UTC(), TokenHash: "same-hash",
	}
	first := base
	first.ID = "a"
	if err := st.CreateAPIToken(ctx, &first); err != nil {
		t.Fatalf("CreateAPIToken(first) error = %v", err)
	}

	second := base
	second.ID = "b"
	if err := st.CreateAPIToken(ctx, &second); err == nil {
		t.Fatal("CreateAPIToken(duplicate hash) = nil, want unique constraint error")
	}
}

func TestAPITokenNotFound(t *testing.T) {
	st := newTokenTestStore(t)

	if _, err := st.GetAPITokenByHash(context.Background(), "missing"); err == nil {
		t.Fatal("GetAPITokenByHash() = nil, want error")
	}
}

func TestListAPITokensOrderingAndFilter(t *testing.T) {
	st := newTokenTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i, token := range []APIToken{
		{ID: "a", Name: "a", UserID: "alice", Scopes: []string{"read:clusters"}, CreatedAt: now.Add(-2 * time.Hour), TokenHash: "h-a"},
		{ID: "b", Name: "b", UserID: "bob", Scopes: []string{"read:clusters"}, CreatedAt: now.Add(-time.Hour), TokenHash: "h-b"},
		{ID: "c", Name: "c", UserID: "alice", Scopes: []string{"read:clusters"}, CreatedAt: now, TokenHash: "h-c"},
	} {
		tok := token
		if err := st.CreateAPIToken(ctx, &tok); err != nil {
			t.Fatalf("CreateAPIToken(%d) error = %v", i, err)
		}
	}

	all, err := st.ListAPITokens(ctx)
	if err != nil {
		t.Fatalf("ListAPITokens() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len(all) = %d", len(all))
	}
	// Newest first.
	if all[0].ID != "c" || all[2].ID != "a" {
		t.Fatalf("order = %v,%v,%v", all[0].ID, all[1].ID, all[2].ID)
	}

	alice, err := st.ListAPITokensForUser(ctx, "alice")
	if err != nil {
		t.Fatalf("ListAPITokensForUser() error = %v", err)
	}
	if len(alice) != 2 {
		t.Fatalf("alice tokens = %d", len(alice))
	}
}

func TestDeleteAPIToken(t *testing.T) {
	st := newTokenTestStore(t)
	ctx := context.Background()

	token := &APIToken{
		ID: "x", Name: "n", UserID: "u", Scopes: []string{"read:clusters"},
		CreatedAt: time.Now().UTC(), TokenHash: "h-x",
	}
	if err := st.CreateAPIToken(ctx, token); err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}
	if err := st.DeleteAPIToken(ctx, "x"); err != nil {
		t.Fatalf("DeleteAPIToken() error = %v", err)
	}
	if _, err := st.GetAPITokenByHash(ctx, "h-x"); err == nil {
		t.Fatal("token still readable after delete")
	}
	if err := st.DeleteAPIToken(ctx, "x"); err == nil {
		t.Fatal("DeleteAPIToken(missing) = nil, want error")
	}
}

func TestTouchAPIToken(t *testing.T) {
	st := newTokenTestStore(t)
	ctx := context.Background()

	token := &APIToken{
		ID: "x", Name: "n", UserID: "u", Scopes: []string{"read:clusters"},
		CreatedAt: time.Now().UTC(), TokenHash: "h-x",
	}
	if err := st.CreateAPIToken(ctx, token); err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	usedAt := time.Now().UTC().Truncate(time.Second)
	if err := st.TouchAPIToken(ctx, "x", usedAt); err != nil {
		t.Fatalf("TouchAPIToken() error = %v", err)
	}

	got, err := st.GetAPITokenByHash(ctx, "h-x")
	if err != nil {
		t.Fatalf("GetAPITokenByHash() error = %v", err)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(usedAt) {
		t.Fatalf("last_used_at = %v, want %v", got.LastUsedAt, usedAt)
	}

	if err := st.TouchAPIToken(ctx, "missing", usedAt); err == nil {
		t.Fatal("TouchAPIToken(missing) = nil, want error")
	}
}

// Migration must be idempotent: reopening an existing database keeps tokens.
func TestAPITokenSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	dsn := dir + "/fleet.db"
	ctx := context.Background()

	st, err := Open(ctx, "sqlite", dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	token := &APIToken{
		ID: "persist", Name: "n", UserID: "u", Scopes: []string{"read:clusters"},
		CreatedAt: time.Now().UTC(), TokenHash: "h-persist",
	}
	if err := st.CreateAPIToken(ctx, token); err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(ctx, "sqlite", dsn)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	got, err := reopened.GetAPITokenByHash(ctx, "h-persist")
	if err != nil {
		t.Fatalf("GetAPITokenByHash() after reopen error = %v", err)
	}
	if got.ID != "persist" {
		t.Fatalf("id = %q", got.ID)
	}
}

// A malformed scope payload must surface as an error, not a zero-value token.
func TestScanAPITokenRejectsBadScopes(t *testing.T) {
	st := newTokenTestStore(t)
	ctx := context.Background()

	if _, err := st.db.ExecContext(ctx, st.rebind(`INSERT INTO api_tokens
		(id, name, user_id, username, scopes, created_at, expires_at, last_used_at, plugin_id, instance_id, token_hash)
		VALUES ('bad','n','u','','not-json',1,0,0,'','','h-bad')`)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := st.GetAPITokenByHash(ctx, "h-bad"); err == nil {
		t.Fatal("GetAPITokenByHash() = nil, want decode error")
	}
}

// Empty scope arrays round-trip as an empty slice rather than null.
func TestAPITokenEmptyScopes(t *testing.T) {
	st := newTokenTestStore(t)
	ctx := context.Background()

	token := &APIToken{
		ID: "empty", Name: "n", UserID: "u", Scopes: []string{},
		CreatedAt: time.Now().UTC(), TokenHash: "h-empty",
	}
	if err := st.CreateAPIToken(ctx, token); err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	got, err := st.GetAPITokenByHash(ctx, "h-empty")
	if err != nil {
		t.Fatalf("GetAPITokenByHash() error = %v", err)
	}
	if got.Scopes == nil || len(got.Scopes) != 0 {
		t.Fatalf("scopes = %v, want an empty slice", got.Scopes)
	}
}

// Expiry and last-use timestamps survive the millisecond encoding.
func TestAPITokenTimestamps(t *testing.T) {
	st := newTokenTestStore(t)
	ctx := context.Background()

	expires := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Millisecond)
	used := time.Now().UTC().Truncate(time.Millisecond)
	token := &APIToken{
		ID: "ts", Name: "n", UserID: "u", Scopes: []string{"read:clusters"},
		CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
		ExpiresAt: &expires, LastUsedAt: &used, TokenHash: "h-ts",
	}
	if err := st.CreateAPIToken(ctx, token); err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}

	got, err := st.GetAPITokenByHash(ctx, "h-ts")
	if err != nil {
		t.Fatalf("GetAPITokenByHash() error = %v", err)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("expires = %v, want %v", got.ExpiresAt, expires)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(used) {
		t.Fatalf("last used = %v, want %v", got.LastUsedAt, used)
	}
}
