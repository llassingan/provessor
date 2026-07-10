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
	}
}

func (s *VPSProvisionService) ProvisionVPS(ctx context.Context, vpsID int64) error {
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
	log.Printf("[DEBUG] provision_vps: vps %d generating SSH key pair", vpsID)

	publicKey, privateKeyPEM, err := GenerateSSHKeyPair()
	if err != nil {
		s.vpsRepo.UpdateStatus(ctx, vpsID, "failed")
		emit("error", "failed", "Failed to generate SSH keys: "+err.Error())
		return fmt.Errorf("generate SSH key pair: %w", err)
	}

	log.Printf("[DEBUG] provision_vps: vps %d SSH key pair generated", vpsID)

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

	log.Printf("[DEBUG] provision_vps: vps %d cloud-init prepared (len=%d)", vpsID, len(cloudInitYAML))

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
		log.Printf("[WARN] provision_vps: vps %d track NSG failed: %v", vpsID, terr)
	}
	if uerr := s.vpsRepo.UpdateNSGID(ctx, vpsID, nsgID); uerr != nil {
		log.Printf("[WARN] provision_vps: vps %d save NSG ID failed: %v", vpsID, uerr)
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
		log.Printf("[WARN] provision_vps: vps %d track instance failed: %v", vpsID, terr)
	}
	if uerr := s.vpsResourceRepo.UpdateStatus(ctx, instanceID, "created"); uerr != nil {
		log.Printf("[WARN] provision_vps: vps %d update resource status failed: %v", vpsID, uerr)
	}

	vps.OCIInstanceID = model.NullString{NullString: sql.NullString{String: instanceID, Valid: true}}
	vps.Status = "provisioning"
	if err := s.vpsRepo.Update(ctx, vps); err != nil {
		emit("error", "failed", "Failed to update VPS: "+err.Error())
		return fmt.Errorf("update vps: %w", err)
	}

	emit("waiting_for_boot", "provisioning", "Waiting for instance to boot")

	log.Printf("[DEBUG] provision_vps: vps %d waiting for instance %s to reach RUNNING state", vpsID, instanceID)

	_, err = s.waitForRunning(ctx, vpsID, network.Region, instanceID)
	if err != nil {
		emit("error", "failed", "Instance failed to start: "+err.Error())
		s.RollbackVPS(ctx, vpsID, network.Region, instanceID)
		return fmt.Errorf("wait for running: %w", err)
	}

	log.Printf("[DEBUG] provision_vps: vps %d instance %s is now RUNNING", vpsID, instanceID)

	vps.OCIInstanceID = model.NullString{NullString: sql.NullString{String: instanceID, Valid: true}}
	vps.Status = "running"

	emit("fetching_ips", "provisioning", "Retrieving instance IP addresses")

	compartmentOCID, err = s.computeService.GetCompartmentOCID(ctx)
	if err != nil {
		log.Printf("[ERROR] provision_vps: vps %d get compartment OCID failed: %v", vpsID, err)
		emit("error", "failed", "Failed to get compartment: "+err.Error())
		s.RollbackVPS(ctx, vpsID, network.Region, instanceID)
		return fmt.Errorf("get compartment: %w", err)
	}

	publicIP, privateIP, ipErr := s.computeService.GetInstanceIPs(ctx, network.Region, instanceID, compartmentOCID)
	if ipErr != nil {
		log.Printf("[ERROR] provision_vps: vps %d get instance IPs failed: %v", vpsID, ipErr)
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
	log.Printf("[DEBUG] provision_vps: vps %d IPs retrieved public_ip=%s private_ip=%s", vpsID, publicIP, privateIP)

	if privateKeyPEM != "" {
		emit("setting_up_ssh", "provisioning", "Creating SSH user")
		sshUser := sanitizeUsername(vps.DisplayName)
		sshPass := generatePassword(16)
		log.Printf("[DEBUG] provision_vps: vps %d creating SSH user %q on %s", vpsID, sshUser, publicIP)

		var sshErr error
		for attempt := 1; attempt <= 20; attempt++ {
			sshErr = sshCreateUserFn(ctx, s.vpsRepo, vpsID, publicIP, privateKeyPEM, sshUser, sshPass)
			if sshErr == nil {
				emit("ssh_connected", "provisioning", "SSH user created successfully")
				break
			}
			log.Printf("[DEBUG] provision_vps: vps %d SSH attempt %d/20 failed: %v", vpsID, attempt, sshErr)
			if attempt < 20 {
				emit("ssh_retry", "provisioning", fmt.Sprintf("Waiting for SSH daemon (attempt %d/20)", attempt))
				time.Sleep(10 * time.Second)
			} else {
				emit("ssh_failed", "provisioning", "SSH user creation failed after 20 attempts")
			}
		}

		if sshErr != nil {
			log.Printf("[ERROR] provision_vps: vps %d SSH user creation failed after 20 attempts: %v", vpsID, sshErr)
			emit("error", "failed", "SSH user creation failed after 20 attempts")
			s.RollbackVPS(ctx, vpsID, network.Region, instanceID)
			return fmt.Errorf("ssh create user: %w", sshErr)
		}

		vps.SSHPrivateKey = model.NullString{NullString: sql.NullString{String: privateKeyPEM, Valid: true}}
		vps.SSHUsername = model.NullString{NullString: sql.NullString{String: sshUser, Valid: true}}
		vps.SSHPassword = model.NullString{NullString: sql.NullString{String: sshPass, Valid: true}}
		log.Printf("[DEBUG] provision_vps: vps %d SSH credentials saved user=%s", vpsID, sshUser)

		if verifyErr := sshVerifyPasswordLoginFn(ctx, s.vpsRepo, vpsID, publicIP, sshUser, sshPass); verifyErr != nil {
			log.Printf("[DEBUG] provision_vps: vps %d password login VERIFICATION FAILED: %v", vpsID, verifyErr)
		} else {
			log.Printf("[DEBUG] provision_vps: vps %d password login verified OK for user=%s", vpsID, sshUser)
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

	log.Printf("[DEBUG] provision_vps: vps %d provisioning complete, publishing ready event", vpsID)
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

func (s *VPSProvisionService) TerminateInstance(ctx context.Context, vpsID int64, region, instanceID string) error {
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
			log.Printf("[WARN] terminate_instance: vps %d delete NSG %s failed: %v", vpsID, vps.NSGID, derr)
		} else {
			_ = s.vpsResourceRepo.MarkDeleted(ctx, vps.NSGID)
			_ = s.vpsRepo.UpdateNSGID(ctx, vpsID, "")
		}
	}

	if err := s.vpsRepo.UpdateStatus(ctx, vpsID, "terminated"); err != nil {
		log.Printf("[WARN] terminate_instance: vps %d update status to terminated failed: %v", vpsID, err)
	}
	if err := s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSFailed)); err != nil {
		log.Printf("[WARN] terminate_instance: vps %d update provisioning state failed: %v", vpsID, err)
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
	log.Printf("[WARN] rollback_vps: vps %d starting rollback instance=%s", vpsID, instanceID)
	channel := fmt.Sprintf("vps:%d", vpsID)
	if err := s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSRollingBack)); err != nil {
		log.Printf("[WARN] rollback_vps: vps %d update provisioning state failed: %v", vpsID, err)
	}
	if instanceID != "" {
		if terr := s.TerminateInstance(ctx, vpsID, region, instanceID); terr != nil {
			log.Printf("[ERROR] rollback_vps: vps %d terminate instance failed: %v", vpsID, terr)
		}
		if merr := s.vpsResourceRepo.MarkDeleted(ctx, instanceID); merr != nil {
			log.Printf("[WARN] rollback_vps: vps %d mark resource deleted failed: %v", vpsID, merr)
		}
	}
	{
		vps, verr := s.vpsRepo.Get(ctx, vpsID)
		if verr == nil && vps.NSGID != "" {
			if derr := s.computeService.DeleteNSG(ctx, region, vps.NSGID); derr != nil {
				log.Printf("[ERROR] rollback_vps: vps %d delete NSG %s failed: %v", vpsID, vps.NSGID, derr)
			} else {
				if merr := s.vpsResourceRepo.MarkDeleted(ctx, vps.NSGID); merr != nil {
					log.Printf("[WARN] rollback_vps: vps %d mark NSG deleted failed: %v", vpsID, merr)
				}
				_ = s.vpsRepo.UpdateNSGID(ctx, vpsID, "")
			}
		}
	}
	if err := s.vpsRepo.UpdateStatus(ctx, vpsID, "failed"); err != nil {
		log.Printf("[ERROR] rollback_vps: vps %d update status failed: %v", vpsID, err)
	}
	if err := s.vpsRepo.UpdateProvisioningState(ctx, vpsID, string(StateVPSFailed)); err != nil {
		log.Printf("[ERROR] rollback_vps: vps %d update provisioning state failed: %v", vpsID, err)
	}
	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "error",
		Status:  "failed",
		Step:    string(StateVPSFailed),
		Message: "VPS provisioning failed and was rolled back",
	})
}

func (s *VPSProvisionService) StartInstance(ctx context.Context, vpsID int64) error {
	log.Printf("[DEBUG] service_start_instance: vps_id=%d", vpsID)

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		log.Printf("[DEBUG] service_start_instance: vps %d get failed: %v", vpsID, err)
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		log.Printf("[DEBUG] service_start_instance: vps %d not found", vpsID)
		return fmt.Errorf("vps %d not found", vpsID)
	}

	if !vps.OCIInstanceID.Valid || vps.OCIInstanceID.String == "" {
		log.Printf("[DEBUG] service_start_instance: vps %d has no OCI instance ID", vpsID)
		return fmt.Errorf("vps has no OCI instance ID")
	}

	if vps.Status != "stopped" {
		log.Printf("[DEBUG] service_start_instance: vps %d not in stopped state (current=%s)", vpsID, vps.Status)
		return fmt.Errorf("vps must be in stopped state to start, current: %s", vps.Status)
	}

	region, err := s.vpsRegion(ctx, vpsID)
	if err != nil {
		log.Printf("[DEBUG] service_start_instance: vps %d get region failed: %v", vpsID, err)
		return err
	}

	log.Printf("[DEBUG] service_start_instance: vps %d region=%s instance_id=%s", vpsID, region, vps.OCIInstanceID.String)

	computeClient, err := s.computeService.GetComputeClient(ctx, region)
	if err != nil {
		log.Printf("[DEBUG] service_start_instance: vps %d get compute client failed: %v", vpsID, err)
		return fmt.Errorf("get compute client: %w", err)
	}

	action := core.InstanceActionActionStart
	_, err = computeClient.InstanceAction(ctx, core.InstanceActionRequest{
		InstanceId: common.String(vps.OCIInstanceID.String),
		Action:     action,
	})
	if err != nil {
		log.Printf("[DEBUG] service_start_instance: vps %d instance action START failed: %v", vpsID, err)
		return fmt.Errorf("instance action start: %w", err)
	}

	if err := s.vpsRepo.UpdateStatus(ctx, vpsID, "running"); err != nil {
		log.Printf("[DEBUG] service_start_instance: vps %d update status failed: %v", vpsID, err)
		return fmt.Errorf("update vps status: %w", err)
	}

	log.Printf("[DEBUG] service_start_instance: vps %d started successfully", vpsID)

	s.broker.Publish(fmt.Sprintf("vps:%d", vpsID), sse.SSEEvent{
		Type:    "status",
		Status:  "running",
		Step:    "started",
		Message: "VPS instance started",
	})

	return nil
}

func (s *VPSProvisionService) StopInstance(ctx context.Context, vpsID int64) error {
	log.Printf("[DEBUG] service_stop_instance: vps_id=%d", vpsID)

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		log.Printf("[DEBUG] service_stop_instance: vps %d get failed: %v", vpsID, err)
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		log.Printf("[DEBUG] service_stop_instance: vps %d not found", vpsID)
		return fmt.Errorf("vps %d not found", vpsID)
	}

	if !vps.OCIInstanceID.Valid || vps.OCIInstanceID.String == "" {
		log.Printf("[DEBUG] service_stop_instance: vps %d has no OCI instance ID", vpsID)
		return fmt.Errorf("vps has no OCI instance ID")
	}

	if vps.Status != "running" {
		log.Printf("[DEBUG] service_stop_instance: vps %d not in running state (current=%s)", vpsID, vps.Status)
		return fmt.Errorf("vps must be in running state to stop, current: %s", vps.Status)
	}

	region, err := s.vpsRegion(ctx, vpsID)
	if err != nil {
		log.Printf("[DEBUG] service_stop_instance: vps %d get region failed: %v", vpsID, err)
		return err
	}

	log.Printf("[DEBUG] service_stop_instance: vps %d region=%s instance_id=%s", vpsID, region, vps.OCIInstanceID.String)

	computeClient, err := s.computeService.GetComputeClient(ctx, region)
	if err != nil {
		log.Printf("[DEBUG] service_stop_instance: vps %d get compute client failed: %v", vpsID, err)
		return fmt.Errorf("get compute client: %w", err)
	}

	action := core.InstanceActionActionStop
	_, err = computeClient.InstanceAction(ctx, core.InstanceActionRequest{
		InstanceId: common.String(vps.OCIInstanceID.String),
		Action:     action,
	})
	if err != nil {
		log.Printf("[DEBUG] service_stop_instance: vps %d instance action STOP failed: %v", vpsID, err)
		return fmt.Errorf("instance action stop: %w", err)
	}

	if err := s.vpsRepo.UpdateStatus(ctx, vpsID, "stopped"); err != nil {
		log.Printf("[DEBUG] service_stop_instance: vps %d update status failed: %v", vpsID, err)
		return fmt.Errorf("update vps status: %w", err)
	}

	log.Printf("[DEBUG] service_stop_instance: vps %d stopped successfully", vpsID)

	s.broker.Publish(fmt.Sprintf("vps:%d", vpsID), sse.SSEEvent{
		Type:    "status",
		Status:  "stopped",
		Step:    "stopped",
		Message: "VPS instance stopped",
	})

	return nil
}

func (s *VPSProvisionService) RestartInstance(ctx context.Context, vpsID int64) error {
	log.Printf("[DEBUG] service_restart_instance: vps_id=%d", vpsID)

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		log.Printf("[DEBUG] service_restart_instance: vps %d get failed: %v", vpsID, err)
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		log.Printf("[DEBUG] service_restart_instance: vps %d not found", vpsID)
		return fmt.Errorf("vps %d not found", vpsID)
	}

	if !vps.OCIInstanceID.Valid || vps.OCIInstanceID.String == "" {
		log.Printf("[DEBUG] service_restart_instance: vps %d has no OCI instance ID", vpsID)
		return fmt.Errorf("vps has no OCI instance ID")
	}

	if vps.Status != "running" {
		log.Printf("[DEBUG] service_restart_instance: vps %d not in running state (current=%s)", vpsID, vps.Status)
		return fmt.Errorf("vps must be in running state to restart, current: %s", vps.Status)
	}

	region, err := s.vpsRegion(ctx, vpsID)
	if err != nil {
		log.Printf("[DEBUG] service_restart_instance: vps %d get region failed: %v", vpsID, err)
		return err
	}

	log.Printf("[DEBUG] service_restart_instance: vps %d region=%s instance_id=%s", vpsID, region, vps.OCIInstanceID.String)

	computeClient, err := s.computeService.GetComputeClient(ctx, region)
	if err != nil {
		log.Printf("[DEBUG] service_restart_instance: vps %d get compute client failed: %v", vpsID, err)
		return fmt.Errorf("get compute client: %w", err)
	}

	action := core.InstanceActionActionSoftreset
	_, err = computeClient.InstanceAction(ctx, core.InstanceActionRequest{
		InstanceId: common.String(vps.OCIInstanceID.String),
		Action:     action,
	})
	if err != nil {
		log.Printf("[DEBUG] service_restart_instance: vps %d instance action SOFTRESET failed: %v", vpsID, err)
		return fmt.Errorf("instance action restart: %w", err)
	}

	log.Printf("[DEBUG] service_restart_instance: vps %d restarted successfully", vpsID)

	s.broker.Publish(fmt.Sprintf("vps:%d", vpsID), sse.SSEEvent{
		Type:    "status",
		Status:  "running",
		Step:    "restarted",
		Message: "VPS instance restarted",
	})

	return nil
}

func (s *VPSProvisionService) ResetInstance(ctx context.Context, vpsID int64) error {
	log.Printf("[DEBUG] service_reset_instance: vps_id=%d", vpsID)

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		log.Printf("[DEBUG] service_reset_instance: vps %d get failed: %v", vpsID, err)
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		log.Printf("[DEBUG] service_reset_instance: vps %d not found", vpsID)
		return fmt.Errorf("vps %d not found", vpsID)
	}

	if !vps.OCIInstanceID.Valid || vps.OCIInstanceID.String == "" {
		log.Printf("[DEBUG] service_reset_instance: vps %d has no OCI instance ID", vpsID)
		return fmt.Errorf("vps has no OCI instance ID")
	}

	if vps.Status != "running" && vps.Status != "stopped" {
		log.Printf("[DEBUG] service_reset_instance: vps %d not in running/stopped state (current=%s)", vpsID, vps.Status)
		return fmt.Errorf("vps must be in running or stopped state to reset, current: %s", vps.Status)
	}

	region, err := s.vpsRegion(ctx, vpsID)
	if err != nil {
		log.Printf("[DEBUG] service_reset_instance: vps %d get region failed: %v", vpsID, err)
		return err
	}

	log.Printf("[DEBUG] service_reset_instance: vps %d region=%s instance_id=%s", vpsID, region, vps.OCIInstanceID.String)

	computeClient, err := s.computeService.GetComputeClient(ctx, region)
	if err != nil {
		log.Printf("[DEBUG] service_reset_instance: vps %d get compute client failed: %v", vpsID, err)
		return fmt.Errorf("get compute client: %w", err)
	}

	action := core.InstanceActionActionReset
	_, err = computeClient.InstanceAction(ctx, core.InstanceActionRequest{
		InstanceId: common.String(vps.OCIInstanceID.String),
		Action:     action,
	})
	if err != nil {
		log.Printf("[DEBUG] service_reset_instance: vps %d instance action RESET failed: %v", vpsID, err)
		return fmt.Errorf("instance action reset: %w", err)
	}

	log.Printf("[DEBUG] service_reset_instance: vps %d reset successfully", vpsID)

	s.broker.Publish(fmt.Sprintf("vps:%d", vpsID), sse.SSEEvent{
		Type:    "status",
		Status:  "running",
		Step:    "reset",
		Message: "VPS instance reset",
	})

	return nil
}

func (s *VPSProvisionService) ResetPassword(ctx context.Context, vpsID int64, newPassword string) error {
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

	log.Printf("[DEBUG] reset_password: vps %d resetting password for user %q via SSH", vpsID, vps.SSHUsername.String)

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
	log.Printf("[DEBUG] service_get_firewall: vps_id=%d", vpsID)

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		log.Printf("[DEBUG] service_get_firewall: vps %d get failed: %v", vpsID, err)
		return nil, fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		log.Printf("[DEBUG] service_get_firewall: vps %d not found", vpsID)
		return nil, fmt.Errorf("vps %d not found", vpsID)
	}

	if vps.NSGID == "" {
		log.Printf("[DEBUG] service_get_firewall: vps %d has no NSG assigned", vpsID)
		return nil, fmt.Errorf("no NSG associated with VPS %d", vpsID)
	}

	if !vps.NetworkID.Valid {
		log.Printf("[DEBUG] service_get_firewall: vps %d has no network assigned", vpsID)
		return nil, fmt.Errorf("vps has no network assigned")
	}

	network, err := s.networkRepo.Get(ctx, vps.NetworkID.Int64)
	if err != nil {
		log.Printf("[DEBUG] service_get_firewall: vps %d get network %d failed: %v", vpsID, vps.NetworkID.Int64, err)
		return nil, fmt.Errorf("get network: %w", err)
	}
	if network == nil {
		log.Printf("[DEBUG] service_get_firewall: vps %d network %d not found", vpsID, vps.NetworkID.Int64)
		return nil, fmt.Errorf("network not found")
	}

	log.Printf("[DEBUG] service_get_firewall: vps %d nsg_id=%s region=%s", vpsID, vps.NSGID, network.Region)

	nsgRules, err := s.computeService.ListNSGRules(ctx, network.Region, vps.NSGID)
	if err != nil {
		log.Printf("[DEBUG] service_get_firewall: vps %d ListNSGRules failed: %v", vpsID, err)
		return nil, fmt.Errorf("list NSG rules: %w", err)
	}

	log.Printf("[DEBUG] service_get_firewall: vps %d NSG has %d rules", vpsID, len(nsgRules))

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
	log.Printf("[DEBUG] service_update_firewall: vps_id=%d rules_count=%d", vpsID, len(rules))

	vps, err := s.vpsRepo.Get(ctx, vpsID)
	if err != nil {
		log.Printf("[DEBUG] service_update_firewall: vps %d get failed: %v", vpsID, err)
		return fmt.Errorf("get vps: %w", err)
	}
	if vps == nil {
		log.Printf("[DEBUG] service_update_firewall: vps %d not found", vpsID)
		return fmt.Errorf("vps %d not found", vpsID)
	}

	if vps.NSGID == "" {
		log.Printf("[DEBUG] service_update_firewall: vps %d has no NSG assigned", vpsID)
		return fmt.Errorf("no NSG associated with VPS %d", vpsID)
	}

	if !vps.NetworkID.Valid {
		log.Printf("[DEBUG] service_update_firewall: vps %d has no network assigned", vpsID)
		return fmt.Errorf("vps has no network assigned")
	}

	network, err := s.networkRepo.Get(ctx, vps.NetworkID.Int64)
	if err != nil {
		log.Printf("[DEBUG] service_update_firewall: vps %d get network %d failed: %v", vpsID, vps.NetworkID.Int64, err)
		return fmt.Errorf("get network: %w", err)
	}
	if network == nil {
		log.Printf("[DEBUG] service_update_firewall: vps %d network %d not found", vpsID, vps.NetworkID.Int64)
		return fmt.Errorf("network not found")
	}

	log.Printf("[DEBUG] service_update_firewall: vps %d nsg_id=%s region=%s", vpsID, vps.NSGID, network.Region)

	// 1. List current NSG rules to get their IDs for removal
	currentRules, err := s.computeService.ListNSGRules(ctx, network.Region, vps.NSGID)
	if err != nil {
		log.Printf("[DEBUG] service_update_firewall: vps %d ListNSGRules failed: %v", vpsID, err)
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
		log.Printf("[DEBUG] service_update_firewall: vps %d RemoveNSGRules failed: %v", vpsID, err)
		return fmt.Errorf("remove current NSG rules: %w", err)
	}

	// 3. Convert FirewallRules → AddSecurityRuleDetails and add them
	if len(rules) == 0 {
		log.Printf("[DEBUG] service_update_firewall: vps %d no rules to add, firewall cleared", vpsID)
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
		log.Printf("[DEBUG] service_update_firewall: vps %d AddNSGRules failed: %v", vpsID, err)
		return fmt.Errorf("add new NSG rules: %w", err)
	}

	log.Printf("[DEBUG] service_update_firewall: vps %d NSG rules updated successfully", vpsID)
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
