package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	appdb "github.com/llassingan/provessor/internal/db"
	"github.com/llassingan/provessor/internal/logger"
	"github.com/llassingan/provessor/internal/model"
	"github.com/llassingan/provessor/internal/repository"
	"github.com/llassingan/provessor/internal/service"
)

type resetPasswordProvisioner struct {
	repo *repository.VPSRepository
}

func (p resetPasswordProvisioner) VPSRegionForDelete(context.Context, int64) (string, error) {
	return "", nil
}
func (p resetPasswordProvisioner) StartInstance(context.Context, int64) error   { return nil }
func (p resetPasswordProvisioner) StopInstance(context.Context, int64) error    { return nil }
func (p resetPasswordProvisioner) RestartInstance(context.Context, int64) error { return nil }
func (p resetPasswordProvisioner) ResetInstance(context.Context, int64) error   { return nil }
func (p resetPasswordProvisioner) GetFirewallRules(context.Context, int64) ([]service.FirewallRule, error) {
	return nil, nil
}
func (p resetPasswordProvisioner) UpdateFirewallRules(context.Context, int64, []service.FirewallRule) error {
	return nil
}
func (p resetPasswordProvisioner) RefreshInstanceIPs(context.Context, int64) error { return nil }
func (p resetPasswordProvisioner) ResetPassword(ctx context.Context, vpsID int64, newPassword string) error {
	return p.repo.UpdateSSHPassword(ctx, vpsID, newPassword)
}

func performResetPassword(handler *VPSHandler, id string, body string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Post("/api/vps/{id}/reset-password", handler.HandleResetPasswordVPS)

	req := httptest.NewRequest(http.MethodPost, "/api/vps/"+id+"/reset-password", strings.NewReader(body))
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	return res
}

func TestHandleResetPasswordRejectsPolicyFailuresBeforeService(t *testing.T) {
	handler := &VPSHandler{log: logger.Nop()}
	cases := []struct {
		name string
		body string
	}{
		{name: "too short", body: `{"password":"short"}`},
		{name: "too long", body: `{"password":"` + strings.Repeat("a", 129) + `"}`},
		{name: "whitespace only", body: `{"password":"            "}`},
		{name: "newline", body: "{\"password\":\"valid-password\\n\"}"},
		{name: "carriage return", body: "{\"password\":\"valid-password\\r\"}"},
		{name: "nul", body: "{\"password\":\"valid-pass\\u0000word\"}"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := performResetPassword(handler, "1", tc.body)
			if res.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", res.Code, res.Body.String())
			}
		})
	}
}

func TestHandleResetPasswordValidResetReturnsUpdatedVPSWithPassword(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := appdb.Run(database); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO templates (id, name, description, type, cloud_init_yaml) VALUES (1, 'Ubuntu', 'Ubuntu', 'predefined', '#cloud-config')`); err != nil {
		t.Fatalf("insert template: %v", err)
	}

	repo := repository.NewVPSRepository(database)
	vps, err := repo.Create(context.Background(), &model.VPS{
		DisplayName:      "reset-handler-test",
		TemplateID:       1,
		Shape:            "VM.Standard.E4.Flex",
		OCPU:             1,
		MemoryGB:         8,
		BootVolumeSizeGB: 50,
		Status:           "running",
	})
	if err != nil {
		t.Fatalf("create vps: %v", err)
	}
	vps.SSHUsername = model.NullString{NullString: sql.NullString{String: "appuser", Valid: true}}
	vps.SSHPassword = model.NullString{NullString: sql.NullString{String: "old-password", Valid: true}}
	if err := repo.Update(context.Background(), vps); err != nil {
		t.Fatalf("seed ssh password: %v", err)
	}

	handler := NewVPSHandler(repo, nil, nil, nil, resetPasswordProvisioner{repo: repo}, nil, logger.Nop(), nil)
	res := performResetPassword(handler, "1", `{"password":"valid-password12"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response model.VPS
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.SSHPassword.Valid || response.SSHPassword.String != "valid-password12" {
		t.Fatalf("expected updated ssh_password in response, got %#v", response.SSHPassword)
	}
}

// ── HandleResetVPS tests ──────────────────────────────────────────────

type resetVPSProvisioner struct {
	repo *repository.VPSRepository
}

func (p resetVPSProvisioner) VPSRegionForDelete(context.Context, int64) (string, error) {
	return "", nil
}
func (p resetVPSProvisioner) StartInstance(context.Context, int64) error   { return nil }
func (p resetVPSProvisioner) StopInstance(context.Context, int64) error    { return nil }
func (p resetVPSProvisioner) RestartInstance(context.Context, int64) error { return nil }
func (p resetVPSProvisioner) GetFirewallRules(context.Context, int64) ([]service.FirewallRule, error) {
	return nil, nil
}
func (p resetVPSProvisioner) UpdateFirewallRules(context.Context, int64, []service.FirewallRule) error {
	return nil
}
func (p resetVPSProvisioner) RefreshInstanceIPs(context.Context, int64) error { return nil }
func (p resetVPSProvisioner) ResetPassword(context.Context, int64, string) error {
	return nil
}

// ResetInstance mimics the real service: validates, then sets status to "resetting".
func (p resetVPSProvisioner) ResetInstance(ctx context.Context, vpsID int64) error {
	vps, err := p.repo.Get(ctx, vpsID)
	if err != nil {
		return err
	}
	if vps == nil {
		return fmt.Errorf("vps %d not found", vpsID)
	}
	if !vps.OCIInstanceID.Valid || vps.OCIInstanceID.String == "" {
		return fmt.Errorf("vps has no OCI instance ID")
	}
	if vps.Status != "running" && vps.Status != "stopped" {
		return fmt.Errorf("vps must be in running or stopped state to reset, current: %s", vps.Status)
	}
	return p.repo.UpdateStatus(ctx, vpsID, "resetting")
}

func performReset(handler *VPSHandler, id string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Post("/api/vps/{id}/reset", handler.HandleResetVPS)

	req := httptest.NewRequest(http.MethodPost, "/api/vps/"+id+"/reset", nil)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	return res
}

func TestHandleResetVPS_ValidRunningVPS_SetsResettingStatus(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := appdb.Run(database); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO templates (id, name, description, type, cloud_init_yaml) VALUES (1, 'Ubuntu', 'Ubuntu', 'predefined', '#cloud-config')`); err != nil {
		t.Fatalf("insert template: %v", err)
	}

	repo := repository.NewVPSRepository(database)
	vps, err := repo.Create(context.Background(), &model.VPS{
		DisplayName:      "reset-test",
		TemplateID:       1,
		Shape:            "VM.Standard.E4.Flex",
		OCPU:             1,
		MemoryGB:         8,
		BootVolumeSizeGB: 50,
		Status:           "running",
	})
	if err != nil {
		t.Fatalf("create vps: %v", err)
	}
	vps.OCIInstanceID = model.NullString{NullString: sql.NullString{String: "ocid1.instance.oc1..test", Valid: true}}
	if err := repo.Update(context.Background(), vps); err != nil {
		t.Fatalf("seed oci instance id: %v", err)
	}

	handler := NewVPSHandler(repo, nil, nil, nil, resetVPSProvisioner{repo: repo}, nil, logger.Nop(), nil)
	res := performReset(handler, fmt.Sprintf("%d", vps.ID))
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", res.Code, res.Body.String())
	}

	var response model.VPS
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Status != "resetting" {
		t.Fatalf("expected status 'resetting', got %q", response.Status)
	}
}

func TestHandleResetVPS_NotFound_Returns404(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := appdb.Run(database); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	repo := repository.NewVPSRepository(database)
	handler := NewVPSHandler(repo, nil, nil, nil, resetVPSProvisioner{repo: repo}, nil, logger.Nop(), nil)
	res := performReset(handler, "99999")
	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", res.Code, res.Body.String())
	}
}

func TestHandleResetVPS_PendingStatus_ReturnsError(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := appdb.Run(database); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO templates (id, name, description, type, cloud_init_yaml) VALUES (1, 'Ubuntu', 'Ubuntu', 'predefined', '#cloud-config')`); err != nil {
		t.Fatalf("insert template: %v", err)
	}

	repo := repository.NewVPSRepository(database)
	vps, err := repo.Create(context.Background(), &model.VPS{
		DisplayName:      "reset-pending",
		TemplateID:       1,
		Shape:            "VM.Standard.E4.Flex",
		OCPU:             1,
		MemoryGB:         8,
		BootVolumeSizeGB: 50,
		Status:           "pending",
	})
	if err != nil {
		t.Fatalf("create vps: %v", err)
	}
	vps.OCIInstanceID = model.NullString{NullString: sql.NullString{String: "ocid1.instance.oc1..test", Valid: true}}
	if err := repo.Update(context.Background(), vps); err != nil {
		t.Fatalf("seed oci instance id: %v", err)
	}

	handler := NewVPSHandler(repo, nil, nil, nil, resetVPSProvisioner{repo: repo}, nil, logger.Nop(), nil)
	res := performReset(handler, fmt.Sprintf("%d", vps.ID))
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", res.Code, res.Body.String())
	}
}

func TestHandleResetVPS_NoOCIInstanceID_ReturnsError(t *testing.T) {
	database, err := appdb.Open(filepath.Join(t.TempDir(), "test.db"), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := appdb.Run(database); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO templates (id, name, description, type, cloud_init_yaml) VALUES (1, 'Ubuntu', 'Ubuntu', 'predefined', '#cloud-config')`); err != nil {
		t.Fatalf("insert template: %v", err)
	}

	repo := repository.NewVPSRepository(database)
	vps, err := repo.Create(context.Background(), &model.VPS{
		DisplayName:      "reset-no-oci",
		TemplateID:       1,
		Shape:            "VM.Standard.E4.Flex",
		OCPU:             1,
		MemoryGB:         8,
		BootVolumeSizeGB: 50,
		Status:           "running",
	})
	if err != nil {
		t.Fatalf("create vps: %v", err)
	}

	handler := NewVPSHandler(repo, nil, nil, nil, resetVPSProvisioner{repo: repo}, nil, logger.Nop(), nil)
	res := performReset(handler, fmt.Sprintf("%d", vps.ID))
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", res.Code, res.Body.String())
	}
}
