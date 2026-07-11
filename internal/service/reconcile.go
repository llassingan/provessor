package service

import (
	"context"
	"fmt"

	"github.com/oracle/oci-go-sdk/v65/core"

	"github.com/llassingan/provessor/internal/logger"
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
	log                 *logger.Logger
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
	log *logger.Logger,
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
		log:                 log,
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
	s.log.Info("reconcile_network", "network_id", n.ID, "status", n.Status, "state", n.ProvisioningState)

	settings, err := s.networkService.settingsRepo.Get(ctx)
	if err != nil {
		s.log.Error("reconcile_network_load_settings_failed", "network_id", n.ID, "error", err)
		return
	}
	region := settings.Region
	if region == "" {
		s.log.Error("reconcile_network_no_region", "network_id", n.ID)
		return
	}

	resources, err := s.networkResourceRepo.ListByNetwork(ctx, n.ID)
	if err != nil {
		s.log.Error("reconcile_network_list_resources_failed", "network_id", n.ID, "error", err)
		return
	}

	if len(resources) == 0 {
		s.log.Info("reconcile_network_no_resources_resetting", "network_id", n.ID)
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
		s.log.Info("reconcile_network_rolling_back", "network_id", n.ID)
		s.networkService.RollbackNetwork(ctx, n.ID, region)
	} else {
		s.log.Info("reconcile_network_resetting_to_pending", "network_id", n.ID)
		for _, r := range resources {
			_ = s.networkResourceRepo.MarkDeleted(ctx, r.ResourceOCID)
		}
		_ = s.networkRepo.UpdateStatus(ctx, n.ID, "pending")
		_ = s.networkRepo.UpdateProvisioningState(ctx, n.ID, string(StatePending))
	}
}

func (s *ReconcileService) reconcileVPS(ctx context.Context, v model.VPS) {
	s.log.Info("reconcile_vps", "vps_id", v.ID, "status", v.Status, "state", v.ProvisioningState)

	if !v.NetworkID.Valid || v.NetworkID.Int64 == 0 {
		s.log.Error("reconcile_vps_no_network_id", "vps_id", v.ID)
		_ = s.vpsRepo.UpdateStatus(ctx, v.ID, "pending")
		_ = s.vpsRepo.UpdateProvisioningState(ctx, v.ID, string(StateVPSPending))
		return
	}
	network, err := s.networkRepo.Get(ctx, v.NetworkID.Int64)
	if err != nil {
		s.log.Error("reconcile_vps_load_network_failed", "vps_id", v.ID, "network_id", v.NetworkID.Int64, "error", err)
		return
	}

	if !v.OCIInstanceID.Valid || v.OCIInstanceID.String == "" {
		s.log.Info("reconcile_vps_no_instance_id", "vps_id", v.ID)
		_ = s.vpsRepo.UpdateStatus(ctx, v.ID, "pending")
		_ = s.vpsRepo.UpdateProvisioningState(ctx, v.ID, string(StateVPSPending))
		return
	}

	instanceID := v.OCIInstanceID.String
	inst, gerr := s.oci.GetInstance(ctx, network.Region, instanceID)
	if gerr != nil {
		s.log.Info("reconcile_vps_instance_not_found_rolling_back", "vps_id", v.ID, "instance_id", instanceID)
		s.vpsProvisionService.RollbackVPS(ctx, v.ID, network.Region, instanceID)
		return
	}

	switch inst.LifecycleState {
	case core.InstanceLifecycleStateRunning:
		s.log.Info("reconcile_vps_marking_running", "vps_id", v.ID)
		_ = s.vpsRepo.UpdateStatus(ctx, v.ID, "running")
		_ = s.vpsRepo.UpdateProvisioningState(ctx, v.ID, string(StateVPSReady))
	case core.InstanceLifecycleStateTerminated, core.InstanceLifecycleStateTerminating:
		s.log.Info("reconcile_vps_marking_terminated", "vps_id", v.ID)
		_ = s.vpsRepo.UpdateStatus(ctx, v.ID, "terminated")
		_ = s.vpsRepo.UpdateProvisioningState(ctx, v.ID, string(StateVPSFailed))
	default:
		s.log.Info("reconcile_vps_leaving_in_progress", "vps_id", v.ID, "instance_state", string(inst.LifecycleState))
	}
}
