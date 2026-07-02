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

	request := core.LaunchInstanceRequest{
		LaunchInstanceDetails: core.LaunchInstanceDetails{
			AvailabilityDomain: adPtr,
			CompartmentId:      common.String(params.CompartmentOCID),
			DisplayName:        common.String(params.DisplayName),
			Shape:              common.String(params.Shape),
			Metadata: map[string]string{
				"user_data": userDataB64,
			},
			CreateVnicDetails: &core.CreateVnicDetails{
				SubnetId:       common.String(params.SubnetOCID),
				AssignPublicIp: common.Bool(true),
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
	s.log.Debug("oci_instance_terminated", "instance_id", instanceID)
	return nil
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
		User: "root",
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

	cmd := fmt.Sprintf(
		"{ id -u %[1]s 2>/dev/null || useradd -m -s /bin/bash %[1]s; } && echo '%[1]s:%[2]s' | chpasswd && printf 'PasswordAuthentication yes\nChallengeResponseAuthentication yes\n' > /etc/ssh/sshd_config.d/99-provinci.conf && sshd -t && systemctl restart sshd && sshd -T | grep -i passwordauthentication",
		shellEscapeTight(username),
		shellEscapeTight(password),
	)

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
