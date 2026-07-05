package service

import (
	"context"
	"fmt"
	"log"

	"github.com/oracle/oci-go-sdk/v65/core"

	"github.com/llassingan/provessor/internal/model"
	"github.com/llassingan/provessor/internal/repository"
	"github.com/llassingan/provessor/internal/sse"
)

type ReconcileService struct {
	networkRepo         *repository.NetworkRepository
	vpsRepo             *repository.VPSRepository
	networkResourceRepo *repository.NetworkResourceRepository
	vpsResourceRepo     *repository.VPSResourceRepository
	oci                 *OCIComputeService
	networkService      *NetworkService
	vpsProvisionService *VPSProvisionService
	broker              *sse.EventBroker
}

func NewReconcileService(
	networkRepo *repository.NetworkRepository,
	vpsRepo *repository.VPSRepository,
	networkResourceRepo *repository.NetworkResourceRepository,
	vpsResourceRepo *repository.VPSResourceRepository,
	oci *OCIComputeService,
	networkService *NetworkService,
	vpsProvisionService *VPSProvisionService,
	broker *sse.EventBroker,
) *ReconcileService {
	return &ReconcileService{
		networkRepo:         networkRepo,
		vpsRepo:             vpsRepo,
		networkResourceRepo: networkResourceRepo,
		vpsResourceRepo:     vpsResourceRepo,
		oci:                 oci,
		networkService:      networkService,
		vpsProvisionService: vpsProvisionService,
		broker:              broker,
	}
}

func (s *ReconcileService) ReconcileOnStartup(ctx context.Context) error {
	// Networks stuck in pending/provisioning
	stuckNetworks, err := s.networkRepo.ListByStatus(ctx, []string{"pending", "provisioning"})
	if err != nil {
		return fmt.Errorf("list stuck networks: %w", err)
	}
	for _, n := range stuckNetworks {
		s.reconcileNetwork(ctx, n)
	}

	// VPS stuck in pending/provisioning
	// VPSRepository.List takes a single status — call it twice
	pendingVPS, err := s.vpsRepo.List(ctx, "pending")
	if err != nil {
		return fmt.Errorf("list pending VPS: %w", err)
	}
	provisioningVPS, err := s.vpsRepo.List(ctx, "provisioning")
	if err != nil {
		return fmt.Errorf("list provisioning VPS: %w", err)
	}
	stuckVPS := append(pendingVPS, provisioningVPS...)
	for _, v := range stuckVPS {
		s.reconcileVPS(ctx, v)
	}

	// TODO(Phase 9): orphan detection via provessor:managed tag

	return nil
}

func (s *ReconcileService) reconcileNetwork(ctx context.Context, n model.Network) {
	log.Printf("[INFO] reconcile: network %d status=%s state=%s", n.ID, n.Status, n.ProvisioningState)

	// Load OCI settings to get region
	settings, err := s.networkService.settingsRepo.Get(ctx)
	if err != nil {
		log.Printf("[ERROR] reconcile: network %d load settings failed: %v", n.ID, err)
		return
	}
	region := settings.Region
	if region == "" {
		log.Printf("[ERROR] reconcile: network %d no region in settings", n.ID)
		return
	}

	// List tracked resources for this network
	resources, err := s.networkResourceRepo.ListByNetwork(ctx, n.ID)
	if err != nil {
		log.Printf("[ERROR] reconcile: network %d list resources failed: %v", n.ID, err)
		return
	}

	if len(resources) == 0 {
		// No resources tracked — reset to pending so user can retry
		log.Printf("[INFO] reconcile: network %d no tracked resources — resetting to pending", n.ID)
		_ = s.networkRepo.UpdateStatus(ctx, n.ID, "pending")
		_ = s.networkRepo.UpdateProvisioningState(ctx, n.ID, string(StatePending))
		return
	}

	// Resources exist — check if they exist in OCI
	// For each resource, verify via the appropriate Get call
	anyExists := false
	for _, r := range resources {
		switch r.ResourceType {
		case "vcn":
			vcn, gerr := s.oci.GetVCN(ctx, region, r.ResourceOCID)
			if gerr == nil && vcn.LifecycleState != core.VcnLifecycleStateTerminated {
				anyExists = true
			}
		case "subnet":
			// No GetSubnet method on OCIComputeService — assume exists if tracked
			anyExists = true
		case "igw":
			// No GetInternetGateway method — assume exists if tracked
			anyExists = true
		case "security_list":
			// No GetSecurityList method — assume exists if tracked
			anyExists = true
		}
	}

	if anyExists {
		// Resources exist in OCI — roll back (safer than partial resume)
		log.Printf("[INFO] reconcile: network %d has live OCI resources — rolling back", n.ID)
		s.networkService.RollbackNetwork(ctx, n.ID, region)
	} else {
		// Resources tracked but not in OCI — they were deleted out-of-band
		// Mark all as deleted, reset network to pending
		log.Printf("[INFO] reconcile: network %d tracked resources gone — resetting to pending", n.ID)
		for _, r := range resources {
			_ = s.networkResourceRepo.MarkDeleted(ctx, r.ResourceOCID)
		}
		_ = s.networkRepo.UpdateStatus(ctx, n.ID, "pending")
		_ = s.networkRepo.UpdateProvisioningState(ctx, n.ID, string(StatePending))
	}
}

func (s *ReconcileService) reconcileVPS(ctx context.Context, v model.VPS) {
	log.Printf("[INFO] reconcile: vps %d status=%s state=%s", v.ID, v.Status, v.ProvisioningState)

	// Need region from network
	if !v.NetworkID.Valid || v.NetworkID.Int64 == 0 {
		log.Printf("[ERROR] reconcile: vps %d no network_id — resetting to pending", v.ID)
		_ = s.vpsRepo.UpdateStatus(ctx, v.ID, "pending")
		_ = s.vpsRepo.UpdateProvisioningState(ctx, v.ID, string(StateVPSPending))
		return
	}
	network, err := s.networkRepo.Get(ctx, v.NetworkID.Int64)
	if err != nil {
		log.Printf("[ERROR] reconcile: vps %d load network %d failed: %v", v.ID, v.NetworkID.Int64, err)
		return
	}

	// If no instance ID — reset to pending (user can retry)
	if !v.OCIInstanceID.Valid || v.OCIInstanceID.String == "" {
		log.Printf("[INFO] reconcile: vps %d no instance ID — resetting to pending", v.ID)
		_ = s.vpsRepo.UpdateStatus(ctx, v.ID, "pending")
		_ = s.vpsRepo.UpdateProvisioningState(ctx, v.ID, string(StateVPSPending))
		return
	}

	instanceID := v.OCIInstanceID.String
	inst, gerr := s.oci.GetInstance(ctx, network.Region, instanceID)
	if gerr != nil {
		// Instance not found in OCI — roll back (delete tracked resources, set failed)
		log.Printf("[INFO] reconcile: vps %d instance %s not found — rolling back", v.ID, instanceID)
		s.vpsProvisionService.RollbackVPS(ctx, v.ID, network.Region, instanceID)
		return
	}

	switch inst.LifecycleState {
	case core.InstanceLifecycleStateRunning:
		log.Printf("[INFO] reconcile: vps %d instance RUNNING — marking running", v.ID)
		_ = s.vpsRepo.UpdateStatus(ctx, v.ID, "running")
		_ = s.vpsRepo.UpdateProvisioningState(ctx, v.ID, string(StateVPSReady))
	case core.InstanceLifecycleStateTerminated, core.InstanceLifecycleStateTerminating:
		log.Printf("[INFO] reconcile: vps %d instance TERMINATED — marking terminated", v.ID)
		_ = s.vpsRepo.UpdateStatus(ctx, v.ID, "terminated")
		_ = s.vpsRepo.UpdateProvisioningState(ctx, v.ID, string(StateVPSFailed))
	default:
		// PROVISIONING/STARTING/etc — leave alone, will reconcile next cycle
		log.Printf("[INFO] reconcile: vps %d instance state %s — leaving in progress", v.ID, inst.LifecycleState)
	}
}
