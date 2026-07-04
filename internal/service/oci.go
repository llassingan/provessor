package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/computeinstanceagent"
	"github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"golang.org/x/crypto/ssh"

	"vps-store/internal/logger"
	"vps-store/internal/repository"
)

type OCIComputeService struct {
	settingsRepo *repository.SettingsRepository
	log          *logger.Logger

	mu              sync.Mutex
	initialized     bool
	compartmentOCID string
	tenancyOCID     string
	userOCID        string
	fingerprint     string
	privateKey      string
	cachedClients   map[string]*regionClients
}

type regionClients struct {
	computeClient        core.ComputeClient
	virtualNetworkClient core.VirtualNetworkClient
	instanceAgentClient  computeinstanceagent.ComputeInstanceAgentClient
	identityClient       identity.IdentityClient
}

func NewOCIComputeService(settingsRepo *repository.SettingsRepository, log *logger.Logger) *OCIComputeService {
	return &OCIComputeService{
		settingsRepo:  settingsRepo,
		log:           log,
		cachedClients: make(map[string]*regionClients),
	}
}

func (s *OCIComputeService) init(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		return nil
	}

	s.log.Debug("oci_init_start")
	settings, err := s.settingsRepo.Get(ctx)
	if err != nil {
		s.log.Error("oci_init_get_settings_failed", "error", err)
		return fmt.Errorf("get settings: %w", err)
	}
	if settings == nil {
		s.log.Error("oci_init_no_settings")
		return fmt.Errorf("no OCI settings configured")
	}

	if settings.TenancyOCID == "" || settings.UserOCID == "" || settings.Fingerprint == "" ||
		settings.PrivateKey == "" || settings.CompartmentOCID == "" {
		s.log.Error("oci_init_incomplete_settings", "has_tenancy", settings.TenancyOCID != "", "has_user", settings.UserOCID != "", "has_fingerprint", settings.Fingerprint != "", "has_private_key", settings.PrivateKey != "", "has_compartment", settings.CompartmentOCID != "")
		return fmt.Errorf("incomplete OCI settings")
	}

	s.tenancyOCID = settings.TenancyOCID
	s.userOCID = settings.UserOCID
	s.fingerprint = settings.Fingerprint
	s.privateKey = settings.PrivateKey
	s.compartmentOCID = settings.CompartmentOCID
	s.initialized = true
	s.log.Debug("oci_init_complete", "compartment_ocid", maskOCID(s.compartmentOCID), "tenancy_ocid", maskOCID(s.tenancyOCID))
	return nil
}

func (s *OCIComputeService) clientProvider(region string) common.ConfigurationProvider {
	return common.NewRawConfigurationProvider(
		s.tenancyOCID,
		s.userOCID,
		region,
		s.fingerprint,
		s.privateKey,
		nil,
	)
}

func (s *OCIComputeService) getOrCreateClients(ctx context.Context, region string) (*regionClients, error) {
	if err := s.init(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if c, ok := s.cachedClients[region]; ok {
		return c, nil
	}

	s.log.Debug("creating_oci_clients", "region", region)

	provider := s.clientProvider(region)

	computeClient, err := core.NewComputeClientWithConfigurationProvider(provider)
	if err != nil {
		s.log.Error("create_compute_client_failed", "region", region, "error", err)
		return nil, fmt.Errorf("create compute client for %s: %w", region, err)
	}

	virtualNetworkClient, err := core.NewVirtualNetworkClientWithConfigurationProvider(provider)
	if err != nil {
		s.log.Error("create_network_client_failed", "region", region, "error", err)
		return nil, fmt.Errorf("create virtual network client for %s: %w", region, err)
	}

	instanceAgentClient, err := computeinstanceagent.NewComputeInstanceAgentClientWithConfigurationProvider(provider)
	if err != nil {
		s.log.Error("create_instance_agent_client_failed", "region", region, "error", err)
		return nil, fmt.Errorf("create instance agent client for %s: %w", region, err)
	}

	identityClient, err := identity.NewIdentityClientWithConfigurationProvider(provider)
	if err != nil {
		s.log.Error("create_identity_client_failed", "region", region, "error", err)
		return nil, fmt.Errorf("create identity client for %s: %w", region, err)
	}

	c := &regionClients{
		computeClient:        computeClient,
		virtualNetworkClient: virtualNetworkClient,
		instanceAgentClient:  instanceAgentClient,
		identityClient:       identityClient,
	}
	s.cachedClients[region] = c
	s.log.Debug("oci_clients_created", "region", region)
	return c, nil
}

func (s *OCIComputeService) GetComputeClient(ctx context.Context, region string) (core.ComputeClient, error) {
	c, err := s.getOrCreateClients(ctx, region)
	if err != nil {
		return core.ComputeClient{}, err
	}
	return c.computeClient, nil
}

func (s *OCIComputeService) GetNetworkClient(ctx context.Context, region string) (core.VirtualNetworkClient, error) {
	c, err := s.getOrCreateClients(ctx, region)
	if err != nil {
		return core.VirtualNetworkClient{}, err
	}
	return c.virtualNetworkClient, nil
}

func (s *OCIComputeService) GetInstanceAgentClient(ctx context.Context, region string) (computeinstanceagent.ComputeInstanceAgentClient, error) {
	c, err := s.getOrCreateClients(ctx, region)
	if err != nil {
		return computeinstanceagent.ComputeInstanceAgentClient{}, err
	}
	return c.instanceAgentClient, nil
}

func (s *OCIComputeService) getIdentityClient(ctx context.Context, region string) (identity.IdentityClient, error) {
	c, err := s.getOrCreateClients(ctx, region)
	if err != nil {
		return identity.IdentityClient{}, err
	}
	return c.identityClient, nil
}

// availabilityDomain returns the first availability domain name for the given
// region. OCI AD names include a 4-character prefix (e.g., HgYx:ap-batam-1-AD-1)
// that cannot be derived from the region string alone — we must call the API.
func (s *OCIComputeService) availabilityDomain(ctx context.Context, region, compartmentOCID string) (string, error) {
	idClient, err := s.getIdentityClient(ctx, region)
	if err != nil {
		return "", err
	}

	resp, err := idClient.ListAvailabilityDomains(ctx, identity.ListAvailabilityDomainsRequest{
		CompartmentId: common.String(compartmentOCID),
	})
	if err != nil {
		return "", fmt.Errorf("list availability domains: %w", err)
	}
	if len(resp.Items) == 0 {
		return "", fmt.Errorf("no availability domains found in %s", region)
	}

	ad := *resp.Items[0].Name
	s.log.Debug("oci_resolved_ad", "region", region, "ad", ad)
	return ad, nil
}

func (s *OCIComputeService) GetCompartmentOCID(ctx context.Context) (string, error) {
	if err := s.init(ctx); err != nil {
		return "", err
	}
	return s.compartmentOCID, nil
}

type LaunchInstanceParams struct {
	Region             string
	CompartmentOCID    string
	SubnetOCID         string
	DisplayName        string
	Shape              string
	OCPU               float64
	MemoryGB           float64
	BootVolumeSizeGB   int
	CloudInitYAML      string
	NSGID              string
	SSHPublicKey       string
}

func (s *OCIComputeService) LaunchInstance(ctx context.Context, params LaunchInstanceParams) (string, error) {
	computeClient, err := s.GetComputeClient(ctx, params.Region)
	if err != nil {
		return "", fmt.Errorf("get compute client: %w", err)
	}

	ad, err := s.availabilityDomain(ctx, params.Region, params.CompartmentOCID)
	if err != nil {
		return "", fmt.Errorf("get availability domain: %w", err)
	}
	adPtr := common.String(ad)

	s.log.Debug("oci_finding_image", "shape", params.Shape)
	images, err := computeClient.ListImages(ctx, core.ListImagesRequest{
		CompartmentId:           common.String(params.CompartmentOCID),
		OperatingSystem:         common.String("Canonical Ubuntu"),
		OperatingSystemVersion:  common.String("22.04"),
		Shape:                   common.String(params.Shape),
		SortBy:                  core.ListImagesSortByTimecreated,
		SortOrder:               core.ListImagesSortOrderDesc,
		Limit:                   common.Int(1),
	})
	if err != nil {
		s.log.Error("oci_list_images_failed", "error", err)
		return "", fmt.Errorf("list images: %w", err)
	}
	if len(images.Items) == 0 {
		s.log.Error("oci_no_images_found", "shape", params.Shape)
		return "", fmt.Errorf("no Ubuntu 22.04 image found for shape %s in %s", params.Shape, params.Region)
	}

	imageID := images.Items[0].Id
	s.log.Debug("oci_image_found", "image_id", *imageID, "image_name", *images.Items[0].DisplayName)

	userDataB64 := base64.StdEncoding.EncodeToString([]byte(params.CloudInitYAML))

	s.log.Debug("oci_launching_instance", "display_name", params.DisplayName, "shape", params.Shape, "ad", ad, "subnet", maskOCID(params.SubnetOCID))

	var nsgIDs []string
	if params.NSGID != "" {
		nsgIDs = []string{params.NSGID}
	}

	metadata := map[string]string{
		"user_data": userDataB64,
	}
	if params.SSHPublicKey != "" {
		metadata["ssh_authorized_keys"] = params.SSHPublicKey
	}

	request := core.LaunchInstanceRequest{
		LaunchInstanceDetails: core.LaunchInstanceDetails{
			AvailabilityDomain: adPtr,
			CompartmentId:      common.String(params.CompartmentOCID),
			DisplayName:        common.String(params.DisplayName),
			Shape:              common.String(params.Shape),
			Metadata:           metadata,
			CreateVnicDetails: &core.CreateVnicDetails{
				SubnetId:       common.String(params.SubnetOCID),
				AssignPublicIp: common.Bool(true),
				NsgIds:         nsgIDs,
			},
			SourceDetails: core.InstanceSourceViaImageDetails{
				ImageId:             imageID,
				BootVolumeSizeInGBs: common.Int64(int64(params.BootVolumeSizeGB)),
			},
		},
	}

	if strings.Contains(params.Shape, ".Flex") {
		request.ShapeConfig = &core.LaunchInstanceShapeConfigDetails{
			Ocpus:       common.Float32(float32(params.OCPU)),
			MemoryInGBs: common.Float32(float32(params.MemoryGB)),
		}
	}

	if body, err := json.Marshal(request.LaunchInstanceDetails); err == nil {
		s.log.Debug("oci_launch_request_body", "body", string(body))
	}

	response, err := computeClient.LaunchInstance(ctx, request)
	if err != nil {
		s.log.Error("oci_launch_instance_failed", "error", err)
		return "", fmt.Errorf("launch instance: %w", err)
	}

	instanceID := *response.Instance.Id
	s.log.Debug("oci_instance_launched", "instance_id", instanceID, "state", string(response.Instance.LifecycleState))
	return instanceID, nil
}

func (s *OCIComputeService) GetInstance(ctx context.Context, region, instanceID string) (*core.Instance, error) {
	computeClient, err := s.GetComputeClient(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("get compute client: %w", err)
	}
	resp, err := computeClient.GetInstance(ctx, core.GetInstanceRequest{
		InstanceId: common.String(instanceID),
	})
	if err != nil {
		return nil, fmt.Errorf("get instance: %w", err)
	}
	return &resp.Instance, nil
}

// GetInstanceIPs retrieves the public and private IP addresses for a running
// OCI instance. It follows the OCI-recommended two-step process:
//  1. ListVnicAttachments → get the VNIC OCIDs attached to the instance
//  2. GetVnic → get the actual IP addresses from the primary VNIC
//
// The OCI core.Instance struct does NOT carry IP addresses directly.
func (s *OCIComputeService) GetInstanceIPs(ctx context.Context, region, instanceID, compartmentOCID string) (publicIP, privateIP string, err error) {
	s.log.Debug("oci_get_instance_ips_start", "instance_id", instanceID, "region", region)

	computeClient, err := s.GetComputeClient(ctx, region)
	if err != nil {
		return "", "", fmt.Errorf("get compute client: %w", err)
	}
	networkClient, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return "", "", fmt.Errorf("get network client: %w", err)
	}

	// Step 1: List VNIC attachments for this instance
	listResp, err := computeClient.ListVnicAttachments(ctx, core.ListVnicAttachmentsRequest{
		CompartmentId: common.String(compartmentOCID),
		InstanceId:    common.String(instanceID),
	})
	if err != nil {
		s.log.Error("oci_list_vnic_attachments_failed", "instance_id", instanceID, "error", err)
		return "", "", fmt.Errorf("list VNIC attachments: %w", err)
	}

	s.log.Debug("oci_vnic_attachments_listed", "instance_id", instanceID, "count", len(listResp.Items))

	// Step 2: Find the first ATTACHED VNIC and fetch its IPs
	for _, att := range listResp.Items {
		if att.LifecycleState != core.VnicAttachmentLifecycleStateAttached {
			s.log.Debug("oci_vnic_attachment_skipped", "state", string(att.LifecycleState))
			continue
		}
		if att.VnicId == nil {
			continue
		}

		s.log.Debug("oci_getting_vnic", "vnic_id", *att.VnicId)
		getResp, err := networkClient.GetVnic(ctx, core.GetVnicRequest{
			VnicId: att.VnicId,
		})
		if err != nil {
			s.log.Error("oci_get_vnic_failed", "vnic_id", *att.VnicId, "error", err)
			return "", "", fmt.Errorf("get VNIC %s: %w", *att.VnicId, err)
		}

		if getResp.Vnic.PrivateIp != nil {
			privateIP = *getResp.Vnic.PrivateIp
		}
		if getResp.Vnic.PublicIp != nil {
			publicIP = *getResp.Vnic.PublicIp
		}

		s.log.Debug("oci_instance_ips_retrieved", "instance_id", instanceID, "public_ip", publicIP, "private_ip", privateIP)
		return publicIP, privateIP, nil
	}

	return "", "", fmt.Errorf("no attached VNIC found for instance %s", instanceID)
}

func (s *OCIComputeService) TerminateInstance(ctx context.Context, region, instanceID string) error {
	computeClient, err := s.GetComputeClient(ctx, region)
	if err != nil {
		return fmt.Errorf("get compute client: %w", err)
	}
	s.log.Debug("oci_terminating_instance", "instance_id", instanceID, "region", region)
	_, err = computeClient.TerminateInstance(ctx, core.TerminateInstanceRequest{
		InstanceId: common.String(instanceID),
	})
	if err != nil {
		s.log.Error("oci_terminate_instance_failed", "instance_id", instanceID, "error", err)
		return fmt.Errorf("terminate instance: %w", err)
	}
	if werr := s.waitForInstanceTerminated(ctx, region, instanceID, 5*time.Minute); werr != nil {
		s.log.Warn("oci_wait_for_terminated_failed", "instance_id", instanceID, "error", werr)
	}
	s.log.Debug("oci_instance_terminated", "instance_id", instanceID)
	return nil
}

// waitForInstanceTerminated polls GetInstance until LifecycleState == TERMINATED.
// OCI's TerminateInstance API is async (returns 202 immediately); the VNIC
// stays attached to any NSG until the instance reaches TERMINATED. Callers that
// delete the NSG right after TerminateInstance must wait for this, otherwise
// OCI rejects with 412 PreconditionFailed ("NSG still has vnics attached").
func (s *OCIComputeService) waitForInstanceTerminated(ctx context.Context, region, instanceID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		inst, err := s.GetInstance(ctx, region, instanceID)
		if err != nil {
			return err
		}
		if inst.LifecycleState == core.InstanceLifecycleStateTerminated {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return fmt.Errorf("instance %s not terminated after %v", maskOCID(instanceID), timeout)
}

// GenerateSSHKeyPair generates an RSA 4096 key pair.
// Returns the public key in OpenSSH authorized_keys format and the
// private key in PEM format.
func GenerateSSHKeyPair() (publicKey string, privateKeyPEM string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", "", fmt.Errorf("generate RSA key: %w", err)
	}

	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("marshal public key: %w", err)
	}
	publicKey = string(ssh.MarshalAuthorizedKey(pub))

	privBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("marshal private key: %w", err)
	}
	privateKeyPEM = string(pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	}))

	return publicKey, privateKeyPEM, nil
}

// SSHCreateUser connects to an instance via SSH using the provided private key
// and creates a new user with the given password.
func SSHCreateUser(host string, privateKeyPEM string, username string, password string) error {
	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: "ubuntu",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	addr := net.JoinHostPort(host, "22")
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	innerCmd := fmt.Sprintf(
		"{ id -u %[1]s 2>/dev/null || useradd -m -s /bin/bash %[1]s; } && echo '%[1]s:%[2]s' | chpasswd && printf 'PasswordAuthentication yes\nChallengeResponseAuthentication yes\n' > /etc/ssh/sshd_config.d/99-provessor.conf && sshd -t && systemctl restart sshd && sshd -T | grep -i passwordauthentication",
		shellEscapeTight(username),
		shellEscapeTight(password),
	)
	cmd := fmt.Sprintf("sudo bash -c '%s'", strings.ReplaceAll(innerCmd, "'", `'\''`))

	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return fmt.Errorf("ssh setup: %w (output: %s)", err, string(out))
	}

	return nil
}

// SSHVerifyPasswordLogin tests that a user can authenticate with password.
// Retries up to 5 times with 5 second pauses between attempts to account
// for sshd restart propagation delay.
func SSHVerifyPasswordLogin(host string, username string, password string) error {
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				if len(questions) == 0 {
					return []string{}, nil
				}
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = password
				}
				return answers, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(host, "22")
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		client, err := ssh.Dial("tcp", addr, config)
		if err == nil {
			client.Close()
			return nil
		}
		lastErr = fmt.Errorf("password login failed (attempt %d/5): %w", attempt, err)
		if attempt < 5 {
			time.Sleep(5 * time.Second)
		}
	}
	return lastErr
}

func shellEscapeTight(s string) string {
	result := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			result = append(result, '\'', '\\', '\'', '\'')
		} else {
			result = append(result, c)
		}
	}
	return string(result)
}

// ── Network SDK methods (VirtualNetworkClient) ─────────────────────────

func managedTags(networkID int64) map[string]string {
	return map[string]string{
		"provessor:managed":    "true",
		"provessor:network_id": strconv.FormatInt(networkID, 10),
	}
}

func vpsManagedTags(vpsID int64) map[string]string {
	return map[string]string{
		"provessor:managed": "true",
		"provessor:vps_id":  strconv.FormatInt(vpsID, 10),
	}
}

func (s *OCIComputeService) CreateVCN(ctx context.Context, region, compartmentID, displayName, cidrBlock, dnsLabel string, networkID int64) (string, error) {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return "", fmt.Errorf("get network client: %w", err)
	}
	resp, err := client.CreateVcn(ctx, core.CreateVcnRequest{
		CreateVcnDetails: core.CreateVcnDetails{
			CompartmentId: common.String(compartmentID),
			CidrBlock:     common.String(cidrBlock),
			DisplayName:   common.String(displayName),
			DnsLabel:      common.String(dnsLabel),
			FreeformTags:  managedTags(networkID),
		},
	})
	if err != nil {
		return "", fmt.Errorf("create vcn: %w", err)
	}
	if err := s.waitForVcnAvailable(ctx, region, *resp.Id); err != nil {
		return "", err
	}
	return *resp.Id, nil
}

func (s *OCIComputeService) waitForVcnAvailable(ctx context.Context, region, ocid string) error {
	deadline := time.Now().Add(5 * time.Minute)
	for {
		vcn, err := s.GetVCN(ctx, region, ocid)
		if err != nil {
			return fmt.Errorf("wait vcn available: %w", err)
		}
		if vcn.LifecycleState == core.VcnLifecycleStateAvailable {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("vcn %s not available after 5m", maskOCID(ocid))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *OCIComputeService) GetVCN(ctx context.Context, region, ocid string) (core.Vcn, error) {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return core.Vcn{}, fmt.Errorf("get network client: %w", err)
	}
	resp, err := client.GetVcn(ctx, core.GetVcnRequest{
		VcnId: common.String(ocid),
	})
	if err != nil {
		return core.Vcn{}, fmt.Errorf("get vcn: %w", err)
	}
	return resp.Vcn, nil
}

func (s *OCIComputeService) DeleteVCN(ctx context.Context, region, ocid string) error {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return fmt.Errorf("get network client: %w", err)
	}
	_, err = client.DeleteVcn(ctx, core.DeleteVcnRequest{
		VcnId: common.String(ocid),
	})
	if err != nil {
		return fmt.Errorf("delete vcn: %w", err)
	}
	return nil
}

func (s *OCIComputeService) ListVcns(ctx context.Context, region, compartmentID string) ([]core.Vcn, error) {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("get network client: %w", err)
	}
	resp, err := client.ListVcns(ctx, core.ListVcnsRequest{
		CompartmentId: common.String(compartmentID),
	})
	if err != nil {
		return nil, fmt.Errorf("list vcns: %w", err)
	}
	return resp.Items, nil
}

func (s *OCIComputeService) CreateInternetGateway(ctx context.Context, region, compartmentID, vcnID, displayName string, networkID int64) (string, error) {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return "", fmt.Errorf("get network client: %w", err)
	}
	resp, err := client.CreateInternetGateway(ctx, core.CreateInternetGatewayRequest{
		CreateInternetGatewayDetails: core.CreateInternetGatewayDetails{
			CompartmentId: common.String(compartmentID),
			VcnId:         common.String(vcnID),
			IsEnabled:     common.Bool(true),
			DisplayName:   common.String(displayName),
			FreeformTags:  managedTags(networkID),
		},
	})
	if err != nil {
		return "", fmt.Errorf("create internet gateway: %w", err)
	}
	return *resp.Id, nil
}

func (s *OCIComputeService) DeleteInternetGateway(ctx context.Context, region, ocid string) error {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return fmt.Errorf("get network client: %w", err)
	}
	_, err = client.DeleteInternetGateway(ctx, core.DeleteInternetGatewayRequest{
		IgId: common.String(ocid),
	})
	if err != nil {
		return fmt.Errorf("delete internet gateway: %w", err)
	}
	return nil
}

func (s *OCIComputeService) GetRouteTable(ctx context.Context, region, ocid string) (core.RouteTable, error) {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return core.RouteTable{}, fmt.Errorf("get network client: %w", err)
	}
	resp, err := client.GetRouteTable(ctx, core.GetRouteTableRequest{
		RtId: common.String(ocid),
	})
	if err != nil {
		return core.RouteTable{}, fmt.Errorf("get route table: %w", err)
	}
	return resp.RouteTable, nil
}

func (s *OCIComputeService) UpdateRouteTable(ctx context.Context, region, rtOCID string, rules []core.RouteRule) error {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return fmt.Errorf("get network client: %w", err)
	}
	_, err = client.UpdateRouteTable(ctx, core.UpdateRouteTableRequest{
		RtId: common.String(rtOCID),
		UpdateRouteTableDetails: core.UpdateRouteTableDetails{
			RouteRules: rules,
		},
	})
	if err != nil {
		return fmt.Errorf("update route table: %w", err)
	}
	return nil
}

func (s *OCIComputeService) CreateSubnet(ctx context.Context, region, compartmentID, vcnID, displayName, cidrBlock, dnsLabel, securityListID string, networkID int64) (string, error) {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return "", fmt.Errorf("get network client: %w", err)
	}
	resp, err := client.CreateSubnet(ctx, core.CreateSubnetRequest{
		CreateSubnetDetails: core.CreateSubnetDetails{
			CompartmentId:   common.String(compartmentID),
			VcnId:           common.String(vcnID),
			CidrBlock:       common.String(cidrBlock),
			DisplayName:     common.String(displayName),
			DnsLabel:        common.String(dnsLabel),
			SecurityListIds: []string{securityListID},
			FreeformTags:    managedTags(networkID),
		},
	})
	if err != nil {
		return "", fmt.Errorf("create subnet: %w", err)
	}
	if err := s.waitForSubnetAvailable(ctx, region, *resp.Id); err != nil {
		return "", err
	}
	return *resp.Id, nil
}

func (s *OCIComputeService) waitForSubnetAvailable(ctx context.Context, region, ocid string) error {
	deadline := time.Now().Add(5 * time.Minute)
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return fmt.Errorf("get network client: %w", err)
	}
	for {
		resp, err := client.GetSubnet(ctx, core.GetSubnetRequest{
			SubnetId: common.String(ocid),
		})
		if err != nil {
			return fmt.Errorf("wait subnet available: %w", err)
		}
		if resp.LifecycleState == core.SubnetLifecycleStateAvailable {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("subnet %s not available after 5m", maskOCID(ocid))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *OCIComputeService) waitForRouteTableCleared(ctx context.Context, region, rtID string, igwOCIDs map[string]bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		rt, err := s.GetRouteTable(ctx, region, rtID)
		if err != nil {
			return fmt.Errorf("wait route table: %w", err)
		}
		stillRefs := false
		if igwOCIDs == nil {
			stillRefs = len(rt.RouteRules) > 0
		} else {
			for _, rule := range rt.RouteRules {
				if rule.NetworkEntityId != nil && igwOCIDs[*rule.NetworkEntityId] {
					stillRefs = true
					break
				}
			}
		}
		if !stillRefs {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("route table %s still has routes after %v", maskOCID(rtID), timeout)
}

func (s *OCIComputeService) WaitForSubnetTerminated(ctx context.Context, region, ocid string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return fmt.Errorf("get network client: %w", err)
	}
	for time.Now().Before(deadline) {
		resp, err := client.GetSubnet(ctx, core.GetSubnetRequest{
			SubnetId: common.String(ocid),
		})
		if err != nil {
			return fmt.Errorf("wait subnet terminated: %w", err)
		}
		if resp.LifecycleState == core.SubnetLifecycleStateTerminated {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("subnet %s not terminated after %v", maskOCID(ocid), timeout)
}

func (s *OCIComputeService) DeleteSubnet(ctx context.Context, region, ocid string) error {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return fmt.Errorf("get network client: %w", err)
	}
	_, err = client.DeleteSubnet(ctx, core.DeleteSubnetRequest{
		SubnetId: common.String(ocid),
	})
	if err != nil {
		return fmt.Errorf("delete subnet: %w", err)
	}
	return nil
}

func (s *OCIComputeService) CreateSecurityList(ctx context.Context, region, compartmentID, vcnID, displayName string, networkID int64) (string, error) {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return "", fmt.Errorf("get network client: %w", err)
	}
	resp, err := client.CreateSecurityList(ctx, core.CreateSecurityListRequest{
		CreateSecurityListDetails: core.CreateSecurityListDetails{
			CompartmentId: common.String(compartmentID),
			VcnId:         common.String(vcnID),
			DisplayName:   common.String(displayName),
			IngressSecurityRules: []core.IngressSecurityRule{
				{
					Protocol:    common.String("6"),
					Source:      common.String("0.0.0.0/0"),
					SourceType:  core.IngressSecurityRuleSourceTypeCidrBlock,
					TcpOptions: &core.TcpOptions{
						DestinationPortRange: &core.PortRange{
							Min: common.Int(22),
							Max: common.Int(22),
						},
					},
				},
			},
			EgressSecurityRules: []core.EgressSecurityRule{
				{
					Protocol:        common.String("all"),
					Destination:     common.String("0.0.0.0/0"),
					DestinationType: core.EgressSecurityRuleDestinationTypeCidrBlock,
				},
			},
			FreeformTags: managedTags(networkID),
		},
	})
	if err != nil {
		return "", fmt.Errorf("create security list: %w", err)
	}
	return *resp.Id, nil
}

func (s *OCIComputeService) DeleteSecurityList(ctx context.Context, region, ocid string) error {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return fmt.Errorf("get network client: %w", err)
	}
	_, err = client.DeleteSecurityList(ctx, core.DeleteSecurityListRequest{
		SecurityListId: common.String(ocid),
	})
	if err != nil {
		return fmt.Errorf("delete security list: %w", err)
	}
	return nil
}

func (s *OCIComputeService) ListSecurityListsByVCN(ctx context.Context, region, compartmentID, vcnID string) ([]core.SecurityList, error) {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("get network client: %w", err)
	}
	resp, err := client.ListSecurityLists(ctx, core.ListSecurityListsRequest{
		CompartmentId: common.String(compartmentID),
		VcnId:         common.String(vcnID),
	})
	if err != nil {
		return nil, fmt.Errorf("list security lists: %w", err)
	}
	return resp.Items, nil
}

func (s *OCIComputeService) ListInternetGatewaysByVCN(ctx context.Context, region, compartmentID, vcnID string) ([]core.InternetGateway, error) {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return nil, fmt.Errorf("get network client: %w", err)
	}
	resp, err := client.ListInternetGateways(ctx, core.ListInternetGatewaysRequest{
		CompartmentId: common.String(compartmentID),
		VcnId:         common.String(vcnID),
	})
	if err != nil {
		return nil, fmt.Errorf("list internet gateways: %w", err)
	}
	return resp.Items, nil
}

// ── NSG (Network Security Group) methods ──────────────────────────────

func (s *OCIComputeService) CreateNSG(ctx context.Context, region, compartmentID, vcnID, displayName string, vpsID int64) (string, error) {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return "", err
	}
	resp, err := client.CreateNetworkSecurityGroup(ctx, core.CreateNetworkSecurityGroupRequest{
		CreateNetworkSecurityGroupDetails: core.CreateNetworkSecurityGroupDetails{
			CompartmentId: common.String(compartmentID),
			VcnId:         common.String(vcnID),
			DisplayName:   common.String(displayName),
			FreeformTags:  vpsManagedTags(vpsID),
		},
	})
	if err != nil {
		return "", err
	}
	return *resp.Id, nil
}

func (s *OCIComputeService) AddNSGRules(ctx context.Context, region, nsgID string, rules []core.AddSecurityRuleDetails) error {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return err
	}
	_, err = client.AddNetworkSecurityGroupSecurityRules(ctx, core.AddNetworkSecurityGroupSecurityRulesRequest{
		NetworkSecurityGroupId: common.String(nsgID),
		AddNetworkSecurityGroupSecurityRulesDetails: core.AddNetworkSecurityGroupSecurityRulesDetails{
			SecurityRules: rules,
		},
	})
	return err
}

func (s *OCIComputeService) ListNSGRules(ctx context.Context, region, nsgID string) ([]core.SecurityRule, error) {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return nil, err
	}
	resp, err := client.ListNetworkSecurityGroupSecurityRules(ctx, core.ListNetworkSecurityGroupSecurityRulesRequest{
		NetworkSecurityGroupId: common.String(nsgID),
	})
	if err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (s *OCIComputeService) RemoveNSGRules(ctx context.Context, region, nsgID string, ruleIDs []string) error {
	if len(ruleIDs) == 0 {
		return nil
	}
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return err
	}
	_, err = client.RemoveNetworkSecurityGroupSecurityRules(ctx, core.RemoveNetworkSecurityGroupSecurityRulesRequest{
		NetworkSecurityGroupId: common.String(nsgID),
		RemoveNetworkSecurityGroupSecurityRulesDetails: core.RemoveNetworkSecurityGroupSecurityRulesDetails{
			SecurityRuleIds: ruleIDs,
		},
	})
	return err
}

func (s *OCIComputeService) DeleteNSG(ctx context.Context, region, nsgID string) error {
	client, err := s.GetNetworkClient(ctx, region)
	if err != nil {
		return err
	}
	_, err = client.DeleteNetworkSecurityGroup(ctx, core.DeleteNetworkSecurityGroupRequest{
		NetworkSecurityGroupId: common.String(nsgID),
	})
	return err
}
