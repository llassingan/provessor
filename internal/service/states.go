package service

type NetworkProvisioningState string

const (
	StatePending         NetworkProvisioningState = "pending"
	StateCreatingVCN     NetworkProvisioningState = "creating_vcn"
	StateCreatingSubnet  NetworkProvisioningState = "creating_subnet"
	StateCreatingIGW     NetworkProvisioningState = "creating_igw"
	StateUpdatingRoute   NetworkProvisioningState = "updating_route"
	StateCreatingSecList NetworkProvisioningState = "creating_sec_list"
	StateReady           NetworkProvisioningState = "ready"
	StateFailed          NetworkProvisioningState = "failed"
	StateRollingBack     NetworkProvisioningState = "rolling_back"
)

type VPSProvisioningState string

const (
	StateVPSPending          VPSProvisioningState = "pending"
	StateCreatingInstance    VPSProvisioningState = "creating_instance"
	StateCreatingNSG         VPSProvisioningState = "creating_nsg"
	StateAttachingNSG        VPSProvisioningState = "attaching_nsg"
	StateWaitingRunning      VPSProvisioningState = "waiting_running"
	StateCreatingSSHUser     VPSProvisioningState = "creating_ssh_user"
	StateVPSReady            VPSProvisioningState = "ready"
	StateVPSRestarting       VPSProvisioningState = "restarting"
	StateVPSResetting        VPSProvisioningState = "resetting"
	StateVPSFailed           VPSProvisioningState = "failed"
	StateVPSRollingBack      VPSProvisioningState = "rolling_back"
)
