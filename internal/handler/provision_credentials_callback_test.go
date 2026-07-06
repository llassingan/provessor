package handler

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	appdb "github.com/llassingan/provessor/internal/db"
	"github.com/llassingan/provessor/internal/model"
	"github.com/llassingan/provessor/internal/repository"
)

func setupCredentialsCallbackHandlerTest(t *testing.T) (*sql.DB, *repository.VPSRepository, *VPSHandler) {
	t.Helper()

	database, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := appdb.Run(database); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	_, err = database.Exec(`INSERT INTO templates (id, name, description, type, cloud_init_yaml) VALUES (1, 'Ubuntu', 'Ubuntu', 'predefined', '#cloud-config')`)
	if err != nil {
		t.Fatalf("insert template: %v", err)
	}

	repo := repository.NewVPSRepository(database)
	return database, repo, NewVPSHandler(repo, nil, nil, nil, nil, nil)
}

func createCallbackVPS(t *testing.T, repo *repository.VPSRepository, token string, expiresAt time.Time) *model.VPS {
	t.Helper()

	vps, err := repo.Create(context.Background(), &model.VPS{
		DisplayName:      "callback-test",
		TemplateID:       1,
		Shape:            "VM.Standard.E4.Flex",
		OCPU:             1,
		MemoryGB:         8,
		BootVolumeSizeGB: 50,
		Status:           "provisioning",
	})
	if err != nil {
		t.Fatalf("create vps: %v", err)
	}
	if err := repo.SetCredentialsCallbackToken(context.Background(), vps.ID, repository.HashCredentialsCallbackToken(token), expiresAt); err != nil {
		t.Fatalf("set token: %v", err)
	}
	return vps
}

func performCredentialsCallback(handler *VPSHandler, id string, token string, body string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Post("/api/vps/{id}/credentials", handler.HandleCredentialsCallback)

	req := httptest.NewRequest(http.MethodPost, "/api/vps/"+id+"/credentials", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	return res
}

func TestHandleCredentialsCallbackValidTokenSuccess(t *testing.T) {
	_, repo, handler := setupCredentialsCallbackHandlerTest(t)
	vps := createCallbackVPS(t, repo, "valid-token", time.Now().Add(time.Hour))

	res := performCredentialsCallback(handler, "1", "valid-token", `{"username":"root","password":"secret"}`)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", res.Code, res.Body.String())
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps: %v", err)
	}
	if !updated.InitialCredentials.Valid {
		t.Fatal("expected credentials to be stored")
	}
	if updated.CredentialsCallbackTokenHash.Valid {
		t.Fatal("expected callback token hash to be cleared")
	}
}

func TestHandleCredentialsCallbackMissingToken(t *testing.T) {
	_, repo, handler := setupCredentialsCallbackHandlerTest(t)
	vps := createCallbackVPS(t, repo, "valid-token", time.Now().Add(time.Hour))

	res := performCredentialsCallback(handler, "1", "", `{"password":"secret"}`)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.Code)
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps: %v", err)
	}
	if updated.InitialCredentials.Valid {
		t.Fatal("missing token stored credentials")
	}
}

func TestHandleCredentialsCallbackWrongExpiredAndReplayTokens(t *testing.T) {
	_, repo, handler := setupCredentialsCallbackHandlerTest(t)
	vps := createCallbackVPS(t, repo, "valid-token", time.Now().Add(time.Hour))

	res := performCredentialsCallback(handler, "1", "wrong-token", `{"password":"wrong"}`)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected wrong token 401, got %d", res.Code)
	}

	if err := repo.SetCredentialsCallbackToken(context.Background(), vps.ID, repository.HashCredentialsCallbackToken("valid-token"), time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("set expired token: %v", err)
	}
	res = performCredentialsCallback(handler, "1", "valid-token", `{"password":"expired"}`)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected expired token 401, got %d", res.Code)
	}

	if err := repo.SetCredentialsCallbackToken(context.Background(), vps.ID, repository.HashCredentialsCallbackToken("valid-token"), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("reset token: %v", err)
	}
	res = performCredentialsCallback(handler, "1", "valid-token", `{"password":"first"}`)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected first callback 204, got %d", res.Code)
	}
	res = performCredentialsCallback(handler, "1", "valid-token", `{"password":"second"}`)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected replay 401, got %d", res.Code)
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps: %v", err)
	}
	if updated.InitialCredentials.String != `{"password":"first"}` {
		t.Fatalf("replay overwrote credentials: %s", updated.InitialCredentials.String)
	}
}

func TestHandleCredentialsCallbackMalformedJSONDoesNotConsumeToken(t *testing.T) {
	_, repo, handler := setupCredentialsCallbackHandlerTest(t)
	vps := createCallbackVPS(t, repo, "valid-token", time.Now().Add(time.Hour))

	res := performCredentialsCallback(handler, "1", "valid-token", `{"password":`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected malformed JSON 400, got %d", res.Code)
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps: %v", err)
	}
	if updated.InitialCredentials.Valid {
		t.Fatal("malformed JSON stored credentials")
	}
	if !updated.CredentialsCallbackTokenHash.Valid {
		t.Fatal("malformed JSON consumed token")
	}

	res = performCredentialsCallback(handler, "1", "valid-token", `{"password":"secret"}`)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected token to remain usable after malformed JSON, got %d", res.Code)
	}
}

func TestHandleCredentialsCallbackRejectsInvalidIDAndNonObjectBody(t *testing.T) {
	_, repo, handler := setupCredentialsCallbackHandlerTest(t)
	_ = createCallbackVPS(t, repo, "valid-token", time.Now().Add(time.Hour))

	res := performCredentialsCallback(handler, "not-an-id", "valid-token", `{"password":"secret"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid id 400, got %d", res.Code)
	}

	res = performCredentialsCallback(handler, "1", "valid-token", `["not-object"]`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected non-object body 400, got %d", res.Code)
	}
}

func TestHandleCredentialsCallbackDoesNotChangeStatus(t *testing.T) {
	_, repo, handler := setupCredentialsCallbackHandlerTest(t)
	vps := createCallbackVPS(t, repo, "valid-token", time.Now().Add(time.Hour))

	res := performCredentialsCallback(handler, "1", "valid-token", `{"password":"secret"}`)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps: %v", err)
	}
	if updated.Status != "provisioning" {
		t.Fatalf("callback changed status: %s", updated.Status)
	}
}
