package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"

	"vps-store/internal/logger"
	"vps-store/internal/repository"
	"vps-store/internal/sse"
)

type NetworkService struct {
	settingsRepo        *repository.SettingsRepository
	networkRepo         *repository.NetworkRepository
	networkResourceRepo *repository.NetworkResourceRepository
	oci                 *OCIComputeService
	broker              *sse.EventBroker
	log                 *logger.Logger
}

func NewNetworkService(
	settingsRepo *repository.SettingsRepository,
	networkRepo *repository.NetworkRepository,
	networkResourceRepo *repository.NetworkResourceRepository,
	oci *OCIComputeService,
	broker *sse.EventBroker,
	log *logger.Logger,
) *NetworkService {
	return &NetworkService{
		settingsRepo:        settingsRepo,
		networkRepo:         networkRepo,
		networkResourceRepo: networkResourceRepo,
		oci:                 oci,
		broker:              broker,
		log:                 log,
	}
}

func (s *NetworkService) createOrGetVCN(ctx context.Context, networkID int64, region, compartmentID, name, cidr, dnsLabel string) (string, error) {
	existing, err := s.networkResourceRepo.GetByNetworkAndType(ctx, networkID, "vcn")
	if err == nil && existing != nil && existing.ResourceOCID != "" {
		vcn, gerr := s.oci.GetVCN(ctx, region, existing.ResourceOCID)
		if gerr == nil && vcn.LifecycleState == core.VcnLifecycleStateAvailable {
			return existing.ResourceOCID, nil
		}
	}
	ocid, cerr := s.oci.CreateVCN(ctx, region, compartmentID, name, cidr, dnsLabel, networkID)
	if cerr != nil {
		return "", cerr
	}
	if _, derr := s.networkResourceRepo.Create(ctx, networkID, "vcn", ocid); derr != nil {
		s.log.Warn("network_resource_track_failed", "network_id", networkID, "type", "vcn", "error", derr)
	}
	if uerr := s.networkResourceRepo.UpdateStatus(ctx, ocid, "available"); uerr != nil {
		s.log.Warn("network_resource_status_update_failed", "ocid", ocid, "error", uerr)
	}
	return ocid, nil
}

func (s *NetworkService) createOrGetIGW(ctx context.Context, networkID int64, region, compartmentID, vcnID, displayName string) (string, error) {
	existing, err := s.networkResourceRepo.GetByNetworkAndType(ctx, networkID, "igw")
	if err == nil && existing != nil && existing.ResourceOCID != "" {
		// IGW doesn't have a dedicated Get* method — verify by listing
		igws, lerr := s.oci.ListInternetGatewaysByVCN(ctx, region, compartmentID, vcnID)
		if lerr == nil {
			for _, igw := range igws {
				if igw.Id != nil && *igw.Id == existing.ResourceOCID &&
					igw.LifecycleState == core.InternetGatewayLifecycleStateAvailable {
					return existing.ResourceOCID, nil
				}
			}
		}
	}
	ocid, cerr := s.oci.CreateInternetGateway(ctx, region, compartmentID, vcnID, displayName, networkID)
	if cerr != nil {
		return "", cerr
	}
	if _, derr := s.networkResourceRepo.Create(ctx, networkID, "igw", ocid); derr != nil {
		s.log.Warn("network_resource_track_failed", "network_id", networkID, "type", "igw", "error", derr)
	}
	if uerr := s.networkResourceRepo.UpdateStatus(ctx, ocid, "available"); uerr != nil {
		s.log.Warn("network_resource_status_update_failed", "ocid", ocid, "error", uerr)
	}
	return ocid, nil
}

func (s *NetworkService) createOrGetSecurityList(ctx context.Context, networkID int64, region, compartmentID, vcnID, displayName string) (string, error) {
	existing, err := s.networkResourceRepo.GetByNetworkAndType(ctx, networkID, "security_list")
	if err == nil && existing != nil && existing.ResourceOCID != "" {
		secLists, lerr := s.oci.ListSecurityListsByVCN(ctx, region, compartmentID, vcnID)
		if lerr == nil {
			for _, sl := range secLists {
				if sl.Id != nil && *sl.Id == existing.ResourceOCID &&
					sl.LifecycleState == core.SecurityListLifecycleStateAvailable {
					return existing.ResourceOCID, nil
				}
			}
		}
	}
	ocid, cerr := s.oci.CreateSecurityList(ctx, region, compartmentID, vcnID, displayName, networkID)
	if cerr != nil {
		return "", cerr
	}
	if _, derr := s.networkResourceRepo.Create(ctx, networkID, "security_list", ocid); derr != nil {
		s.log.Warn("network_resource_track_failed", "network_id", networkID, "type", "security_list", "error", derr)
	}
	if uerr := s.networkResourceRepo.UpdateStatus(ctx, ocid, "available"); uerr != nil {
		s.log.Warn("network_resource_status_update_failed", "ocid", ocid, "error", uerr)
	}
	return ocid, nil
}

func (s *NetworkService) createOrGetSubnet(ctx context.Context, networkID int64, region, compartmentID, vcnID, displayName, cidrBlock, dnsLabel, secListID string) (string, error) {
	existing, err := s.networkResourceRepo.GetByNetworkAndType(ctx, networkID, "subnet")
	if err == nil && existing != nil && existing.ResourceOCID != "" {
		// Verify subnet exists via list (OCI doesn't have a simple "is available" check without Get, but we don't have GetSubnet on OCIComputeService...)
		// We can try GetVCN and check the subnet is still referenced, but simplest: just trust if tracked
		return existing.ResourceOCID, nil
	}
	ocid, cerr := s.oci.CreateSubnet(ctx, region, compartmentID, vcnID, displayName, cidrBlock, dnsLabel, secListID, networkID)
	if cerr != nil {
		return "", cerr
	}
	if _, derr := s.networkResourceRepo.Create(ctx, networkID, "subnet", ocid); derr != nil {
		s.log.Warn("network_resource_track_failed", "network_id", networkID, "type", "subnet", "error", derr)
	}
	if uerr := s.networkResourceRepo.UpdateStatus(ctx, ocid, "available"); uerr != nil {
		s.log.Warn("network_resource_status_update_failed", "ocid", ocid, "error", uerr)
	}
	return ocid, nil
}

func (s *NetworkService) ProvisionNetwork(ctx context.Context, networkID int64) error {
	channel := fmt.Sprintf("network:%d", networkID)

	s.log.Debug("provision_network_start", "network_id", networkID, "channel", channel)

	emitStatus := func(step, message string) {
		s.broker.Publish(channel, sse.SSEEvent{
			Type:    "status",
			Status:  "provisioning",
			Step:    step,
			Message: message,
		})
	}

	emitError := func(message string) {
		s.broker.Publish(channel, sse.SSEEvent{
			Type:    "error",
			Message: message,
		})
	}

	network, err := s.networkRepo.Get(ctx, networkID)
	if err != nil || network == nil {
		if network == nil {
			s.log.Error("network_not_found", "network_id", networkID)
			emitError("Network not found")
			return fmt.Errorf("network %d not found", networkID)
		}
		s.log.Error("get_network_failed", "network_id", networkID, "error", err)
		emitError("Failed to load network: " + err.Error())
		return fmt.Errorf("get network: %w", err)
	}

	s.log.Debug("network_loaded", "network_id", networkID, "name", network.Name, "region", network.Region,
		"status", network.Status, "cidr_vcn", network.CIDRVCN, "cidr_subnet", network.CIDRSubnet)

	if network.Status == "provisioning" {
		emitError("Network is already provisioning")
		return fmt.Errorf("network %d is already provisioning", networkID)
	}

	if err := s.networkRepo.UpdateStatus(ctx, networkID, "provisioning"); err != nil {
		emitError("Failed to update network status: " + err.Error())
		return fmt.Errorf("update status: %w", err)
	}

	_ = s.networkRepo.UpdateProvisioningState(ctx, networkID, string(StateCreatingVCN))
	emitStatus(string(StateCreatingVCN), "Creating VCN")

	settings, err := s.settingsRepo.Get(ctx)
	if err != nil {
		s.log.Error("load_settings_failed", "error", err)
		s.failNetwork(ctx, networkID, channel, "Failed to load settings: "+err.Error())
		return fmt.Errorf("load settings: %w", err)
	}

	if settings.TenancyOCID == "" || settings.UserOCID == "" || settings.Fingerprint == "" ||
		settings.PrivateKey == "" || settings.Region == "" || settings.CompartmentOCID == "" {
		s.log.Error("incomplete_oci_settings",
			"has_tenancy", settings.TenancyOCID != "",
			"has_user", settings.UserOCID != "",
			"has_fingerprint", settings.Fingerprint != "",
			"has_private_key", settings.PrivateKey != "",
			"has_region", settings.Region != "",
			"has_compartment", settings.CompartmentOCID != "")
		s.failNetwork(ctx, networkID, channel, "Missing OCI credentials in settings")
		return fmt.Errorf("incomplete OCI settings")
	}

	s.log.Debug("oci_settings_loaded", "region", settings.Region,
		"compartment_ocid", maskOCID(settings.CompartmentOCID),
		"tenancy_ocid", maskOCID(settings.TenancyOCID))

	region := settings.Region
	compartment := settings.CompartmentOCID

	vcnOCID, err := s.createOrGetVCN(ctx, networkID, region, compartment, network.Name, network.CIDRVCN, safeDNSLabel(network.Name))
	if err != nil {
		s.log.Error("create_vcn_failed", "network_id", networkID, "error", err)
		emitError("Failed to create VCN: " + err.Error())
		s.RollbackNetwork(ctx, networkID, region)
		return fmt.Errorf("create vcn: %w", err)
	}
	s.log.Debug("vcn_created", "vcn_ocid", vcnOCID)

	_ = s.networkRepo.UpdateProvisioningState(ctx, networkID, string(StateCreatingIGW))
	emitStatus(string(StateCreatingIGW), "Creating Internet Gateway")

	igwID, err := s.createOrGetIGW(ctx, networkID, region, compartment, vcnOCID, network.Name+"-igw")
	if err != nil {
		s.log.Error("create_igw_failed", "network_id", networkID, "error", err)
		emitError("Failed to create Internet Gateway: " + err.Error())
		s.RollbackNetwork(ctx, networkID, region)
		return fmt.Errorf("create igw: %w", err)
	}
	s.log.Debug("igw_created", "igw_ocid", igwID)

	_ = s.networkRepo.UpdateProvisioningState(ctx, networkID, string(StateUpdatingRoute))
	emitStatus(string(StateUpdatingRoute), "Updating route table")

	vcn, _ := s.oci.GetVCN(ctx, region, vcnOCID)
	if vcn.DefaultRouteTableId == nil {
		s.log.Error("no_default_route_table", "vcn_ocid", vcnOCID)
		emitError("VCN has no default route table")
		s.RollbackNetwork(ctx, networkID, region)
		return fmt.Errorf("vcn %s has no default route table", maskOCID(vcnOCID))
	}
	rtID := *vcn.DefaultRouteTableId

	rt, err := s.oci.GetRouteTable(ctx, region, rtID)
	if err != nil {
		s.log.Error("get_route_table_failed", "network_id", networkID, "error", err)
		emitError("Failed to get route table: " + err.Error())
		s.RollbackNetwork(ctx, networkID, region)
		return fmt.Errorf("get route table: %w", err)
	}

	rules := append(rt.RouteRules, core.RouteRule{
		Destination:     common.String("0.0.0.0/0"),
		DestinationType: core.RouteRuleDestinationTypeCidrBlock,
		NetworkEntityId: common.String(igwID),
	})
	if err := s.oci.UpdateRouteTable(ctx, region, rtID, rules); err != nil {
		s.log.Error("update_route_table_failed", "network_id", networkID, "error", err)
		emitError("Failed to update route table: " + err.Error())
		s.RollbackNetwork(ctx, networkID, region)
		return fmt.Errorf("update route table: %w", err)
	}

	_ = s.networkRepo.UpdateProvisioningState(ctx, networkID, string(StateCreatingSecList))
	emitStatus(string(StateCreatingSecList), "Creating security list")

	secListID, err := s.createOrGetSecurityList(ctx, networkID, region, compartment, vcnOCID, network.Name+"-public-sl")
	if err != nil {
		s.log.Error("create_security_list_failed", "network_id", networkID, "error", err)
		emitError("Failed to create security list: " + err.Error())
		s.RollbackNetwork(ctx, networkID, region)
		return fmt.Errorf("create security list: %w", err)
	}

	_ = s.networkRepo.UpdateProvisioningState(ctx, networkID, string(StateCreatingSubnet))
	emitStatus(string(StateCreatingSubnet), "Creating subnet")

	subnetOCID, err := s.createOrGetSubnet(ctx, networkID, region, compartment, vcnOCID,
		network.Name+"-subnet", network.CIDRSubnet, safeDNSLabel(network.Name+"-sub"), secListID)
	if err != nil {
		s.log.Error("create_subnet_failed", "network_id", networkID, "error", err)
		emitError("Failed to create subnet: " + err.Error())
		s.RollbackNetwork(ctx, networkID, region)
		return fmt.Errorf("create subnet: %w", err)
	}

	if err := s.networkRepo.UpdateProvisionResult(ctx, networkID, vcnOCID, subnetOCID); err != nil {
		emitError("Failed to update network: " + err.Error())
		return fmt.Errorf("update network: %w", err)
	}

	_ = s.networkRepo.UpdateProvisioningState(ctx, networkID, string(StateReady))
	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "status",
		Status:  "ready",
		Step:    string(StateReady),
		Message: "Network infrastructure provisioned",
		Data: map[string]string{
			"vcn_ocid":    vcnOCID,
			"subnet_ocid": subnetOCID,
		},
	})

	return nil
}

func (s *NetworkService) RollbackNetwork(ctx context.Context, networkID int64, region string) {
	if err := s.networkRepo.UpdateProvisioningState(ctx, networkID, string(StateRollingBack)); err != nil {
		s.log.Warn("network_state_update_failed", "network_id", networkID, "state", StateRollingBack, "error", err)
	}
	resources, err := s.networkResourceRepo.ListByNetwork(ctx, networkID)
	if err != nil {
		s.log.Error("network_resource_list_failed", "network_id", networkID, "error", err)
		return
	}
	for i := len(resources) - 1; i >= 0; i-- {
		r := resources[i]
		var derr error
		switch r.ResourceType {
		case "subnet":
			derr = s.oci.DeleteSubnet(ctx, region, r.ResourceOCID)
		case "security_list":
			derr = s.oci.DeleteSecurityList(ctx, region, r.ResourceOCID)
		case "igw":
			derr = s.oci.DeleteInternetGateway(ctx, region, r.ResourceOCID)
		case "vcn":
			derr = s.oci.DeleteVCN(ctx, region, r.ResourceOCID)
		default:
			s.log.Warn("unknown_resource_type_rollback", "type", r.ResourceType)
			continue
		}
		if derr != nil {
			s.log.Error("resource_rollback_failed", "type", r.ResourceType, "ocid", r.ResourceOCID, "error", derr)
		}
		if merr := s.networkResourceRepo.MarkDeleted(ctx, r.ResourceOCID); merr != nil {
			s.log.Warn("resource_mark_deleted_failed", "ocid", r.ResourceOCID, "error", merr)
		}
	}
	if err := s.networkRepo.UpdateStatus(ctx, networkID, "failed"); err != nil {
		s.log.Error("network_status_failed_update_failed", "network_id", networkID, "error", err)
	}
	if err := s.networkRepo.UpdateProvisioningState(ctx, networkID, string(StateFailed)); err != nil {
		s.log.Error("network_state_failed_update_failed", "network_id", networkID, "error", err)
	}
}

func (s *NetworkService) failNetwork(ctx context.Context, networkID int64, channel, message string) {
	_ = s.networkRepo.UpdateStatus(ctx, networkID, "failed")
	_ = s.networkRepo.UpdateProvisioningState(ctx, networkID, string(StateFailed))
	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "error",
		Message: message,
	})
}

func (s *NetworkService) DestroyNetwork(ctx context.Context, networkID int64) error {
	channel := fmt.Sprintf("network:%d", networkID)
	s.log.Debug("destroy_network_start", "network_id", networkID)

	network, err := s.networkRepo.Get(ctx, networkID)
	if err != nil || network == nil {
		if network == nil {
			return fmt.Errorf("network %d not found", networkID)
		}
		return fmt.Errorf("get network: %w", err)
	}

	prevStatus := network.Status

	_ = s.networkRepo.UpdateStatus(ctx, networkID, "destroying")
	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "status",
		Status:  "destroying",
		Step:    "destroying",
		Message: "Destroying network infrastructure",
	})

	settings, err := s.settingsRepo.Get(ctx)
	if err != nil {
		s.log.Error("destroy_load_settings_failed", "network_id", networkID, "error", err)
		_ = s.networkRepo.UpdateStatus(ctx, networkID, prevStatus)
		return fmt.Errorf("load settings: %w", err)
	}

	region := settings.Region
	compartment := settings.CompartmentOCID
	var errs []string

	// Step 1: Clear ALL route rules from the default route table.
	// This must happen BEFORE deleting IGWs and BEFORE deleting the subnet.
	// OCI rejects IGW deletion (409 Conflict) if any route rule still
	// references the IGW, and the subnet must not be terminating while we
	// update the route table.
	if network.VCNOCID != "" {
		vcn, verr := s.oci.GetVCN(ctx, region, network.VCNOCID)
		if verr != nil {
			s.log.Error("destroy_get_vcn_failed", "network_id", networkID, "error", verr)
			errs = append(errs, "get VCN: "+verr.Error())
		}

		// Step 1: Remove route rules that reference managed IGWs.
		// Must happen BEFORE deleting IGWs (OCI 409 Conflict otherwise).
		// Keep other routes intact — OCI won't allow an empty route table
		// while a subnet is still associated.
		if verr == nil && vcn.DefaultRouteTableId != nil {
			rtID := *vcn.DefaultRouteTableId
			rt, rerr := s.oci.GetRouteTable(ctx, region, rtID)
			if rerr != nil {
				s.log.Error("destroy_get_rt_failed", "network_id", networkID, "error", rerr)
				errs = append(errs, "get route table: "+rerr.Error())
			} else {
				igws, _ := s.oci.ListInternetGatewaysByVCN(ctx, region, compartment, network.VCNOCID)
				managedIGW := make(map[string]bool)
				for _, igw := range igws {
					if igw.Id != nil && igw.FreeformTags != nil && igw.FreeformTags["provessor:managed"] == "true" {
						managedIGW[*igw.Id] = true
						s.log.Debug("found_managed_igw", "network_id", networkID, "igw_ocid", maskOCID(*igw.Id))
					}
				}
				var filteredRules []core.RouteRule
				for _, rule := range rt.RouteRules {
					if rule.NetworkEntityId != nil && managedIGW[*rule.NetworkEntityId] {
						s.log.Debug("removing_igw_route", "network_id", networkID, "target", maskOCID(*rule.NetworkEntityId))
						continue
					}
					filteredRules = append(filteredRules, rule)
				}
				if len(filteredRules) != len(rt.RouteRules) {
					if filteredRules == nil {
						filteredRules = []core.RouteRule{}
					}
					s.log.Debug("updating_route_table", "network_id", networkID, "removed", len(rt.RouteRules)-len(filteredRules))
					if uerr := s.oci.UpdateRouteTable(ctx, region, rtID, filteredRules); uerr != nil {
						s.log.Error("remove_igw_routes_failed", "network_id", networkID, "error", uerr)
						errs = append(errs, "remove IGW routes: "+uerr.Error())
					} else {
						s.log.Debug("waiting_route_table_propagate", "network_id", networkID)
						if werr := s.oci.waitForRouteTableCleared(ctx, region, rtID, managedIGW, 30*time.Second); werr != nil {
							s.log.Error("wait_route_table_failed", "network_id", networkID, "error", werr)
							errs = append(errs, "wait for route table: "+werr.Error())
						}
					}
				}
			}
		}

		// Step 2: Delete managed internet gateways
		if len(errs) == 0 {
			igws, lerr := s.oci.ListInternetGatewaysByVCN(ctx, region, compartment, network.VCNOCID)
			if lerr != nil {
				s.log.Error("list_igws_failed", "network_id", networkID, "error", lerr)
				errs = append(errs, "list IGWs: "+lerr.Error())
			} else {
				for _, igw := range igws {
					if igw.FreeformTags != nil && igw.FreeformTags["provessor:managed"] == "true" {
						s.log.Debug("destroying_igw", "network_id", networkID, "igw_ocid", maskOCID(*igw.Id))
						if derr := s.oci.DeleteInternetGateway(ctx, region, *igw.Id); derr != nil {
							if isNotFound(derr) {
								s.log.Debug("igw_already_deleted", "network_id", networkID, "igw_ocid", maskOCID(*igw.Id))
							} else {
								s.log.Error("destroy_igw_failed", "network_id", networkID, "igw_ocid", maskOCID(*igw.Id), "error", derr)
								errs = append(errs, "delete IGW: "+derr.Error())
							}
						}
					}
				}
			}
		}

		// Step 3: Delete subnet (now that route table is clean and IGW is gone)
		if network.SubnetOCID != "" && len(errs) == 0 {
			s.log.Debug("destroying_subnet", "network_id", networkID, "subnet_ocid", maskOCID(network.SubnetOCID))
			if err := s.oci.DeleteSubnet(ctx, region, network.SubnetOCID); err != nil {
				if isNotFound(err) {
					s.log.Debug("subnet_already_deleted", "network_id", networkID, "subnet_ocid", maskOCID(network.SubnetOCID))
				} else {
					s.log.Error("destroy_subnet_failed", "network_id", networkID, "error", err)
					errs = append(errs, "delete subnet: "+err.Error())
				}
			}
			if len(errs) == 0 {
				if werr := s.oci.WaitForSubnetTerminated(ctx, region, network.SubnetOCID, 2*time.Minute); werr != nil {
					if !isNotFound(werr) {
						s.log.Error("wait_subnet_terminated_failed", "network_id", networkID, "error", werr)
						errs = append(errs, "wait for subnet termination: "+werr.Error())
					}
				}
			}
		}

		// Step 4: Delete managed security lists — skip the VCN's default
		// security list (OCI rejects deletes on the default SL while the VCN is alive).
		if len(errs) == 0 {
			defaultSLID := ""
			if vcn.DefaultSecurityListId != nil {
				defaultSLID = *vcn.DefaultSecurityListId
			}
			secLists, lerr := s.oci.ListSecurityListsByVCN(ctx, region, compartment, network.VCNOCID)
			if lerr != nil {
				s.log.Error("list_sec_lists_failed", "network_id", networkID, "error", lerr)
				errs = append(errs, "list security lists: "+lerr.Error())
			} else {
				for _, sl := range secLists {
					isDefault := sl.Id != nil && *sl.Id == defaultSLID
					if isDefault {
						s.log.Debug("skipping_default_security_list", "network_id", networkID, "sl_ocid", maskOCID(*sl.Id))
						continue
					}
					if sl.FreeformTags != nil && sl.FreeformTags["provessor:managed"] == "true" {
						s.log.Debug("destroying_security_list", "network_id", networkID, "sl_ocid", maskOCID(*sl.Id))
						if derr := s.oci.DeleteSecurityList(ctx, region, *sl.Id); derr != nil {
							s.log.Error("destroy_security_list_failed", "network_id", networkID, "sl_ocid", maskOCID(*sl.Id), "error", derr)
							errs = append(errs, "delete security list: "+derr.Error())
						}
					}
				}
			}
		}

		// Step 5: Delete VCN — by now all dependencies are removed.
		if len(errs) == 0 {
			s.log.Debug("destroying_vcn", "network_id", networkID, "vcn_ocid", maskOCID(network.VCNOCID))
			if err := s.oci.DeleteVCN(ctx, region, network.VCNOCID); err != nil {
				s.log.Error("destroy_vcn_failed", "network_id", networkID, "error", err)
				errs = append(errs, "delete VCN: "+err.Error())
			}
		}
	}

	// If any OCI deletion failed, revert the status and return an error without
	// touching the database record. The user can retry after fixing the issue.
	if len(errs) > 0 {
		_ = s.networkRepo.UpdateStatus(ctx, networkID, prevStatus)
		fullErr := strings.Join(errs, "; ")
		s.log.Error("destroy_network_partial_failure", "network_id", networkID, "errors", fullErr)
		s.broker.Publish(channel, sse.SSEEvent{
			Type:    "error",
			Status:  "destroy_failed",
			Message: "Network destruction failed: " + fullErr,
		})
		return fmt.Errorf("destroy network %d: %s", networkID, fullErr)
	}

	// All OCI resources destroyed successfully — now delete the DB record.
	if err := s.networkRepo.Delete(ctx, networkID); err != nil {
		s.log.Error("destroy_network_db_delete_failed", "network_id", networkID, "error", err)
		_ = s.networkRepo.UpdateStatus(ctx, networkID, prevStatus)
		return fmt.Errorf("delete network record: %w", err)
	}

	s.broker.Publish(channel, sse.SSEEvent{
		Type:    "status",
		Status:  "destroyed",
		Step:    "destroyed",
		Message: "Network infrastructure destroyed",
	})

	return nil
}

func safeDNSLabel(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name) && len(result) < 15; i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			result = append(result, c)
		} else if c >= 'A' && c <= 'Z' {
			result = append(result, c+32)
		}
	}
	if len(result) == 0 {
		return "network"
	}
	if result[0] < 'a' || result[0] > 'z' {
		return "n" + string(result)
	}
	return string(result)
}

func maskOCID(ocid string) string {
	if len(ocid) <= 20 {
		return "***"
	}
	return ocid[:10] + "..." + ocid[len(ocid)-10:]
}

func isNotFound(err error) bool {
	var se common.ServiceError
	if errors.As(err, &se) {
		return se.GetHTTPStatusCode() == 404
	}
	return false
}
