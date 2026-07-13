package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/computeinstanceagent"
	"github.com/oracle/oci-go-sdk/v65/core"

	"github.com/llassingan/provessor/internal/logger"
	"github.com/llassingan/provessor/internal/model"
	"github.com/llassingan/provessor/internal/repository"
	"github.com/llassingan/provessor/internal/sse"
)

var ErrResetPasswordPolicy = errors.New("reset password policy violation")

// ── OCI Instance Agent (retained for future use) ─────────────────────
// These are intentionally unused. The Oracle Cloud Agent snap on Ubuntu
// does not yet ship the Compute Instance Run Command plugin (as of
// v1.60.0). ResetPassword uses SSH instead. Restore these when Oracle
// releases a snap with Run Command for Ubuntu (announced for v1.61.0).
// ─────────────────────────────────────────────────────────────────────

var createInstanceAgentCommand = func(ctx context.Context, computeService *OCIComputeService, region string, request computeinstanceagent.CreateInstanceAgentCommandRequest) (string, error) {
	instanceAgentClient, err := computeService.GetInstanceAgentClient(ctx, region)
	if err != nil {
		return "", fmt.Errorf("get instance agent client: %w", err)
	}
	response, err := instanceAgentClient.CreateInstanceAgentCommand(ctx, request)
	if err != nil {
		return "", err
	}
	if response.InstanceAgentCommand.Id == nil || *response.InstanceAgentCommand.Id == "" {
		return "", fmt.Errorf("instance agent command response missing command id")
	}
	return *response.InstanceAgentCommand.Id, nil
}

var waitInstanceAgentCommandExecution = func(ctx context.Context, computeService *OCIComputeService, region string, instanceID string, commandID string) error {
	instanceAgentClient, err := computeService.GetInstanceAgentClient(ctx, region)
	if err != nil {
		return fmt.Errorf("get instance agent client: %w", err)
	}

	deadline := time.Now().Add(2 * time.Minute)
	startTime := time.Now()
	var lastState string
	for {
		response, err := instanceAgentClient.GetInstanceAgentCommandExecution(ctx, computeinstanceagent.GetInstanceAgentCommandExecutionRequest{
			InstanceAgentCommandId: common.String(commandID),
			InstanceId:             common.String(instanceID),
		})
		if err != nil {
			return fmt.Errorf("get instance agent command execution: %w", err)
		}

		elapsed := time.Since(startTime).Round(time.Second)
		execution := response.InstanceAgentCommandExecution
		currentState := string(execution.LifecycleState)
		if currentState != lastState {
			log.Printf("[DEBUG] wait_agent_command: command=%s state=%s elapsed=%s", maskOCID(commandID), currentState, elapsed)
			lastState = currentState
		}
		switch execution.LifecycleState {
		case computeinstanceagent.InstanceAgentCommandExecutionLifecycleStateSucceeded:
			if execution.Content != nil && execution.Content.GetExitCode() != nil && *execution.Content.GetExitCode() != 0 {
				return fmt.Errorf("instance agent command exited with code %d", *execution.Content.GetExitCode())
			}
			return nil
		case computeinstanceagent.InstanceAgentCommandExecutionLifecycleStateFailed,
			computeinstanceagent.InstanceAgentCommandExecutionLifecycleStateTimedOut,
			computeinstanceagent.InstanceAgentCommandExecutionLifecycleStateCanceled:
			return fmt.Errorf("instance agent command execution ended with state %s", execution.LifecycleState)
		}

		if time.Now().After(deadline) {
			log.Printf("[DEBUG] wait_agent_command: command=%s deadline exceeded, last_state=%s elapsed=%s", maskOCID(commandID), lastState, elapsed)
			return fmt.Errorf("instance agent command execution did not complete before timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

var getComputeCompartmentOCID = func(ctx context.Context, computeService *OCIComputeService) (string, error) {
	return computeService.GetCompartmentOCID(ctx)
}

var sshCreateUserFn = SSHCreateUser
var sshVerifyPasswordLoginFn = SSHVerifyPasswordLogin
var sshResetPasswordFn = SSHResetPassword
var checkInstanceAgentAvailable = func(ctx context.Context, computeService *OCIComputeService, region, instanceID string) error {
	inst, err := computeService.GetInstance(ctx, region, instanceID)
	if err != nil {
		log.Printf("[DEBUG] check_agent: get instance failed, proceeding anyway: %v", err)
		return nil
	}
	if inst.AgentConfig == nil || len(inst.AgentConfig.PluginsConfig) == 0 {
		log.Printf("[DEBUG] check_agent: agent config is empty, proceeding anyway (agent may be snap-installed)")
		return nil
	}
	for _, p := range inst.AgentConfig.PluginsConfig {
		if p.Name != nil && *p.Name == "Compute Instance Run Command" {
			if p.DesiredState == core.InstanceAgentPluginConfigDetailsDesiredStateEnabled {
				return nil
			}
			log.Printf("[DEBUG] check_agent: Run Command plugin desired state is %q, proceeding anyway", p.DesiredState)
			return nil
		}
	}
	log.Printf("[DEBUG] check_agent: Run Command plugin not found in agent config, proceeding anyway (agent may be snap-installed)")
	return nil
}

func ValidateResetPassword(password string) error {
	if len(password) < 12 {
		return fmt.Errorf("%w: password must be at least 12 characters", ErrResetPasswordPolicy)
	}
	if len(password) > 128 {
		return fmt.Errorf("%w: password must be at most 128 characters", ErrResetPasswordPolicy)
	}
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("%w: password must not be whitespace only", ErrResetPasswordPolicy)
	}
	if strings.ContainsAny(password, "\n\r\x00") {
		return fmt.Errorf("%w: password must not contain newline, carriage return, or NUL", ErrResetPasswordPolicy)
	}
	return nil
}

type VPSProvisionService struct {
	computeService  *OCIComputeService
	vpsRepo         *repository.VPSRepository
	vpsResourceRepo *repository.VPSResourceRepository
	networkRepo     *repository.NetworkRepository
	templateRepo    *repository.TemplateRepository
	broker          *sse.EventBroker
	settingsRepo    *repository.SettingsRepository
	apiURL          string
	log             *logger.Logger
	audit           *repository.AuditLogRepository
}

func NewVPSProvisionService(
	computeService *OCIComputeService,
	vpsRepo *repository.VPSRepository,
	vpsResourceRepo *repository.VPSResourceRepository,
	networkRepo *repository.NetworkRepository,
	templateRepo *repository.TemplateRepository,
	broker *sse.EventBroker,
	settingsRepo *repository.SettingsRepository,
	apiURL string,
	log *logger.Logger,
	audit *repository.AuditLogRepository,
) *VPSProvisionService {
	return &VPSProvisionService{
		computeService:  computeService,
		vpsRepo:         vpsRepo,
		vpsResourceRepo: vpsResourceRepo,
		networkRepo:     networkRepo,
		templateRepo:    templateRepo,
		broker:          broker,
		settingsRepo:    settingsRepo,
		apiURL:          apiURL,
		log:             log,
		audit:           audit,
	}
}

func (s *VPSProvisionService) ProvisionVPS(ctx context.Context, vpsID int64) (err error) {
	defer func() {
		status := "success"
		errMsg := ""
		if err != nil {
			status = "failure"
			errMsg = sanitize(err.Error())
		}
		s.audit.Log(ctx, model.AuditLog{Operation: "vps.provision", ResourceType: "vps", ResourceID: vpsID, Status: status, ErrorMessage: errMsg})
	}()

	channel := fmt.Sprintf("vps:%d", vpsID)

	emit := func(step, status, message string) {
		s.broker.Publish(channel, sse.SSEEvent{
			Type:    "status",
			Status:  status,
			Step:    step,
			Message: message,
		})
	}

	emit("fetching_vps", "provisioning", "Loading VPS details")

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil || vps == nil {
		if vps == nil {
			emit("error", "failed", "VPS not found")
			return fmt.Errorf("vps %d not found", vpsID)
		}
		emit("error", "failed", "Failed to load VPS: "+err.Error())
		return fmt.Errorf("get vps: %w", err)
	}

	if err := s.vpsRepo.UpdateStatus(ctx, vpsID, "provisioning"); err != nil {
		emit("error", "failed", "Failed to update VPS status")
		return fmt.Errorf("update status: %w", err)
	}

	if !vps.NetworkID.Valid {
		s.vpsRepo.UpdateStatus(ctx, vpsID, "failed")
		emit("error", "failed", "VPS has no network assigned")
		return fmt.Errorf("vps has no network")
	}

	emit("loading_network", "provisioning", "Loading network details")
	network, err := s.networkRepo.Get(ctx, vps.NetworkID.Int64)
	if err != nil || network == nil {
		s.vpsRepo.UpdateStatus(ctx, vpsID, "failed")
		emit("error", "failed", "Failed to load network")
		return fmt.Errorf("get network: %w", err)
	}

	emit("loading_template", "provisioning", "Loading template")
	template, err := s.templateRepo.Get(ctx, vps.TemplateID)
	if err != nil || template == nil {
		s.vpsRepo.UpdateStatus(ctx, vpsID, "failed")
		emit("error", "failed", "Failed to load template")
		return fmt.Errorf("get template: %w", err)
	}

	compartmentOCID, err := s.computeService.GetCompartmentOCID(ctx)
	if err != nil {
		s.vpsRepo.UpdateStatus(ctx, vpsID, "failed")
		emit("error", "failed", "Failed to get compartment: "+err.Error())
		return fmt.Errorf("get compartment: %w", err)
	}

	emit("generating_keys", "provisioning", "Generating SSH key pair")
	s.log.Debug("provision_vps_generating_ssh_key", "vps_id", vpsID)

	publicKey, privateKeyPEM, err := GenerateSSHKeyPair()
	if err != nil {
		s.vpsRepo.UpdateStatus(ctx, vpsID, "failed")
		emit("error", "failed", "Failed to generate SSH keys: "+err.Error())
		return fmt.Errorf("generate SSH key pair: %w", err)
	}

	s.log.Debug("provision_vps_ssh_key_generated", "vps_id", vpsID)

	callbackToken, err := repository.GenerateCredentialsCallbackToken()
	if err != nil {
		s.vpsRepo.UpdateStatus(ctx, vpsID, "failed")
		emit("error", "failed", "Failed to generate callback token")
		return fmt.Errorf("generate callback token: %w", err)
	}
	if err := s.vpsRepo.SetCredentialsCallbackToken(ctx, vpsID, repository.HashCredentialsCallbackToken(callbackToken), time.Now().UTC().Add(24*time.Hour)); err != nil {
		s.vpsRepo.UpdateStatus(ctx, vpsID, "failed")
		emit("error", "failed", "Failed to save callback token")
		return fmt.Errorf("save callback token: %w", err)
	}

	cloudInitYAML := template.CloudInitYAML
	cloudInitYAML = strings.ReplaceAll(cloudInitYAML, "API_HOST", s.apiURL)
	cloudInitYAML = strings.ReplaceAll(cloudInitYAML, "INSTANCE_ID", fmt.Sprintf("%d", vpsID))
	cloudInitYAML = strings.ReplaceAll(cloudInitYAML, "API_TOKEN", callbackToken)

	s.log.Debug("provision_vps_cloud_init_prepared", "vps_id", vpsID, "len", len(cloudInitYAML))

	// Create NSG before LaunchInstance (Option A: attach at VNIC creation time)
	emit("creating_nsg", "provisioning", "Creating network security group")
	_ = s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateCreatingNSG))
	nsgID, nsgErr := s.computeService.CreateNSG(ctx, network.Region, compartmentOCID, network.VCNOCID, "nsg-"+vps.DisplayName, vpsID)
	if nsgErr != nil {
		emit("error", "failed", "Failed to create NSG: "+nsgErr.Error())
		_ = s.vpsRepo.UpdateStatus(ctx, vpsID, "failed")
		_ = s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSFailed))
		return fmt.Errorf("create NSG: %w", nsgErr)
	}
	if _, terr := s.vpsResourceRepo.Create(ctx, vpsID, "nsg", nsgID); terr != nil {
		s.log.Warn("provision_vps_track_nsg_failed", "vps_id", vpsID, "error", terr)
	}
	if uerr := s.vpsRepo.UpdateNSGID(ctx, vpsID, nsgID); uerr != nil {
		s.log.Warn("provision_vps_save_nsg_id_failed", "vps_id", vpsID, "error", uerr)
	}
	vps.NSGID = nsgID

	// Add default NSG rules: HTTP (80) + HTTPS (443) from 0.0.0.0/0 (NO SSH — that's in the security list backdoor)
	defaultRules := []core.AddSecurityRuleDetails{
		{
			Direction:  core.AddSecurityRuleDetailsDirectionIngress,
			Protocol:   common.String("6"),
			Source:     common.String("0.0.0.0/0"),
			SourceType: core.AddSecurityRuleDetailsSourceTypeCidrBlock,
			TcpOptions: &core.TcpOptions{
				DestinationPortRange: &core.PortRange{Min: common.Int(80), Max: common.Int(80)},
			},
		},
		{
			Direction:  core.AddSecurityRuleDetailsDirectionIngress,
			Protocol:   common.String("6"),
			Source:     common.String("0.0.0.0/0"),
			SourceType: core.AddSecurityRuleDetailsSourceTypeCidrBlock,
			TcpOptions: &core.TcpOptions{
				DestinationPortRange: &core.PortRange{Min: common.Int(443), Max: common.Int(443)},
			},
		},
	}
	if aerr := s.computeService.AddNSGRules(ctx, network.Region, nsgID, defaultRules); aerr != nil {
		emit("error", "failed", "Failed to add default NSG rules: "+aerr.Error())
		_ = s.computeService.DeleteNSG(ctx, network.Region, nsgID)
		_ = s.vpsResourceRepo.MarkDeleted(ctx, nsgID)
		_ = s.vpsRepo.UpdateNSGID(ctx, vpsID, "")
		_ = s.vpsRepo.UpdateStatus(ctx, vpsID, "failed")
		_ = s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSFailed))
		return fmt.Errorf("add default NSG rules: %w", aerr)
	}
	emit("nsg_ready", "provisioning", "Network security group ready with HTTP/HTTPS defaults")
	_ = s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateAttachingNSG))

	emit("launching_instance", "provisioning", "Launching OCI instance")
	instanceID, err := s.computeService.LaunchInstance(ctx, LaunchInstanceParams{
		Region:           network.Region,
		CompartmentOCID:  compartmentOCID,
		SubnetOCID:       network.SubnetOCID,
		DisplayName:      vps.DisplayName,
		Shape:            vps.Shape,
		OCPU:             vps.OCPU,
		MemoryGB:         vps.MemoryGB,
		BootVolumeSizeGB: vps.BootVolumeSizeGB,
		CloudInitYAML:    cloudInitYAML,
		NSGID:            nsgID,
		SSHPublicKey:     publicKey,
	})
	if err != nil {
		emit("error", "failed", "Failed to launch instance: "+err.Error())
		s.RollbackVPS(ctx, vpsID, network.Region, "")
		return fmt.Errorf("launch instance: %w", err)
	}

	_ = s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateCreatingInstance))
	if _, terr := s.vpsResourceRepo.Create(ctx, vpsID, "instance", instanceID); terr != nil {
		s.log.Warn("provision_vps_track_instance_failed", "vps_id", vpsID, "error", terr)
	}
	if uerr := s.vpsResourceRepo.UpdateStatus(ctx, instanceID, "created"); uerr != nil {
		s.log.Warn("provision_vps_update_resource_status_failed", "vps_id", vpsID, "error", uerr)
	}

	vps.OCIInstanceID = model.NullString{NullString: sql.NullString{String: instanceID, Valid: true}}
	vps.Status = "provisioning"
	if err := s.vpsRepo.Update(ctx, vps); err != nil {
		emit("error", "failed", "Failed to update VPS: "+err.Error())
		return fmt.Errorf("update vps: %w", err)
	}

	emit("waiting_for_boot", "provisioning", "Waiting for instance to boot")

	s.log.Debug("provision_vps_waiting_running", "vps_id", vpsID, "instance_id", instanceID)

	_, err = s.waitForRunning(ctx, vpsID, network.Region, instanceID)
	if err != nil {
		emit("error", "failed", "Instance failed to start: "+err.Error())
		s.RollbackVPS(ctx, vpsID, network.Region, instanceID)
		return fmt.Errorf("wait for running: %w", err)
	}

	s.log.Debug("provision_vps_instance_running", "vps_id", vpsID, "instance_id", instanceID)

	vps.OCIInstanceID = model.NullString{NullString: sql.NullString{String: instanceID, Valid: true}}
	vps.Status = "running"

	emit("fetching_ips", "provisioning", "Retrieving instance IP addresses")

	compartmentOCID, err = s.computeService.GetCompartmentOCID(ctx)
	if err != nil {
		s.log.Error("provision_vps_get_compartment_failed", "vps_id", vpsID, "error", err)
		emit("error", "failed", "Failed to get compartment: "+err.Error())
		s.RollbackVPS(ctx, vpsID, network.Region, instanceID)
		return fmt.Errorf("get compartment: %w", err)
	}

	publicIP, privateIP, ipErr := s.computeService.GetInstanceIPs(ctx, network.Region, instanceID, compartmentOCID)
	if ipErr != nil {
		s.log.Error("provision_vps_get_ips_failed", "vps_id", vpsID, "error", ipErr)
		emit("error", "failed", "Failed to retrieve instance IPs: "+ipErr.Error())
		s.RollbackVPS(ctx, vpsID, network.Region, instanceID)
		return fmt.Errorf("get instance IPs: %w", ipErr)
	}

	if publicIP != "" {
		vps.PublicIP = model.NullString{NullString: sql.NullString{String: publicIP, Valid: true}}
	}
	if privateIP != "" {
		vps.PrivateIP = model.NullString{NullString: sql.NullString{String: privateIP, Valid: true}}
	}
	s.log.Debug("provision_vps_ips_retrieved", "vps_id", vpsID, "public_ip", publicIP, "private_ip", privateIP)

	if privateKeyPEM != "" {
		emit("setting_up_ssh", "provisioning", "Creating SSH user")
		sshUser := sanitizeUsername(vps.DisplayName)
		sshPass := generatePassword(16)
		s.log.Debug("provision_vps_creating_ssh_user", "vps_id", vpsID, "ssh_user", sshUser, "host", publicIP)

		var sshErr error
		for attempt := 1; attempt <= 20; attempt++ {
			sshErr = sshCreateUserFn(ctx, s.vpsRepo, vpsID, publicIP, privateKeyPEM, sshUser, sshPass)
			if sshErr == nil {
				emit("ssh_connected", "provisioning", "SSH user created successfully")
				break
			}
			s.log.Debug("provision_vps_ssh_attempt_failed", "vps_id", vpsID, "attempt", attempt, "error", sshErr)
			if attempt < 20 {
				emit("ssh_retry", "provisioning", fmt.Sprintf("Waiting for SSH daemon (attempt %d/20)", attempt))
				time.Sleep(10 * time.Second)
			} else {
				emit("ssh_failed", "provisioning", "SSH user creation failed after 20 attempts")
			}
		}

		if sshErr != nil {
			s.log.Error("provision_vps_ssh_user_creation_failed", "vps_id", vpsID, "error", sshErr)
			emit("error", "failed", "SSH user creation failed after 20 attempts")
			s.RollbackVPS(ctx, vpsID, network.Region, instanceID)
			return fmt.Errorf("ssh create user: %w", sshErr)
		}

		vps.SSHPrivateKey = model.NullString{NullString: sql.NullString{String: privateKeyPEM, Valid: true}}
		vps.SSHUsername = model.NullString{NullString: sql.NullString{String: sshUser, Valid: true}}
		vps.SSHPassword = model.NullString{NullString: sql.NullString{String: sshPass, Valid: true}}
		s.log.Debug("provision_vps_ssh_credentials_saved", "vps_id", vpsID, "ssh_user", sshUser)

		if verifyErr := sshVerifyPasswordLoginFn(ctx, s.vpsRepo, vpsID, publicIP, sshUser, sshPass); verifyErr != nil {
			s.log.Debug("provision_vps_password_verify_failed", "vps_id", vpsID, "error", verifyErr)
		} else {
			s.log.Debug("provision_vps_password_verified", "vps_id", vpsID, "ssh_user", sshUser)
		}
	}

	if err := s.vpsRepo.Update(ctx, vps); err != nil {
		emit("error", "failed", "Failed to update VPS: "+err.Error())
		return fmt.Errorf("update vps: %w", err)
	}

	emit("ready", "running", "VPS instance is ready")

	eventData := map[string]string{"instance_id": instanceID}
	if vps.PublicIP.Valid {
		eventData["public_ip"] = vps.PublicIP.String
	}
	if vps.PrivateIP.Valid {
		eventData["private_ip"] = vps.PrivateIP.String
	}

	s.log.Debug("provision_vps_complete", "vps_id", vpsID)
	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "status",
		Status:  "running",
		Step:    "ready",
		Message: "VPS instance provisioned successfully",
		Data:    eventData,
	})

	return nil
}

func (s *VPSProvisionService) waitForRunning(ctx context.Context, vpsID int64, region, instanceID string) (*core.Instance, error) {
	channel := fmt.Sprintf("vps:%d", vpsID)
	deadline := time.Now().Add(5 * time.Minute)
	started := time.Now()

	for time.Now().Before(deadline) {
		instance, err := s.computeService.GetInstance(ctx, region, instanceID)
		if err != nil {
			return nil, err
		}

		state := instance.LifecycleState
		elapsed := time.Since(started).Round(time.Second)

		switch state {
		case core.InstanceLifecycleStateRunning:
			return instance, nil
		case core.InstanceLifecycleStateTerminated, core.InstanceLifecycleStateTerminating:
			return nil, fmt.Errorf("instance %s entered state %s", instanceID, state)
		default:
			s.broker.Publish(channel, sse.SSEEvent{
				Type:      "status",
				Status:    "provisioning",
				Step:      "waiting_for_boot",
				Message:   fmt.Sprintf("Instance state: %s (%s elapsed)", state, elapsed),
				Timestamp: time.Now().UnixMilli(),
			})
			time.Sleep(3 * time.Second)
		}
	}

	return nil, fmt.Errorf("instance %s did not reach running state within 5 minutes", instanceID)
}

func (s *VPSProvisionService) vpsRegion(ctx context.Context, vpsID int64) (string, error) {
	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		return "", fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		return "", fmt.Errorf("vps %d not found", vpsID)
	}
	if !vps.NetworkID.Valid {
		return "", fmt.Errorf("vps has no network assigned")
	}
	network, err := s.networkRepo.Get(ctx, vps.NetworkID.Int64)
	if err != nil {
		return "", fmt.Errorf("get network: %w", err)
	}
	if network == nil {
		return "", fmt.Errorf("network not found")
	}
	if network.Region == "" {
		return "", fmt.Errorf("network has no region configured")
	}
	return network.Region, nil
}

func (s *VPSProvisionService) VPSRegionForDelete(ctx context.Context, vpsID int64) (string, error) {
	return s.vpsRegion(ctx, vpsID)
}

func (s *VPSProvisionService) TerminateInstance(ctx context.Context, vpsID int64, region, instanceID string) (err error) {
	defer func() {
		status := "success"
		errMsg := ""
		if err != nil {
			status = "failure"
			errMsg = sanitize(err.Error())
		}
		s.audit.Log(ctx, model.AuditLog{Operation: "vps.terminate", ResourceType: "vps", ResourceID: vpsID, Status: status, ErrorMessage: errMsg})
	}()

	if err := s.computeService.TerminateInstance(ctx, region, instanceID); err != nil {
		_ = s.vpsRepo.UpdateStatus(ctx, vpsID, "running")
		channel := fmt.Sprintf("vps:%d", vpsID)
		s.broker.Publish(channel, sse.SSEEvent{
			Type:    "error",
			Status:  "terminate_failed",
			Message: "Failed to terminate instance: " + err.Error(),
		})
		return err
	}

	vps, verr := s.vpsRepo.Get(ctx, vpsID)
	if verr == nil && vps.NSGID != "" {
		if derr := s.computeService.DeleteNSG(ctx, region, vps.NSGID); derr != nil {
			s.log.Warn("terminate_instance_delete_nsg_failed", "vps_id", vpsID, "nsg_id", vps.NSGID, "error", derr)
		} else {
			_ = s.vpsResourceRepo.MarkDeleted(ctx, vps.NSGID)
			_ = s.vpsRepo.UpdateNSGID(ctx, vpsID, "")
		}
	}

	if err := s.vpsRepo.UpdateStatus(ctx, vpsID, "terminated"); err != nil {
		s.log.Warn("terminate_instance_update_status_failed", "vps_id", vpsID, "error", err)
	}
	if err := s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSFailed)); err != nil {
		s.log.Warn("terminate_instance_update_provisioning_state_failed", "vps_id", vpsID, "error", err)
	}

	channel := fmt.Sprintf("vps:%d", vpsID)
	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "status",
		Status:  "terminated",
		Step:    "terminated",
		Message: "VPS instance terminated",
	})

	return nil
}

func (s *VPSProvisionService) RollbackVPS(ctx context.Context, vpsID int64, region, instanceID string) {
	s.audit.Log(ctx, model.AuditLog{Operation: "vps.rollback", ResourceType: "vps", ResourceID: vpsID, Status: "started"})

	s.log.Warn("rollback_vps_starting", "vps_id", vpsID, "instance_id", instanceID)
	channel := fmt.Sprintf("vps:%d", vpsID)
	if err := s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSRollingBack)); err != nil {
		s.log.Warn("rollback_vps_update_state_failed", "vps_id", vpsID, "error", err)
	}
	if instanceID != "" {
		if terr := s.TerminateInstance(ctx, vpsID, region, instanceID); terr != nil {
			s.log.Error("rollback_vps_terminate_failed", "vps_id", vpsID, "error", terr)
		}
		if merr := s.vpsResourceRepo.MarkDeleted(ctx, instanceID); merr != nil {
			s.log.Warn("rollback_vps_mark_deleted_failed", "vps_id", vpsID, "error", merr)
		}
	}
	{
		vps, verr := s.vpsRepo.Get(ctx, vpsID)
		if verr == nil && vps.NSGID != "" {
			if derr := s.computeService.DeleteNSG(ctx, region, vps.NSGID); derr != nil {
				s.log.Error("rollback_vps_delete_nsg_failed", "vps_id", vpsID, "nsg_id", vps.NSGID, "error", derr)
			} else {
				if merr := s.vpsResourceRepo.MarkDeleted(ctx, vps.NSGID); merr != nil {
					s.log.Warn("rollback_vps_mark_nsg_deleted_failed", "vps_id", vpsID, "error", merr)
				}
				_ = s.vpsRepo.UpdateNSGID(ctx, vpsID, "")
			}
		}
	}
	if err := s.vpsRepo.UpdateStatus(ctx, vpsID, "failed"); err != nil {
		s.log.Error("rollback_vps_update_status_failed", "vps_id", vpsID, "error", err)
	}
	if err := s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSFailed)); err != nil {
		s.log.Error("rollback_vps_update_prov_state_failed", "vps_id", vpsID, "error", err)
	}
	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "error",
		Status:  "failed",
		Step:    string(StateVPSFailed),
		Message: "VPS provisioning failed and was rolled back",
	})
}

func (s *VPSProvisionService) StartInstance(ctx context.Context, vpsID int64) (err error) {
	defer func() {
		status := "success"
		errMsg := ""
		if err != nil {
			status = "failure"
			errMsg = sanitize(err.Error())
		}
		s.audit.Log(ctx, model.AuditLog{Operation: "vps.start", ResourceType: "vps", ResourceID: vpsID, Status: status, ErrorMessage: errMsg})
	}()

	s.log.Debug("start_instance_begin", "vps_id", vpsID)

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		s.log.Debug("start_instance_get_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		s.log.Debug("start_instance_not_found", "vps_id", vpsID)
		return fmt.Errorf("vps %d not found", vpsID)
	}

	if !vps.OCIInstanceID.Valid || vps.OCIInstanceID.String == "" {
		s.log.Debug("start_instance_no_instance_id", "vps_id", vpsID)
		return fmt.Errorf("vps has no OCI instance ID")
	}

	if vps.Status != "stopped" {
		s.log.Debug("start_instance_wrong_state", "vps_id", vpsID, "current_status", vps.Status)
		return fmt.Errorf("vps must be in stopped state to start, current: %s", vps.Status)
	}

	region, err := s.vpsRegion(ctx, vpsID)
	if err != nil {
		s.log.Debug("start_instance_get_region_failed", "vps_id", vpsID, "error", err)
		return err
	}

	s.log.Debug("start_instance_region", "vps_id", vpsID, "region", region, "instance_id", vps.OCIInstanceID.String)

	computeClient, err := s.computeService.GetComputeClient(ctx, region)
	if err != nil {
		s.log.Debug("start_instance_get_client_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("get compute client: %w", err)
	}

	action := core.InstanceActionActionStart
	_, err = computeClient.InstanceAction(ctx, core.InstanceActionRequest{
		InstanceId: common.String(vps.OCIInstanceID.String),
		Action:     action,
	})
	if err != nil {
		s.log.Debug("start_instance_action_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("instance action start: %w", err)
	}

	if err := s.vpsRepo.UpdateStatus(ctx, vpsID, "running"); err != nil {
		s.log.Debug("start_instance_update_status_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("update vps status: %w", err)
	}

	s.log.Debug("start_instance_success", "vps_id", vpsID)

	s.broker.Publish(fmt.Sprintf("vps:%d", vpsID), sse.SSEEvent{
		Type:    "status",
		Status:  "running",
		Step:    "started",
		Message: "VPS instance started",
	})

	return nil
}

func (s *VPSProvisionService) StopInstance(ctx context.Context, vpsID int64) (err error) {
	defer func() {
		status := "success"
		errMsg := ""
		if err != nil {
			status = "failure"
			errMsg = sanitize(err.Error())
		}
		s.audit.Log(ctx, model.AuditLog{Operation: "vps.stop", ResourceType: "vps", ResourceID: vpsID, Status: status, ErrorMessage: errMsg})
	}()

	s.log.Debug("stop_instance_begin", "vps_id", vpsID)

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		s.log.Debug("stop_instance_get_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		s.log.Debug("stop_instance_not_found", "vps_id", vpsID)
		return fmt.Errorf("vps %d not found", vpsID)
	}

	if !vps.OCIInstanceID.Valid || vps.OCIInstanceID.String == "" {
		s.log.Debug("stop_instance_no_instance_id", "vps_id", vpsID)
		return fmt.Errorf("vps has no OCI instance ID")
	}

	if vps.Status != "running" {
		s.log.Debug("stop_instance_wrong_state", "vps_id", vpsID, "current_status", vps.Status)
		return fmt.Errorf("vps must be in running state to stop, current: %s", vps.Status)
	}

	region, err := s.vpsRegion(ctx, vpsID)
	if err != nil {
		s.log.Debug("stop_instance_get_region_failed", "vps_id", vpsID, "error", err)
		return err
	}

	s.log.Debug("stop_instance_region", "vps_id", vpsID, "region", region, "instance_id", vps.OCIInstanceID.String)

	computeClient, err := s.computeService.GetComputeClient(ctx, region)
	if err != nil {
		s.log.Debug("stop_instance_get_client_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("get compute client: %w", err)
	}

	action := core.InstanceActionActionStop
	_, err = computeClient.InstanceAction(ctx, core.InstanceActionRequest{
		InstanceId: common.String(vps.OCIInstanceID.String),
		Action:     action,
	})
	if err != nil {
		s.log.Debug("stop_instance_action_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("instance action stop: %w", err)
	}

	if err := s.vpsRepo.UpdateStatus(ctx, vpsID, "stopped"); err != nil {
		s.log.Debug("stop_instance_update_status_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("update vps status: %w", err)
	}

	s.log.Debug("stop_instance_success", "vps_id", vpsID)

	s.broker.Publish(fmt.Sprintf("vps:%d", vpsID), sse.SSEEvent{
		Type:    "status",
		Status:  "stopped",
		Step:    "stopped",
		Message: "VPS instance stopped",
	})

	return nil
}

func (s *VPSProvisionService) RestartInstance(ctx context.Context, vpsID int64) (err error) {
	defer func() {
		status := "success"
		errMsg := ""
		if err != nil {
			status = "failure"
			errMsg = sanitize(err.Error())
		}
		s.audit.Log(ctx, model.AuditLog{Operation: "vps.restart", ResourceType: "vps", ResourceID: vpsID, Status: status, ErrorMessage: errMsg})
	}()

	s.log.Debug("restart_instance_begin", "vps_id", vpsID)

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		s.log.Debug("restart_instance_get_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		s.log.Debug("restart_instance_not_found", "vps_id", vpsID)
		return fmt.Errorf("vps %d not found", vpsID)
	}

	if !vps.OCIInstanceID.Valid || vps.OCIInstanceID.String == "" {
		s.log.Debug("restart_instance_no_instance_id", "vps_id", vpsID)
		return fmt.Errorf("vps has no OCI instance ID")
	}

	if vps.Status != "running" {
		s.log.Debug("restart_instance_wrong_state", "vps_id", vpsID, "current_status", vps.Status)
		return fmt.Errorf("vps must be in running state to restart, current: %s", vps.Status)
	}

	region, err := s.vpsRegion(ctx, vpsID)
	if err != nil {
		s.log.Debug("restart_instance_get_region_failed", "vps_id", vpsID, "error", err)
		return err
	}

	s.log.Debug("restart_instance_region", "vps_id", vpsID, "region", region, "instance_id", vps.OCIInstanceID.String)

	instanceID := vps.OCIInstanceID.String

	if err := s.vpsRepo.UpdateStatus(ctx, vpsID, "restarting"); err != nil {
		s.log.Debug("restart_instance_update_status_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("update status: %w", err)
	}
	if err := s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSRestarting)); err != nil {
		s.log.Warn("restart_instance_update_prov_state_failed", "vps_id", vpsID, "error", err)
	}

	channel := fmt.Sprintf("vps:%d", vpsID)
	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "status",
		Status:  "restarting",
		Step:    "restart_initiated",
		Message: "Restart initiated — graceful OS reboot",
	})

	go s.doRestartInstance(vpsID, region, instanceID)

	s.log.Debug("restart_instance_dispatched", "vps_id", vpsID)
	return nil
}

func (s *VPSProvisionService) doRestartInstance(vpsID int64, region, instanceID string) {
	bgCtx := context.Background()
	channel := fmt.Sprintf("vps:%d", vpsID)

	s.log.Debug("do_restart_instance_begin", "vps_id", vpsID, "region", region, "instance_id", instanceID)

	defer func() {
		if r := recover(); r != nil {
			s.log.Error("restart_instance_panic", "vps_id", vpsID, "error", fmt.Errorf("%v", r))
			_ = s.vpsRepo.UpdateStatus(bgCtx, vpsID, "running")
			_ = s.vpsRepo.UpdateProvisioningState(bgCtx, vpsID, string(StateVPSReady))
			s.broker.Publish(channel, sse.SSEEvent{
				Type:    "error",
				Status:  "running",
				Step:    "restart_failed",
				Message: "Restart failed due to an internal error",
			})
		}
	}()

	computeClient, err := s.computeService.GetComputeClient(bgCtx, region)
	if err != nil {
		s.doRestartRollback(bgCtx, vpsID, channel, fmt.Sprintf("Failed to connect to cloud provider: %v", err))
		return
	}

	_, err = computeClient.InstanceAction(bgCtx, core.InstanceActionRequest{
		InstanceId: common.String(instanceID),
		Action:     core.InstanceActionActionSoftreset,
	})
	if err != nil {
		s.doRestartRollback(bgCtx, vpsID, channel, fmt.Sprintf("Cloud provider rejected restart: %v", err))
		return
	}

	s.log.Debug("restart_instance_oci_action_sent", "vps_id", vpsID, "instance_id", instanceID)

	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "status",
		Status:  "restarting",
		Step:    "restart_rebooting",
		Message: "Instance is rebooting — waiting for it to come back online",
	})

	s.log.Debug("restart_instance_polling_begin", "vps_id", vpsID)
	if err := s.waitForRestartRunning(bgCtx, vpsID, region, instanceID); err != nil {
		s.doRestartRollback(bgCtx, vpsID, channel, fmt.Sprintf("Instance did not recover: %v", err))
		return
	}
	s.log.Debug("restart_instance_polling_complete", "vps_id", vpsID)

	_ = s.vpsRepo.UpdateStatus(bgCtx, vpsID, "running")
	_ = s.vpsRepo.UpdateProvisioningState(bgCtx, vpsID, string(StateVPSReady))

	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "status",
		Status:  "running",
		Step:    "restart_complete",
		Message: "Instance restart complete — back online",
	})

	s.audit.Log(bgCtx, model.AuditLog{
		Operation:    "vps.restart.async",
		ResourceType: "vps",
		ResourceID:   vpsID,
		Status:       "success",
	})
	s.log.Debug("restart_instance_complete", "vps_id", vpsID)
}

func (s *VPSProvisionService) waitForRestartRunning(ctx context.Context, vpsID int64, region, instanceID string) error {
	channel := fmt.Sprintf("vps:%d", vpsID)
	deadline := time.Now().Add(5 * time.Minute)
	started := time.Now()

	for time.Now().Before(deadline) {
		instance, err := s.computeService.GetInstance(ctx, region, instanceID)
		if err != nil {
			s.log.Debug("restart_instance_get_failed_retrying", "vps_id", vpsID, "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		state := instance.LifecycleState
		elapsed := time.Since(started).Round(time.Second)

		switch state {
		case core.InstanceLifecycleStateRunning:
			return nil
		case core.InstanceLifecycleStateTerminated, core.InstanceLifecycleStateTerminating:
			return fmt.Errorf("instance entered %s during restart", state)
		default:
			s.broker.Publish(channel, sse.SSEEvent{
				Type:      "status",
				Status:    "restarting",
				Step:      "restart_rebooting",
				Message:   fmt.Sprintf("Instance state: %s (%s elapsed)", state, elapsed),
				Timestamp: time.Now().UnixMilli(),
			})
			time.Sleep(5 * time.Second)
		}
	}

	return fmt.Errorf("instance did not reach running state within 5 minutes")
}

func (s *VPSProvisionService) doRestartRollback(ctx context.Context, vpsID int64, channel, errMsg string) {
	s.log.Warn("restart_instance_rollback", "vps_id", vpsID, "error", errMsg)
	_ = s.vpsRepo.UpdateStatus(ctx, vpsID, "running")
	_ = s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSReady))
	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "error",
		Status:  "running",
		Step:    "restart_failed",
		Message: "Restart failed: " + errMsg,
	})
	s.audit.Log(ctx, model.AuditLog{
		Operation:    "vps.restart.async",
		ResourceType: "vps",
		ResourceID:   vpsID,
		Status:       "failure",
		ErrorMessage: sanitize(errMsg),
	})
}

func (s *VPSProvisionService) ResetInstance(ctx context.Context, vpsID int64) (err error) {
	defer func() {
		status := "success"
		errMsg := ""
		if err != nil {
			status = "failure"
			errMsg = sanitize(err.Error())
		}
		s.audit.Log(ctx, model.AuditLog{Operation: "vps.reset", ResourceType: "vps", ResourceID: vpsID, Status: status, ErrorMessage: errMsg})
	}()

	s.log.Debug("reset_instance_begin", "vps_id", vpsID)

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		s.log.Debug("reset_instance_get_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		s.log.Debug("reset_instance_not_found", "vps_id", vpsID)
		return fmt.Errorf("vps %d not found", vpsID)
	}

	if !vps.OCIInstanceID.Valid || vps.OCIInstanceID.String == "" {
		s.log.Debug("reset_instance_no_instance_id", "vps_id", vpsID)
		return fmt.Errorf("vps has no OCI instance ID")
	}

	if vps.Status != "running" && vps.Status != "stopped" {
		s.log.Debug("reset_instance_wrong_state", "vps_id", vpsID, "current_status", vps.Status)
		return fmt.Errorf("vps must be in running or stopped state to reset, current: %s", vps.Status)
	}

	region, err := s.vpsRegion(ctx, vpsID)
	if err != nil {
		s.log.Debug("reset_instance_get_region_failed", "vps_id", vpsID, "error", err)
		return err
	}

	s.log.Debug("reset_instance_region", "vps_id", vpsID, "region", region, "instance_id", vps.OCIInstanceID.String)

	previousStatus := vps.Status
	instanceID := vps.OCIInstanceID.String

	if err := s.vpsRepo.UpdateStatus(ctx, vpsID, "resetting"); err != nil {
		s.log.Debug("reset_instance_update_status_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("update status: %w", err)
	}
	if err := s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSResetting)); err != nil {
		s.log.Warn("reset_instance_update_prov_state_failed", "vps_id", vpsID, "error", err)
	}

	channel := fmt.Sprintf("vps:%d", vpsID)
	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "status",
		Status:  "resetting",
		Step:    "reset_initiated",
		Message: "Reset initiated — forcing instance power cycle",
	})

	go s.doResetInstance(vpsID, region, instanceID, previousStatus)

	s.log.Debug("reset_instance_dispatched", "vps_id", vpsID)
	return nil
}

// doResetInstance performs the actual OCI reset + polling. It runs in a
// background goroutine so the HTTP handler returns immediately.
func (s *VPSProvisionService) doResetInstance(vpsID int64, region, instanceID, previousStatus string) {
	bgCtx := context.Background()
	channel := fmt.Sprintf("vps:%d", vpsID)

	s.log.Debug("do_reset_instance_begin", "vps_id", vpsID, "region", region, "instance_id", instanceID)

	defer func() {
		if r := recover(); r != nil {
			s.log.Error("reset_instance_panic", "vps_id", vpsID, "error", fmt.Errorf("%v", r))
			_ = s.vpsRepo.UpdateStatus(bgCtx, vpsID, previousStatus)
			_ = s.vpsRepo.UpdateProvisioningState(bgCtx, vpsID, string(StateVPSReady))
			s.broker.Publish(channel, sse.SSEEvent{
				Type:    "error",
				Status:  previousStatus,
				Step:    "reset_failed",
				Message: "Reset failed due to an internal error",
			})
		}
	}()

	computeClient, err := s.computeService.GetComputeClient(bgCtx, region)
	if err != nil {
		s.doResetRollback(bgCtx, vpsID, previousStatus, channel, fmt.Sprintf("Failed to connect to cloud provider: %v", err))
		return
	}

	_, err = computeClient.InstanceAction(bgCtx, core.InstanceActionRequest{
		InstanceId: common.String(instanceID),
		Action:     core.InstanceActionActionReset,
	})
	if err != nil {
		s.doResetRollback(bgCtx, vpsID, previousStatus, channel, fmt.Sprintf("Cloud provider rejected reset: %v", err))
		return
	}

	s.log.Debug("reset_instance_oci_action_sent", "vps_id", vpsID, "instance_id", instanceID)

	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "status",
		Status:  "resetting",
		Step:    "reset_power_cycle",
		Message: "Instance is power cycling — waiting for it to come back online",
	})

	s.log.Debug("reset_instance_polling_begin", "vps_id", vpsID)
	if err := s.waitForResetRunning(bgCtx, vpsID, region, instanceID); err != nil {
		s.doResetRollback(bgCtx, vpsID, previousStatus, channel, fmt.Sprintf("Instance did not recover: %v", err))
		return
	}
	s.log.Debug("reset_instance_polling_complete", "vps_id", vpsID)

	_ = s.vpsRepo.UpdateStatus(bgCtx, vpsID, "running")
	_ = s.vpsRepo.UpdateProvisioningState(bgCtx, vpsID, string(StateVPSReady))

	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "status",
		Status:  "running",
		Step:    "reset_complete",
		Message: "Instance reset complete — back online",
	})

	s.audit.Log(bgCtx, model.AuditLog{
		Operation:    "vps.reset.async",
		ResourceType: "vps",
		ResourceID:   vpsID,
		Status:       "success",
	})
	s.log.Debug("reset_instance_complete", "vps_id", vpsID)
}

// waitForResetRunning polls GetInstance until LifecycleState == RUNNING.
// A hard reset power-cycles the instance, so it may transition through
// STOPPING → STOPPED → STARTING → RUNNING. We wait up to 10 minutes.
func (s *VPSProvisionService) waitForResetRunning(ctx context.Context, vpsID int64, region, instanceID string) error {
	channel := fmt.Sprintf("vps:%d", vpsID)
	deadline := time.Now().Add(10 * time.Minute)
	started := time.Now()

	for time.Now().Before(deadline) {
		instance, err := s.computeService.GetInstance(ctx, region, instanceID)
		if err != nil {
			s.log.Debug("reset_instance_get_failed_retrying", "vps_id", vpsID, "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		state := instance.LifecycleState
		elapsed := time.Since(started).Round(time.Second)

		switch state {
		case core.InstanceLifecycleStateRunning:
			return nil
		case core.InstanceLifecycleStateTerminated, core.InstanceLifecycleStateTerminating:
			return fmt.Errorf("instance entered %s during reset", state)
		default:
			s.broker.Publish(channel, sse.SSEEvent{
				Type:      "status",
				Status:    "resetting",
				Step:      "reset_power_cycle",
				Message:   fmt.Sprintf("Instance state: %s (%s elapsed)", state, elapsed),
				Timestamp: time.Now().UnixMilli(),
			})
			time.Sleep(5 * time.Second)
		}
	}

	return fmt.Errorf("instance did not reach running state within 10 minutes")
}

// doResetRollback reverts the VPS status on reset failure and emits an SSE error.
func (s *VPSProvisionService) doResetRollback(ctx context.Context, vpsID int64, previousStatus, channel, errMsg string) {
	s.log.Warn("reset_instance_rollback", "vps_id", vpsID, "error", errMsg)
	_ = s.vpsRepo.UpdateStatus(ctx, vpsID, previousStatus)
	_ = s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSReady))
	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "error",
		Status:  previousStatus,
		Step:    "reset_failed",
		Message: "Reset failed: " + errMsg,
	})
	s.audit.Log(ctx, model.AuditLog{
		Operation:    "vps.reset.async",
		ResourceType: "vps",
		ResourceID:   vpsID,
		Status:       "failure",
		ErrorMessage: sanitize(errMsg),
	})
}

func (s *VPSProvisionService) ResetPassword(ctx context.Context, vpsID int64, newPassword string) (err error) {
	defer func() {
		status := "success"
		errMsg := ""
		if err != nil {
			status = "failure"
			errMsg = sanitize(err.Error())
		}
		s.audit.Log(ctx, model.AuditLog{Operation: "vps.reset_password", ResourceType: "vps", ResourceID: vpsID, Status: status, ErrorMessage: errMsg})
	}()

	if err := ValidateResetPassword(newPassword); err != nil {
		return err
	}

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		return fmt.Errorf("vps %d not found", vpsID)
	}

	if !vps.OCIInstanceID.Valid || vps.OCIInstanceID.String == "" {
		return fmt.Errorf("vps has no OCI instance ID")
	}

	if vps.Status != "running" {
		return fmt.Errorf("vps must be running to reset password, current: %s", vps.Status)
	}
	if !vps.SSHUsername.Valid || vps.SSHUsername.String == "" {
		return fmt.Errorf("vps has no provisioned SSH username")
	}
	if vps.SSHUsername.String == "root" {
		return fmt.Errorf("reset password cannot target root")
	}
	if !vps.PublicIP.Valid || vps.PublicIP.String == "" {
		return fmt.Errorf("vps has no public IP")
	}

	if !vps.SSHPrivateKey.Valid || vps.SSHPrivateKey.String == "" {
		return fmt.Errorf("vps has no SSH private key")
	}

	s.log.Debug("reset_password_begin", "vps_id", vpsID, "ssh_user", vps.SSHUsername.String)

	if err := sshResetPasswordFn(ctx, s.vpsRepo, vpsID, vps.PublicIP.String, vps.SSHPrivateKey.String, vps.SSHUsername.String, newPassword); err != nil {
		return fmt.Errorf("ssh reset password: %w", err)
	}
	if err := sshVerifyPasswordLoginFn(ctx, s.vpsRepo, vpsID, vps.PublicIP.String, vps.SSHUsername.String, newPassword); err != nil {
		return fmt.Errorf("verify reset password login: %w", err)
	}
	if err := s.vpsRepo.UpdateSSHPassword(ctx, vpsID, newPassword); err != nil {
		return fmt.Errorf("update ssh password: %w", err)
	}

	s.broker.Publish(fmt.Sprintf("vps:%d", vpsID), sse.SSEEvent{
		Type:    "status",
		Status:  "running",
		Step:    "password_reset",
		Message: "Password reset command sent to VPS instance",
	})

	return nil
}

func (s *VPSProvisionService) RefreshInstanceIPs(ctx context.Context, vpsID int64) error {
	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		return fmt.Errorf("vps %d not found", vpsID)
	}
	if !vps.OCIInstanceID.Valid || vps.OCIInstanceID.String == "" {
		return fmt.Errorf("vps has no OCI instance ID")
	}

	region, err := s.vpsRegion(ctx, vpsID)
	if err != nil {
		return fmt.Errorf("get region: %w", err)
	}
	compartmentOCID, err := s.computeService.GetCompartmentOCID(ctx)
	if err != nil {
		return fmt.Errorf("get compartment: %w", err)
	}

	publicIP, privateIP, err := s.computeService.GetInstanceIPs(ctx, region, vps.OCIInstanceID.String, compartmentOCID)
	if err != nil {
		return fmt.Errorf("get instance IPs: %w", err)
	}

	updated := false
	if publicIP != "" && (!vps.PublicIP.Valid || vps.PublicIP.String != publicIP) {
		vps.PublicIP = model.NullString{NullString: sql.NullString{String: publicIP, Valid: true}}
		updated = true
	}
	if privateIP != "" && (!vps.PrivateIP.Valid || vps.PrivateIP.String != privateIP) {
		vps.PrivateIP = model.NullString{NullString: sql.NullString{String: privateIP, Valid: true}}
		updated = true
	}

	if updated {
		if err := s.vpsRepo.Update(ctx, vps); err != nil {
			return fmt.Errorf("update vps: %w", err)
		}
	}

	return nil
}

func buildChpasswdCommand(username, password string) string {
	return fmt.Sprintf("printf '%%s\\n' %s | chpasswd", shellSingleQuote(username+":"+password))
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

type FirewallRule struct {
	Port        int    `json:"port"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Direction   string `json:"direction"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
}

func (s *VPSProvisionService) GetFirewallRules(ctx context.Context, vpsID int64) ([]FirewallRule, error) {
	s.log.Debug("get_firewall_begin", "vps_id", vpsID)

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		s.log.Debug("get_firewall_get_vps_failed", "vps_id", vpsID, "error", err)
		return nil, fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		s.log.Debug("get_firewall_vps_not_found", "vps_id", vpsID)
		return nil, fmt.Errorf("vps %d not found", vpsID)
	}

	if vps.NSGID == "" {
		s.log.Debug("get_firewall_no_nsg", "vps_id", vpsID)
		return nil, fmt.Errorf("no NSG associated with VPS %d", vpsID)
	}

	if !vps.NetworkID.Valid {
		s.log.Debug("get_firewall_no_network", "vps_id", vpsID)
		return nil, fmt.Errorf("vps has no network assigned")
	}

	network, err := s.networkRepo.Get(ctx, vps.NetworkID.Int64)
	if err != nil {
		s.log.Debug("get_firewall_get_network_failed", "vps_id", vpsID, "network_id", vps.NetworkID.Int64, "error", err)
		return nil, fmt.Errorf("get network: %w", err)
	}
	if network == nil {
		s.log.Debug("get_firewall_network_not_found", "vps_id", vpsID, "network_id", vps.NetworkID.Int64)
		return nil, fmt.Errorf("network not found")
	}

	s.log.Debug("get_firewall_nsg_region", "vps_id", vpsID, "nsg_id", vps.NSGID, "region", network.Region)

	nsgRules, err := s.computeService.ListNSGRules(ctx, network.Region, vps.NSGID)
	if err != nil {
		s.log.Debug("get_firewall_list_rules_failed", "vps_id", vpsID, "error", err)
		return nil, fmt.Errorf("list NSG rules: %w", err)
	}

	s.log.Debug("get_firewall_rules_count", "vps_id", vpsID, "count", len(nsgRules))

	var rules []FirewallRule
	for _, r := range nsgRules {
		fr := FirewallRule{
			Direction: strings.ToLower(string(r.Direction)),
		}
		if r.Source != nil {
			fr.Source = *r.Source
		}
		if r.Destination != nil {
			fr.Destination = *r.Destination
		}
		if r.Description != nil {
			fr.Name = *r.Description
			fr.Description = *r.Description
		}
		if r.TcpOptions != nil && r.TcpOptions.DestinationPortRange != nil {
			if r.TcpOptions.DestinationPortRange.Min != nil {
				fr.Port = *r.TcpOptions.DestinationPortRange.Min
			}
		} else if r.UdpOptions != nil && r.UdpOptions.DestinationPortRange != nil {
			if r.UdpOptions.DestinationPortRange.Min != nil {
				fr.Port = *r.UdpOptions.DestinationPortRange.Min
			}
		}
		rules = append(rules, fr)
	}

	if rules == nil {
		rules = []FirewallRule{}
	}
	return rules, nil
}

func (s *VPSProvisionService) UpdateFirewallRules(ctx context.Context, vpsID int64, rules []FirewallRule) error {
	s.log.Debug("update_firewall_begin", "vps_id", vpsID, "rules_count", len(rules))

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		s.log.Debug("update_firewall_get_vps_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		s.log.Debug("update_firewall_vps_not_found", "vps_id", vpsID)
		return fmt.Errorf("vps %d not found", vpsID)
	}

	if vps.NSGID == "" {
		s.log.Debug("update_firewall_no_nsg", "vps_id", vpsID)
		return fmt.Errorf("no NSG associated with VPS %d", vpsID)
	}

	if !vps.NetworkID.Valid {
		s.log.Debug("update_firewall_no_network", "vps_id", vpsID)
		return fmt.Errorf("vps has no network assigned")
	}

	network, err := s.networkRepo.Get(ctx, vps.NetworkID.Int64)
	if err != nil {
		s.log.Debug("update_firewall_get_network_failed", "vps_id", vpsID, "network_id", vps.NetworkID.Int64, "error", err)
		return fmt.Errorf("get network: %w", err)
	}
	if network == nil {
		s.log.Debug("update_firewall_network_not_found", "vps_id", vpsID, "network_id", vps.NetworkID.Int64)
		return fmt.Errorf("network not found")
	}

	s.log.Debug("update_firewall_nsg_region", "vps_id", vpsID, "nsg_id", vps.NSGID, "region", network.Region)

	// 1. List current NSG rules to get their IDs for removal
	currentRules, err := s.computeService.ListNSGRules(ctx, network.Region, vps.NSGID)
	if err != nil {
		s.log.Debug("update_firewall_list_rules_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("list current NSG rules: %w", err)
	}

	// 2. Remove all current rules by ID
	currentIDs := make([]string, 0, len(currentRules))
	for _, r := range currentRules {
		if r.Id != nil {
			currentIDs = append(currentIDs, *r.Id)
		}
	}
	if err := s.computeService.RemoveNSGRules(ctx, network.Region, vps.NSGID, currentIDs); err != nil {
		s.log.Debug("update_firewall_remove_rules_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("remove current NSG rules: %w", err)
	}

	// 3. Convert FirewallRules → AddSecurityRuleDetails and add them
	if len(rules) == 0 {
		s.log.Debug("update_firewall_cleared", "vps_id", vpsID)
		return nil
	}

	details := make([]core.AddSecurityRuleDetails, 0, len(rules))
	for _, fr := range rules {
		d := core.AddSecurityRuleDetails{
			Protocol: common.String("6"),
		}

		switch strings.ToLower(fr.Direction) {
		case "ingress":
			d.Direction = core.AddSecurityRuleDetailsDirectionIngress
			if fr.Source != "" {
				d.Source = common.String(fr.Source)
				d.SourceType = core.AddSecurityRuleDetailsSourceTypeCidrBlock
			} else {
				d.Source = common.String("0.0.0.0/0")
				d.SourceType = core.AddSecurityRuleDetailsSourceTypeCidrBlock
			}
			if fr.Port > 0 {
				d.TcpOptions = &core.TcpOptions{
					DestinationPortRange: &core.PortRange{
						Min: common.Int(fr.Port), Max: common.Int(fr.Port),
					},
				}
			}
		case "egress":
			d.Direction = core.AddSecurityRuleDetailsDirectionEgress
			if fr.Destination != "" {
				d.Destination = common.String(fr.Destination)
				d.DestinationType = core.AddSecurityRuleDetailsDestinationTypeCidrBlock
			} else {
				d.Destination = common.String("0.0.0.0/0")
				d.DestinationType = core.AddSecurityRuleDetailsDestinationTypeCidrBlock
			}
			if fr.Port > 0 {
				d.TcpOptions = &core.TcpOptions{
					DestinationPortRange: &core.PortRange{
						Min: common.Int(fr.Port), Max: common.Int(fr.Port),
					},
				}
			}
		default:
			continue
		}

		if fr.Description != "" {
			d.Description = common.String(fr.Description)
		}
		details = append(details, d)
	}

	if err := s.computeService.AddNSGRules(ctx, network.Region, vps.NSGID, details); err != nil {
		s.log.Debug("update_firewall_add_rules_failed", "vps_id", vpsID, "error", err)
		return fmt.Errorf("add new NSG rules: %w", err)
	}

	s.log.Debug("update_firewall_success", "vps_id", vpsID)
	return nil
}

func sanitizeUsername(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name) && len(result) < 32; i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			result = append(result, c)
		} else if c >= 'A' && c <= 'Z' {
			result = append(result, c+32)
		} else if c == ' ' || c == '-' || c == '_' {
			result = append(result, '_')
		}
	}
	if len(result) == 0 {
		return "vpsuser"
	}
	if result[0] >= '0' && result[0] <= '9' {
		result = append([]byte{'u'}, result...)
	}
	return string(result)
}

const passwordChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generatePassword(length int) string {
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(passwordChars))))
		b[i] = passwordChars[n.Int64()]
	}
	return string(b)
}

func sanitize(errMsg string) string {
	if len(errMsg) > 256 {
		return errMsg[:256]
	}
	return errMsg
}
