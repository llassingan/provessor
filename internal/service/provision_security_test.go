package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	appdb "github.com/llassingan/provessor/internal/db"
	"github.com/llassingan/provessor/internal/logger"
	"github.com/llassingan/provessor/internal/model"
	"github.com/llassingan/provessor/internal/repository"
	"github.com/llassingan/provessor/internal/sse"
)

func setupProvisionSecurityTest(t *testing.T) (*sql.DB, *repository.VPSRepository, *repository.NetworkRepository, *VPSProvisionService) {
	t.Helper()

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
	if _, err := database.Exec(`INSERT INTO networks (id, name, region, cidr_vcn, cidr_subnet, status) VALUES (1, 'Test', 'ap-test-1', '10.0.0.0/16', '10.0.1.0/24', 'active')`); err != nil {
		t.Fatalf("insert network: %v", err)
	}

	vpsRepo := repository.NewVPSRepository(database)
	networkRepo := repository.NewNetworkRepository(database)
	auditRepo := repository.NewAuditLogRepository(database)
	service := &VPSProvisionService{
		vpsRepo:     vpsRepo,
		networkRepo: networkRepo,
		broker:      sse.NewEventBroker(),
		log:         logger.Nop(),
		audit:       auditRepo,
	}
	return database, vpsRepo, networkRepo, service
}

func createResettableVPS(t *testing.T, repo *repository.VPSRepository, status string) *model.VPS {
	t.Helper()

	vps, err := repo.Create(context.Background(), &model.VPS{
		DisplayName:      "reset-test",
		TemplateID:       1,
		NetworkID:        model.NullInt64{NullInt64: sql.NullInt64{Int64: 1, Valid: true}},
		Shape:            "VM.Standard.E4.Flex",
		OCPU:             1,
		MemoryGB:         8,
		BootVolumeSizeGB: 50,
		Status:           status,
	})
	if err != nil {
		t.Fatalf("create vps: %v", err)
	}
	vps.OCIInstanceID = model.NullString{NullString: sql.NullString{String: "ocid1.instance.test", Valid: true}}
	vps.PublicIP = model.NullString{NullString: sql.NullString{String: "203.0.113.10", Valid: true}}
	vps.SSHUsername = model.NullString{NullString: sql.NullString{String: "appuser", Valid: true}}
	vps.SSHPassword = model.NullString{NullString: sql.NullString{String: "old-password", Valid: true}}
	vps.SSHPrivateKey = model.NullString{NullString: sql.NullString{String: "-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----", Valid: true}}
	if err := repo.Update(context.Background(), vps); err != nil {
		t.Fatalf("seed resettable vps: %v", err)
	}
	return vps
}

func withResetPasswordSeams(t *testing.T, reset func(context.Context, sshHostKeyRepository, int64, string, string, string, string) error, verify func(context.Context, sshHostKeyRepository, int64, string, string, string) error) {
	t.Helper()
	origReset := sshResetPasswordFn
	origVerify := sshVerifyPasswordLoginFn
	sshResetPasswordFn = reset
	sshVerifyPasswordLoginFn = verify
	t.Cleanup(func() {
		sshResetPasswordFn = origReset
		sshVerifyPasswordLoginFn = origVerify
	})
}

func TestValidateResetPasswordPolicy(t *testing.T) {
	cases := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "too short", password: "short", wantErr: true},
		{name: "too long", password: strings.Repeat("a", 129), wantErr: true},
		{name: "whitespace only", password: strings.Repeat(" ", 12), wantErr: true},
		{name: "newline", password: "valid-password\n", wantErr: true},
		{name: "carriage return", password: "valid-password\r", wantErr: true},
		{name: "nul", password: "valid-pass\x00word", wantErr: true},
		{name: "valid", password: "valid-pass12", wantErr: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateResetPassword(tc.password)
			if tc.wantErr && !errors.Is(err, ErrResetPasswordPolicy) {
				t.Fatalf("expected policy error, got %v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected valid password, got %v", err)
			}
		})
	}
}

func TestBuildChpasswdCommandUsesPrintfAndSingleQuote(t *testing.T) {
	command := buildChpasswdCommand("app'user", "valid'password12")
	want := "printf '%s\\n' 'app'\\''user:valid'\\''password12' | chpasswd"
	if command != want {
		t.Fatalf("unexpected command:\n got %q\nwant %q", command, want)
	}
	if strings.Contains(command, "root:") {
		t.Fatalf("command targets root: %s", command)
	}
}

func TestResetPasswordTargetsSSHUserVerifiesBeforePersisting(t *testing.T) {
	_, repo, _, service := setupProvisionSecurityTest(t)
	vps := createResettableVPS(t, repo, "running")
	newPassword := "valid-password12"

	var verifyCalled bool
	var capturedTargetUser, capturedNewPassword string
	withResetPasswordSeams(t,
		func(ctx context.Context, hostKeyRepo sshHostKeyRepository, id int64, host string, privateKey string, targetUser string, password string) error {
			if id != vps.ID || host != "203.0.113.10" || targetUser != "appuser" || password != newPassword {
				t.Fatalf("unexpected reset args id=%d host=%q targetUser=%q password=%q", id, host, targetUser, password)
			}
			if privateKey == "" {
				t.Fatal("private key is empty")
			}
			capturedTargetUser = targetUser
			capturedNewPassword = password
			return nil
		},
		func(ctx context.Context, hostKeyRepo sshHostKeyRepository, id int64, host string, username string, password string) error {
			verifyCalled = true
			if id != vps.ID || host != "203.0.113.10" || username != "appuser" || password != newPassword {
				t.Fatalf("unexpected verify args id=%d host=%q username=%q password=%q", id, host, username, password)
			}
			loaded, err := repo.Get(ctx, id)
			if err != nil {
				t.Fatalf("get during verify: %v", err)
			}
			if loaded.SSHPassword.String != "old-password" {
				t.Fatalf("password persisted before verification: %q", loaded.SSHPassword.String)
			}
			return nil
		},
	)

	if err := service.ResetPassword(context.Background(), vps.ID, newPassword); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if !verifyCalled {
		t.Fatal("expected password verification")
	}
	if capturedTargetUser != "appuser" {
		t.Fatalf("SSH reset targeted wrong user: %q", capturedTargetUser)
	}
	if capturedNewPassword != newPassword {
		t.Fatalf("SSH reset used wrong password: %q", capturedNewPassword)
	}

	updated, err := repo.Get(context.Background(), vps.ID)
	if err != nil {
		t.Fatalf("get updated vps: %v", err)
	}
	if updated.SSHPassword.String != newPassword {
		t.Fatalf("password not persisted after verification: %q", updated.SSHPassword.String)
	}
}

func TestResetPasswordDoesNotPersistWhenVerificationFails(t *testing.T) {
	_, repo, _, service := setupProvisionSecurityTest(t)
	vps := createResettableVPS(t, repo, "running")

	withResetPasswordSeams(t,
		func(context.Context, sshHostKeyRepository, int64, string, string, string, string) error {
			return nil
		},
		func(context.Context, sshHostKeyRepository, int64, string, string, string) error {
			return errors.New("verify failed")
		},
	)

	err := service.ResetPassword(context.Background(), vps.ID, "valid-password12")
	if err == nil {
		t.Fatal("expected verification failure")
	}
	updated, getErr := repo.Get(context.Background(), vps.ID)
	if getErr != nil {
		t.Fatalf("get updated vps: %v", getErr)
	}
	if updated.SSHPassword.String != "old-password" {
		t.Fatalf("password changed after verification failure: %q", updated.SSHPassword.String)
	}
}

func TestResetPasswordDoesNotPersistWhenSSHResetFails(t *testing.T) {
	_, repo, _, service := setupProvisionSecurityTest(t)
	vps := createResettableVPS(t, repo, "running")

	withResetPasswordSeams(t,
		func(context.Context, sshHostKeyRepository, int64, string, string, string, string) error {
			return errors.New("ssh reset failed")
		},
		func(context.Context, sshHostKeyRepository, int64, string, string, string) error {
			t.Fatal("verification should not run after SSH reset failure")
			return nil
		},
	)

	err := service.ResetPassword(context.Background(), vps.ID, "valid-password12")
	if err == nil {
		t.Fatal("expected command failure")
	}
	updated, getErr := repo.Get(context.Background(), vps.ID)
	if getErr != nil {
		t.Fatalf("get updated vps: %v", getErr)
	}
	if updated.SSHPassword.String != "old-password" {
		t.Fatalf("password changed after command failure: %q", updated.SSHPassword.String)
	}
}

func TestResetPasswordRequiresRunningProvisionedVPS(t *testing.T) {
	_, repo, _, service := setupProvisionSecurityTest(t)
	vps := createResettableVPS(t, repo, "stopped")

	withResetPasswordSeams(t,
		func(context.Context, sshHostKeyRepository, int64, string, string, string, string) error {
			t.Fatal("SSH reset should not run for stopped VPS")
			return nil
		},
		func(context.Context, sshHostKeyRepository, int64, string, string, string) error {
			t.Fatal("verification should not run for stopped VPS")
			return nil
		},
	)

	if err := service.ResetPassword(context.Background(), vps.ID, "valid-password12"); err == nil {
		t.Fatal("expected stopped VPS to be rejected")
	}

	vps.Status = "running"
	vps.OCIInstanceID = model.NullString{NullString: sql.NullString{}}
	if err := repo.Update(context.Background(), vps); err != nil {
		t.Fatalf("clear instance id: %v", err)
	}
	if err := service.ResetPassword(context.Background(), vps.ID, "valid-password12"); err == nil {
		t.Fatal("expected missing OCI instance ID to be rejected")
	}

	vps.OCIInstanceID = model.NullString{NullString: sql.NullString{String: "ocid1.instance.test", Valid: true}}
	vps.SSHUsername = model.NullString{NullString: sql.NullString{}}
	if err := repo.Update(context.Background(), vps); err != nil {
		t.Fatalf("clear ssh username: %v", err)
	}
	if err := service.ResetPassword(context.Background(), vps.ID, "valid-password12"); err == nil {
		t.Fatal("expected missing SSH username to be rejected")
	}

	vps.SSHUsername = model.NullString{NullString: sql.NullString{String: "root", Valid: true}}
	if err := repo.Update(context.Background(), vps); err != nil {
		t.Fatalf("set root username: %v", err)
	}
	if err := service.ResetPassword(context.Background(), vps.ID, "valid-password12"); err == nil {
		t.Fatal("expected root SSH username to be rejected")
	}

	vps.SSHUsername = model.NullString{NullString: sql.NullString{String: "appuser", Valid: true}}
	vps.PublicIP = model.NullString{NullString: sql.NullString{}}
	if err := repo.Update(context.Background(), vps); err != nil {
		t.Fatalf("clear public ip: %v", err)
	}
	if err := service.ResetPassword(context.Background(), vps.ID, "valid-password12"); err == nil {
		t.Fatal("expected missing public IP to be rejected")
	}

	vps.PublicIP = model.NullString{NullString: sql.NullString{String: "203.0.113.10", Valid: true}}
	vps.SSHPrivateKey = model.NullString{NullString: sql.NullString{}}
	if err := repo.Update(context.Background(), vps); err != nil {
		t.Fatalf("clear private key: %v", err)
	}
	if err := service.ResetPassword(context.Background(), vps.ID, "valid-password12"); err == nil {
		t.Fatal("expected missing SSH private key to be rejected")
	}
}

type fakeHostKeyRepo struct {
	fingerprint sql.NullString
	setCalls    int
	setResult   bool
}

func (r *fakeHostKeyRepo) GetSSHHostKeyFingerprint(context.Context, int64) (sql.NullString, error) {
	return r.fingerprint, nil
}

func (r *fakeHostKeyRepo) SetSSHHostKeyFingerprintIfUnset(_ context.Context, _ int64, fingerprint string) (bool, error) {
	r.setCalls++
	if r.fingerprint.Valid && r.fingerprint.String != "" {
		return false, nil
	}
	if !r.setResult {
		return false, nil
	}
	r.fingerprint = sql.NullString{String: fingerprint, Valid: true}
	return true, nil
}

func TestVerifyAndPersistSSHHostKey(t *testing.T) {
	ctx := context.Background()

	t.Run("stores unset fingerprint", func(t *testing.T) {
		repo := &fakeHostKeyRepo{setResult: true}
		if err := verifyAndPersistSSHHostKey(ctx, repo, 1, "SHA256:first", sql.NullString{}); err != nil {
			t.Fatalf("verify host key: %v", err)
		}
		if repo.setCalls != 1 || repo.fingerprint.String != "SHA256:first" {
			t.Fatalf("fingerprint not stored: calls=%d fingerprint=%#v", repo.setCalls, repo.fingerprint)
		}
	})

	t.Run("accepts matching fingerprint", func(t *testing.T) {
		stored := sql.NullString{String: "SHA256:first", Valid: true}
		repo := &fakeHostKeyRepo{fingerprint: stored}
		if err := verifyAndPersistSSHHostKey(ctx, repo, 1, "SHA256:first", stored); err != nil {
			t.Fatalf("verify host key: %v", err)
		}
		if repo.setCalls != 0 {
			t.Fatalf("matching fingerprint should not rewrite, calls=%d", repo.setCalls)
		}
	})

	t.Run("rejects mismatch", func(t *testing.T) {
		stored := sql.NullString{String: "SHA256:first", Valid: true}
		repo := &fakeHostKeyRepo{fingerprint: stored}
		if err := verifyAndPersistSSHHostKey(ctx, repo, 1, "SHA256:second", stored); err == nil {
			t.Fatal("expected mismatch error")
		}
		if repo.fingerprint.String != "SHA256:first" {
			t.Fatalf("mismatch overwrote fingerprint: %#v", repo.fingerprint)
		}
	})

	t.Run("rejects empty presented fingerprint", func(t *testing.T) {
		repo := &fakeHostKeyRepo{setResult: true}
		if err := verifyAndPersistSSHHostKey(ctx, repo, 1, "", sql.NullString{}); err == nil {
			t.Fatal("expected empty fingerprint error")
		}
	})
}

func TestSSHHostKeyCallbackRejectsStoredMismatchPreAuth(t *testing.T) {
	storedKey := newTestSSHPublicKey(t)
	mismatchKey := newTestSSHPublicKey(t)
	storedFingerprint := ssh.FingerprintSHA256(storedKey)
	mismatchFingerprint := ssh.FingerprintSHA256(mismatchKey)

	var presentedFingerprint string
	callback := sshHostKeyCallback(sql.NullString{String: storedFingerprint, Valid: true}, &presentedFingerprint, 42)
	if err := callback("example.test", nil, mismatchKey); err == nil {
		t.Fatal("expected stored fingerprint mismatch to fail in HostKeyCallback")
	}
	if presentedFingerprint != mismatchFingerprint {
		t.Fatalf("callback did not capture presented fingerprint: got %q want %q", presentedFingerprint, mismatchFingerprint)
	}

}

func TestSSHHostKeyCallbackAcceptsStoredMatchAndUnsetPreAuth(t *testing.T) {
	key := newTestSSHPublicKey(t)
	fingerprint := ssh.FingerprintSHA256(key)

	var matchedFingerprint string
	matchCallback := sshHostKeyCallback(sql.NullString{String: fingerprint, Valid: true}, &matchedFingerprint, 42)
	if err := matchCallback("example.test", nil, key); err != nil {
		t.Fatalf("expected matching stored fingerprint to pass: %v", err)
	}
	if matchedFingerprint != fingerprint {
		t.Fatalf("callback did not capture matching fingerprint: got %q want %q", matchedFingerprint, fingerprint)
	}

	var firstUseFingerprint string
	firstUseCallback := sshHostKeyCallback(sql.NullString{}, &firstUseFingerprint, 42)
	if err := firstUseCallback("example.test", nil, key); err != nil {
		t.Fatalf("expected unset fingerprint to pass pre-auth: %v", err)
	}
	if firstUseFingerprint != fingerprint {
		t.Fatalf("callback did not capture first-use fingerprint: got %q want %q", firstUseFingerprint, fingerprint)
	}
}

func newTestSSHPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("new ssh public key: %v", err)
	}
	return publicKey
}
