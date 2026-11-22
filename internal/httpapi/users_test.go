package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/store"
	"github.com/mickeyzzc/incus-fleet/internal/testutil/incusfake"
)

// userFixture builds an API whose authenticator is backed by the fixture store,
// so account and role paths run end to end.
func userFixture(t *testing.T) *apiFixture {
	t.Helper()

	source := &incusfake.Server{}
	target := &incusfake.Server{}

	// The authenticator needs the store, which the fixture creates, so build it
	// in two passes: a probe fixture provides the store, then the real fixture
	// reuses that exact store so accounts written by loginAs are visible.
	probe := newAPIFixture(t, source, target)
	adapter := auth.NewStoreAdapter(probe.store)
	authenticator := auth.New("secret", time.Hour, auth.WithUserStore(adapter))

	grown := newAPIFixtureWithAuth(t, source, target, authenticator)
	// Point the new fixture at the probe's store and rebuild the handler so the
	// API and the authenticator share one database.
	grown.store = probe.store
	grown.handler = newHandlerWith(grown, authenticator)

	return grown
}

// newHandlerWith rebuilds the handler bound to the fixture's current store and
// the supplied authenticator.
func newHandlerWith(f *apiFixture, authenticator *auth.Authenticator) http.Handler {
	return New(Deps{
		Clusters:   f.manager,
		Instances:  instances.New(f.manager),
		Migrations: f.migrations,
		Store:      f.store,
		Auth:       authenticator,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:    "test",
	}).Handler()
}

// loginAs creates an account and returns its session cookie.
func loginAs(t *testing.T, f *apiFixture, username string, role auth.Role, password string) *http.Cookie {
	t.Helper()

	hash, err := f.auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := time.Now().UTC()
	user := &store.User{
		ID:           "user-" + username,
		Username:     username,
		PasswordHash: hash,
		Role:         string(role),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := f.store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	rec := f.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": username,
		"password": password,
	}, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s status = %d body = %s", username, rec.Code, rec.Body.String())
	}

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login %s returned no cookie", username)
	}

	return cookies[0]
}

func TestLoginWithUsernameAndPassword(t *testing.T) {
	f := userFixture(t)
	cookie := loginAs(t, f, "alice", auth.RoleOperator, "correct-horse-9")

	rec := f.request(t, http.MethodGet, "/api/v1/auth/session", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("session status = %d body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
		Role          string `json:"role"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Authenticated || body.Username != "alice" || body.Role != "operator" {
		t.Fatalf("session = %#v", body)
	}
}

func TestViewerCannotWrite(t *testing.T) {
	f := userFixture(t)
	viewer := loginAs(t, f, "viewer1", auth.RoleViewer, "correct-horse-9")

	// Reads are allowed.
	if rec := f.request(t, http.MethodGet, "/api/v1/clusters", nil, viewer, ""); rec.Code != http.StatusOK {
		t.Fatalf("viewer read status = %d", rec.Code)
	}

	// Writes are rejected before reaching the handler.
	rec := f.request(t, http.MethodPost, "/api/v1/clusters/c1/reconnect", nil, viewer, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer write status = %d body = %s, want 403", rec.Code, rec.Body.String())
	}
}

func TestOperatorCannotManageUsers(t *testing.T) {
	f := userFixture(t)
	operator := loginAs(t, f, "op1", auth.RoleOperator, "correct-horse-9")

	rec := f.request(t, http.MethodGet, "/api/v1/users", nil, operator, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("operator list users status = %d, want 403", rec.Code)
	}
}

func TestAdminCanManageUsers(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	// Create
	rec := f.request(t, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "newbie",
		"password": "correct-horse-9",
		"role":     "viewer",
	}, admin, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Username != "newbie" || created.Role != "viewer" {
		t.Fatalf("created = %#v", created)
	}

	// List
	rec = f.request(t, http.MethodGet, "/api/v1/users", nil, admin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list users status = %d", rec.Code)
	}

	// Update the role
	rec = f.request(t, http.MethodPatch, "/api/v1/users/"+created.ID, map[string]any{
		"role": "operator",
	}, admin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("update user status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Delete
	if rec := f.request(t, http.MethodDelete, "/api/v1/users/"+created.ID, nil, admin, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete user status = %d", rec.Code)
	}
}

func TestCannotDeleteLastAdmin(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	users, err := f.store.ListUsers(context.Background())
	if err != nil || len(users) != 1 {
		t.Fatalf("ListUsers = %d, %v", len(users), err)
	}

	rec := f.request(t, http.MethodDelete, "/api/v1/users/"+users[0].ID, nil, admin, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete last admin status = %d body = %s, want 409", rec.Code, rec.Body.String())
	}
}

func TestBootstrapTokenCannotCreateSecondUser(t *testing.T) {
	f := userFixture(t)

	// The bootstrap token works while no account exists.
	bootstrap := f.login(t)
	rec := f.request(t, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "first",
		"password": "correct-horse-9",
		"role":     "admin",
	}, bootstrap, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap create status = %d body = %s", rec.Code, rec.Body.String())
	}

	// Afterwards the token no longer authenticates at all.
	rec = f.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{"token": "secret"}, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bootstrap login after first account status = %d, want 401", rec.Code)
	}
}

func TestInvalidCredentialsAreRejected(t *testing.T) {
	f := userFixture(t)
	loginAs(t, f, "alice", auth.RoleViewer, "correct-horse-9")

	rec := f.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "alice",
		"password": "wrong-password",
	}, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401", rec.Code)
	}
}

func TestChangePassword(t *testing.T) {
	f := userFixture(t)
	cookie := loginAs(t, f, "alice", auth.RoleOperator, "correct-horse-9")

	rec := f.request(t, http.MethodPost, "/api/v1/auth/password", map[string]string{
		"current_password": "correct-horse-9",
		"new_password":     "brand-new-pass-1",
	}, cookie, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change password status = %d body = %s", rec.Code, rec.Body.String())
	}

	// The old password no longer works.
	rec = f.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "alice",
		"password": "correct-horse-9",
	}, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("old password status = %d, want 401", rec.Code)
	}

	// The new one does.
	rec = f.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "alice",
		"password": "brand-new-pass-1",
	}, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("new password status = %d", rec.Code)
	}
}

func TestUserValidationErrors(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"missing username", map[string]any{"password": "correct-horse-9", "role": "viewer"}, http.StatusBadRequest},
		{"bad role", map[string]any{"username": "x", "password": "correct-horse-9", "role": "superuser"}, http.StatusBadRequest},
		{"weak password", map[string]any{"username": "y", "password": "short", "role": "viewer"}, http.StatusBadRequest},
	}

	for _, tc := range cases {
		rec := f.request(t, http.MethodPost, "/api/v1/users", tc.body, admin, "")
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d (body %s)", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestUserDuplicateUsername(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	body := map[string]any{"username": "dup", "password": "correct-horse-9", "role": "viewer"}
	if rec := f.request(t, http.MethodPost, "/api/v1/users", body, admin, ""); rec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d", rec.Code)
	}
	if rec := f.request(t, http.MethodPost, "/api/v1/users", body, admin, ""); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, want 409", rec.Code)
	}
}

func TestCannotDemoteLastAdmin(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	users, err := f.store.ListUsers(context.Background())
	if err != nil || len(users) != 1 {
		t.Fatalf("ListUsers = %d, %v", len(users), err)
	}

	rec := f.request(t, http.MethodPatch, "/api/v1/users/"+users[0].ID, map[string]any{
		"role": "viewer",
	}, admin, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("demote last admin status = %d body = %s, want 409", rec.Code, rec.Body.String())
	}

	disabled := true
	rec = f.request(t, http.MethodPatch, "/api/v1/users/"+users[0].ID, map[string]any{
		"disabled": disabled,
	}, admin, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("disable last admin status = %d body = %s, want 409", rec.Code, rec.Body.String())
	}
}

func TestUpdateUserNotFound(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	rec := f.request(t, http.MethodPatch, "/api/v1/users/missing", map[string]any{"role": "viewer"}, admin, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update missing status = %d, want 404", rec.Code)
	}
	if rec := f.request(t, http.MethodDelete, "/api/v1/users/missing", nil, admin, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing status = %d, want 404", rec.Code)
	}
}

func TestPasswordChangeRejectsWrongCurrent(t *testing.T) {
	f := userFixture(t)
	cookie := loginAs(t, f, "alice", auth.RoleOperator, "correct-horse-9")

	rec := f.request(t, http.MethodPost, "/api/v1/auth/password", map[string]string{
		"current_password": "not-the-password",
		"new_password":     "another-good-pass-1",
	}, cookie, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password status = %d, want 401", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/auth/password", map[string]string{
		"current_password": "correct-horse-9",
		"new_password":     "short",
	}, cookie, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("weak new password status = %d, want 400", rec.Code)
	}
}

func TestBootstrapSessionCannotChangePassword(t *testing.T) {
	f := userFixture(t)
	bootstrap := f.login(t)

	rec := f.request(t, http.MethodPost, "/api/v1/auth/password", map[string]string{
		"current_password": "x",
		"new_password":     "correct-horse-9",
	}, bootstrap, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bootstrap change password status = %d, want 400", rec.Code)
	}
}

func TestListSessions(t *testing.T) {
	f := userFixture(t)
	cookie := loginAs(t, f, "alice", auth.RoleOperator, "correct-horse-9")

	rec := f.request(t, http.MethodGet, "/api/v1/auth/sessions", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list sessions status = %d", rec.Code)
	}

	var body struct {
		Sessions []struct {
			Username string `json:"username"`
			Current  bool   `json:"current"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sessions) != 1 || body.Sessions[0].Username != "alice" || !body.Sessions[0].Current {
		t.Fatalf("sessions = %#v", body.Sessions)
	}
}

func TestUserCreationBootstrapCheckFails(t *testing.T) {
	f := userFixture(t)
	bootstrap := f.login(t)

	// Break the store so the "does any account exist" probe fails.
	f.closeStore(t)

	rec := f.request(t, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "x",
		"password": "correct-horse-9",
		"role":     "viewer",
	}, bootstrap, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("bootstrap probe failure status = %d, want 500", rec.Code)
	}
}

func TestUserStoreFailures(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")
	f.closeStore(t)

	if rec := f.request(t, http.MethodGet, "/api/v1/users", nil, admin, ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("list users on broken store = %d, want 500", rec.Code)
	}
	if rec := f.request(t, http.MethodPatch, "/api/v1/users/x", map[string]any{"role": "viewer"}, admin, ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("update user on broken store = %d, want 500", rec.Code)
	}
	if rec := f.request(t, http.MethodDelete, "/api/v1/users/x", nil, admin, ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("delete user on broken store = %d, want 500", rec.Code)
	}
}

func TestUpdateUserWeakPassword(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	users, err := f.store.ListUsers(context.Background())
	if err != nil || len(users) == 0 {
		t.Fatalf("ListUsers = %d, %v", len(users), err)
	}

	rec := f.request(t, http.MethodPatch, "/api/v1/users/"+users[0].ID, map[string]any{
		"password": "short",
	}, admin, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("weak password update = %d, want 400", rec.Code)
	}
}

func TestUpdateUserBadRole(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	users, _ := f.store.ListUsers(context.Background())
	rec := f.request(t, http.MethodPatch, "/api/v1/users/"+users[0].ID, map[string]any{
		"role": "superuser",
	}, admin, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad role update = %d, want 400", rec.Code)
	}
}

func TestSessionsRequiresAuth(t *testing.T) {
	f := userFixture(t)
	if rec := f.request(t, http.MethodGet, "/api/v1/auth/sessions", nil, nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated sessions = %d, want 401", rec.Code)
	}
}

func TestCreateUserDefaultsToViewer(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	rec := f.request(t, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "noro",
		"password": "correct-horse-9",
	}, admin, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Role != "viewer" {
		t.Fatalf("default role = %q, want viewer", body.Role)
	}
}

func TestAdminPromotesAndDemotesAnotherAdmin(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	// A second admin means the first is no longer the last.
	rec := f.request(t, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "second",
		"password": "correct-horse-9",
		"role":     "admin",
	}, admin, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create second admin = %d", rec.Code)
	}

	users, err := f.store.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var second string
	for _, user := range users {
		if user.Username == "second" {
			second = user.ID
		}
	}
	if second == "" {
		t.Fatal("second admin not found")
	}

	// Demotion is now allowed.
	rec = f.request(t, http.MethodPatch, "/api/v1/users/"+second, map[string]any{"role": "viewer"}, admin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("demote with another admin = %d body = %s", rec.Code, rec.Body.String())
	}

	// Re-enable and disable works too.
	disabled := true
	rec = f.request(t, http.MethodPatch, "/api/v1/users/"+second, map[string]any{"disabled": disabled}, admin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("disable non-last admin = %d", rec.Code)
	}
}

func TestLoginLockedAccount(t *testing.T) {
	f := userFixture(t)
	loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	// Create a second account and lock it out through repeated failures.
	loginAs(t, f, "target", auth.RoleViewer, "correct-horse-9")
	for i := 0; i < 5; i++ {
		rec := f.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"username": "target",
			"password": "wrong-password",
		}, nil, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d", i+1, rec.Code)
		}
	}

	rec := f.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "target",
		"password": "correct-horse-9",
	}, nil, "")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("locked login status = %d body = %s, want 429", rec.Code, rec.Body.String())
	}
}

func TestLoginDisabledAccount(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	rec := f.request(t, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "suspended",
		"password": "correct-horse-9",
		"role":     "viewer",
		"disabled": true,
	}, admin, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create disabled user = %d", rec.Code)
	}

	rec = f.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "suspended",
		"password": "correct-horse-9",
	}, nil, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled login status = %d body = %s, want 403", rec.Code, rec.Body.String())
	}
}

func TestLogoutThenSessionIsUnauthorized(t *testing.T) {
	f := userFixture(t)
	cookie := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	if rec := f.request(t, http.MethodPost, "/api/v1/auth/logout", nil, cookie, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", rec.Code)
	}
	if rec := f.request(t, http.MethodGet, "/api/v1/auth/session", nil, cookie, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("session after logout = %d, want 401", rec.Code)
	}
}

func TestLoginInternalError(t *testing.T) {
	f := userFixture(t)
	loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")
	// Closing the store makes the user lookup fail with an internal error.
	f.closeStore(t)

	rec := f.request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"username": "root",
		"password": "correct-horse-9",
	}, nil, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s, want 500", rec.Code, rec.Body.String())
	}
}

func TestListSessionsRejectsMissingContext(t *testing.T) {
	// A handler invoked without requireAuth has no session in context.
	f := userFixture(t)
	rec := f.request(t, http.MethodGet, "/api/v1/auth/sessions", nil, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestUploadRejectsNonMultipart(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	rec := f.request(t, http.MethodPost,
		"/api/v1/clusters/c1/instances/vm/files?path=/tmp/x.txt", "not-a-form", admin, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("non-multipart upload status = %d, want 400", rec.Code)
	}
}

func TestRoleGateDoesNotLeakOnReadRoutes(t *testing.T) {
	f := userFixture(t)
	viewer := loginAs(t, f, "reader", auth.RoleViewer, "correct-horse-9")

	// Several read endpoints must stay reachable for a read-only account.
	for _, path := range []string{"/api/v1/clusters", "/api/v1/instances", "/api/v1/migrations"} {
		if rec := f.request(t, http.MethodGet, path, nil, viewer, ""); rec.Code != http.StatusOK {
			t.Errorf("viewer GET %s = %d, want 200", path, rec.Code)
		}
	}
}

func TestSessionReportsExpiry(t *testing.T) {
	f := userFixture(t)
	cookie := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	rec := f.request(t, http.MethodGet, "/api/v1/auth/session", nil, cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var body struct {
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ExpiresAt == "" {
		t.Fatal("session response omitted expires_at")
	}
}

func TestDeleteAdminWithAnotherAdminPresent(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "root", auth.RoleAdmin, "correct-horse-9")

	// Promote a second account so the original is no longer the last admin.
	loginAs(t, f, "second", auth.RoleAdmin, "correct-horse-9")

	users, err := f.store.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var secondID string
	for _, user := range users {
		if user.Username == "second" {
			secondID = user.ID
		}
	}

	if rec := f.request(t, http.MethodDelete, "/api/v1/users/"+secondID, nil, admin, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete non-last admin = %d body = %s", rec.Code, rec.Body.String())
	}
}

// TestDeleteUserBranches covers the not-found path and the
// last-administrator guard, which protects against locking everyone out.
func TestDeleteUserBranches(t *testing.T) {
	f := userFixture(t)
	admin := loginAs(t, f, "admin1", auth.RoleAdmin, "correct-horse-9")

	if rec := f.request(t, http.MethodDelete, "/api/v1/users/missing", nil, admin, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing status = %d body = %s", rec.Code, rec.Body.String())
	}

	// admin1 is the only enabled administrator, so deleting it conflicts.
	rec := f.request(t, http.MethodGet, "/api/v1/users", nil, admin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list users status = %d body = %s", rec.Code, rec.Body.String())
	}
	var users struct {
		Users []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	var adminID string
	for _, u := range users.Users {
		if u.Role == string(auth.RoleAdmin) {
			adminID = u.ID
		}
	}
	if adminID == "" {
		t.Fatal("no administrator in fixture")
	}
	rec = f.request(t, http.MethodDelete, "/api/v1/users/"+adminID, nil, admin, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete last admin status = %d body = %s", rec.Code, rec.Body.String())
	}
}
