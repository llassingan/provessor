package repository

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	appdb "github.com/llassingan/provessor/internal/db"
	"github.com/llassingan/provessor/internal/model"
)

func setupVPSRepositoryTest(t *testing.T) (*sql.DB, *VPSRepository) {
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

	return database, NewVPSRepository(database)
}

func createVPSForCallbackTest(t *testing.T, repo *VPSRepository, status string) *model.VPS {
	t.Helper()

	vps, err := repo.Create(context.Background(), &model.VPS{
		DisplayName:      "callback-test",
		TemplateID:       1,
		Shape:            "VM.Standard.E4.Flex",
		OCPU:             1,
		MemoryGB:         8,
		BootVolumeSizeGB: 50,
		Status:           status,
	})
	if err != nil {
		t.Fatalf("create vps: %v", err)
	}
	return vps
}

func TestConsumeCredentialsCallbackValidTokenSuccess(t *testing.T) {
	_, repo := setupVPSRepositoryTest(t)
	vps := createVPSForCallbackTest(t, repo, "provisioning")
	token := "valid-token"

	if err := repo.SetCredentialsCallbackToken(context.Background(), vps.ID, HashCredentialsCallbackToken(token), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("set token: %v", err)
	}

	consumed, err := repo.ConsumeCredentialsCallback(context.Background(), vps.ID, HashCredentialsCallbackToken(token), `{"username":"root","password":"secret"}`)
	if err != nil {
		t.Fatalf("consume token: %v", err)
	}
	if !consumed {
		t.Fatal("expected callback token to be consumed")
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps: %v", err)
	}
	if !updated.InitialCredentials.Valid || updated.InitialCredentials.String == "" {
		t.Fatal("expected initial credentials to be stored")
	}
	if updated.CredentialsCallbackTokenHash.Valid {
		t.Fatal("expected token hash to be cleared")
	}
	if updated.CredentialsCallbackTokenExpires.Valid {
		t.Fatal("expected token expiry to be cleared")
	}
	if !updated.CredentialsCallbackTokenUsedAt.Valid {
		t.Fatal("expected token used timestamp")
	}
	if !updated.CredentialsReceivedAt.Valid {
		t.Fatal("expected credentials received timestamp")
	}
}

func TestConsumeCredentialsCallbackRejectsWrongExpiredAndReplay(t *testing.T) {
	_, repo := setupVPSRepositoryTest(t)
	vps := createVPSForCallbackTest(t, repo, "provisioning")
	token := "valid-token"

	if err := repo.SetCredentialsCallbackToken(context.Background(), vps.ID, HashCredentialsCallbackToken(token), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("set token: %v", err)
	}

	consumed, err := repo.ConsumeCredentialsCallback(context.Background(), vps.ID, HashCredentialsCallbackToken("wrong-token"), `{"password":"wrong"}`)
	if err != nil {
		t.Fatalf("consume wrong token: %v", err)
	}
	if consumed {
		t.Fatal("wrong token should not consume callback")
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps after wrong token: %v", err)
	}
	if updated.InitialCredentials.Valid {
		t.Fatal("wrong token stored credentials")
	}
	if !updated.CredentialsCallbackTokenHash.Valid {
		t.Fatal("wrong token cleared callback token")
	}

	if err := repo.SetCredentialsCallbackToken(context.Background(), vps.ID, HashCredentialsCallbackToken(token), time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("set expired token: %v", err)
	}
	consumed, err = repo.ConsumeCredentialsCallback(context.Background(), vps.ID, HashCredentialsCallbackToken(token), `{"password":"expired"}`)
	if err != nil {
		t.Fatalf("consume expired token: %v", err)
	}
	if consumed {
		t.Fatal("expired token should not consume callback")
	}

	if err := repo.SetCredentialsCallbackToken(context.Background(), vps.ID, HashCredentialsCallbackToken(token), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("reset token: %v", err)
	}
	consumed, err = repo.ConsumeCredentialsCallback(context.Background(), vps.ID, HashCredentialsCallbackToken(token), `{"password":"first"}`)
	if err != nil {
		t.Fatalf("consume valid token: %v", err)
	}
	if !consumed {
		t.Fatal("expected valid token to consume callback")
	}
	consumed, err = repo.ConsumeCredentialsCallback(context.Background(), vps.ID, HashCredentialsCallbackToken(token), `{"password":"second"}`)
	if err != nil {
		t.Fatalf("replay token: %v", err)
	}
	if consumed {
		t.Fatal("replay should not consume callback")
	}

	updated, err = repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps after replay: %v", err)
	}
	if updated.InitialCredentials.String != `{"password":"first"}` {
		t.Fatalf("replay overwrote credentials: %s", updated.InitialCredentials.String)
	}
}

func TestConsumeCredentialsCallbackDoesNotChangeStatus(t *testing.T) {
	_, repo := setupVPSRepositoryTest(t)
	vps := createVPSForCallbackTest(t, repo, "provisioning")
	token := "valid-token"

	if err := repo.SetCredentialsCallbackToken(context.Background(), vps.ID, HashCredentialsCallbackToken(token), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("set token: %v", err)
	}

	consumed, err := repo.ConsumeCredentialsCallback(context.Background(), vps.ID, HashCredentialsCallbackToken(token), `{"password":"secret"}`)
	if err != nil {
		t.Fatalf("consume token: %v", err)
	}
	if !consumed {
		t.Fatal("expected callback token to be consumed")
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps: %v", err)
	}
	if updated.Status != "provisioning" {
		t.Fatalf("callback changed status: %s", updated.Status)
	}
}

func TestConsumeCredentialsCallbackRejectsIneligibleStatus(t *testing.T) {
	_, repo := setupVPSRepositoryTest(t)
	vps := createVPSForCallbackTest(t, repo, "failed")
	token := "valid-token"

	if err := repo.SetCredentialsCallbackToken(context.Background(), vps.ID, HashCredentialsCallbackToken(token), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("set token: %v", err)
	}

	consumed, err := repo.ConsumeCredentialsCallback(context.Background(), vps.ID, HashCredentialsCallbackToken(token), `{"password":"secret"}`)
	if err != nil {
		t.Fatalf("consume token: %v", err)
	}
	if consumed {
		t.Fatal("failed VPS should not accept credentials callback")
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps: %v", err)
	}
	if updated.InitialCredentials.Valid {
		t.Fatal("ineligible status stored credentials")
	}
	if !updated.CredentialsCallbackTokenHash.Valid {
		t.Fatal("ineligible status consumed callback token")
	}
}

func TestUpdatePreservesCredentialsReceivedByCallback(t *testing.T) {
	_, repo := setupVPSRepositoryTest(t)
	vps := createVPSForCallbackTest(t, repo, "provisioning")
	token := "valid-token"

	if err := repo.SetCredentialsCallbackToken(context.Background(), vps.ID, HashCredentialsCallbackToken(token), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("set token: %v", err)
	}
	loadedBeforeCallback, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get stale vps: %v", err)
	}

	consumed, err := repo.ConsumeCredentialsCallback(context.Background(), vps.ID, HashCredentialsCallbackToken(token), `{"password":"from-callback"}`)
	if err != nil {
		t.Fatalf("consume token: %v", err)
	}
	if !consumed {
		t.Fatal("expected callback token to be consumed")
	}

	loadedBeforeCallback.Status = "running"
	loadedBeforeCallback.PublicIP = model.NullString{NullString: sql.NullString{String: "203.0.113.10", Valid: true}}
	if err := repo.Update(context.Background(), loadedBeforeCallback); err != nil {
		t.Fatalf("update stale vps: %v", err)
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps: %v", err)
	}
	if updated.InitialCredentials.String != `{"password":"from-callback"}` {
		t.Fatalf("stale update overwrote callback credentials: %q", updated.InitialCredentials.String)
	}
	if updated.Status != "running" {
		t.Fatalf("expected normal update to keep working, got status %s", updated.Status)
	}
}

func TestUpdateSSHPasswordOnlyUpdatesPassword(t *testing.T) {
	_, repo := setupVPSRepositoryTest(t)
	vps := createVPSForCallbackTest(t, repo, "running")
	vps.SSHUsername = model.NullString{NullString: sql.NullString{String: "appuser", Valid: true}}
	vps.SSHPassword = model.NullString{NullString: sql.NullString{String: "old-password", Valid: true}}
	if err := repo.Update(context.Background(), vps); err != nil {
		t.Fatalf("seed ssh credentials: %v", err)
	}

	if err := repo.UpdateSSHPassword(context.Background(), vps.ID, "new-password"); err != nil {
		t.Fatalf("update ssh password: %v", err)
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps: %v", err)
	}
	if updated.SSHUsername.String != "appuser" {
		t.Fatalf("ssh username changed: %q", updated.SSHUsername.String)
	}
	if !updated.SSHPassword.Valid || updated.SSHPassword.String != "new-password" {
		t.Fatalf("ssh password not updated: %#v", updated.SSHPassword)
	}
}

func TestSSHHostKeyFingerprintSetIfUnset(t *testing.T) {
	_, repo := setupVPSRepositoryTest(t)
	vps := createVPSForCallbackTest(t, repo, "running")

	set, err := repo.SetSSHHostKeyFingerprintIfUnset(context.Background(), vps.ID, "SHA256:first")
	if err != nil {
		t.Fatalf("set fingerprint: %v", err)
	}
	if !set {
		t.Fatal("expected first fingerprint to be stored")
	}

	set, err = repo.SetSSHHostKeyFingerprintIfUnset(context.Background(), vps.ID, "SHA256:second")
	if err != nil {
		t.Fatalf("set second fingerprint: %v", err)
	}
	if set {
		t.Fatal("second fingerprint should not overwrite existing value")
	}

	fingerprint, err := repo.GetSSHHostKeyFingerprint(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get fingerprint: %v", err)
	}
	if !fingerprint.Valid || fingerprint.String != "SHA256:first" {
		t.Fatalf("unexpected fingerprint: %#v", fingerprint)
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get vps: %v", err)
	}
	if !updated.SSHHostKeyFingerprint.Valid || updated.SSHHostKeyFingerprint.String != "SHA256:first" {
		t.Fatalf("fingerprint not scanned on VPS: %#v", updated.SSHHostKeyFingerprint)
	}
}
