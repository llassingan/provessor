const API_BASE = import.meta.env.VITE_API_BASE_URL as string | undefined ?? '/api';

interface ApiError {
  error?: string;
}

interface CSRFResponse {
  csrf_token: string;
}

let csrfToken: string | null = null;
let csrfTokenRequest: Promise<string> | null = null;

function requestMethod(options?: RequestInit): string {
  return (options?.method ?? "GET").toUpperCase();
}

function isUnsafeMethod(method: string): boolean {
  return !["GET", "HEAD", "OPTIONS", "TRACE"].includes(method);
}

async function getCSRFToken(): Promise<string> {
  if (csrfToken !== null) {
    return csrfToken;
  }
  if (csrfTokenRequest === null) {
    csrfTokenRequest = fetch(`${API_BASE}/auth/csrf`, { credentials: "include" })
      .then((res) => {
        if (!res.ok) {
          throw new Error("Failed to fetch CSRF token");
        }
        return res.json() as Promise<CSRFResponse>;
      })
      .then((data) => {
        csrfToken = data.csrf_token;
        return data.csrf_token;
      })
      .finally(() => {
        csrfTokenRequest = null;
      });
  }
  return csrfTokenRequest;
}

async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const method = requestMethod(options);
  const headers = new Headers(options?.headers);
  if (!headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (isUnsafeMethod(method) && !headers.has("X-CSRF-Token")) {
    headers.set("X-CSRF-Token", await getCSRFToken());
  }

  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    method,
    credentials: "include",
    headers,
  });

  if (res.status === 401) {
    throw new Error("Unauthorized");
  }

  if (res.status === 423) {
    const err = (await res.json().catch(() => ({ error: "Account locked" }))) as ApiError;
    throw new Error(err.error ?? "Account locked");
  }

  if (!res.ok) {
    const err = (await res.json().catch(() => ({ error: res.statusText }))) as ApiError;
    throw new Error(err.error ?? `HTTP ${String(res.status)}`);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  return (await res.json()) as T;
}

export interface User {
  id: number;
  email: string;
  created_at: string;
}

export interface Network {
  id: number;
  name: string;
  region: string;
  cidr_vcn: string;
  cidr_subnet: string;
  vcn_ocid: string;
  subnet_ocid: string;
  status: "pending" | "provisioning" | "ready" | "failed";
  created_at: string;
  updated_at: string;
}

export interface NetworkListResponse {
  networks: Network[];
  max_networks: number;
}

export interface RegionItem {
  key: string;
  name: string;
}

export interface RegionGroup {
  group: string;
  items: RegionItem[];
}

export interface Template {
  id: number;
  name: string;
  description: string;
  type: "predefined" | "custom";
  logo_url?: string;
  shape: string;
  default_ocpu: number;
  default_memory: number;
  boot_volume_size_gb: number;
}

export type VPSStatus =
  | "pending"
  | "provisioning"
  | "running"
  | "stopped"
  | "failed"
  | "terminating"
  | "terminated";

export interface VPS {
  id: number;
  display_name: string;
  template_id: number;
  network_id: number | null;
  shape: string;
  ocpu: number;
  memory_gb: number;
  boot_volume_size_gb: number;
  oci_instance_id?: string;
  public_ip?: string;
  private_ip?: string;
  status: VPSStatus;
  initial_credentials?: string;
  ssh_username?: string;
  ssh_password?: string;
  created_at: string;
  updated_at: string;
}

export interface FirewallRule {
  port: number;
  name: string;
  description: string;
  direction: "ingress" | "egress";
  source?: string;
  destination?: string;
}

export interface FirewallRules {
  ingress: FirewallRule[];
  egress: FirewallRule[];
}

export interface Settings {
  id: number;
  tenancy_ocid: string;
  user_ocid: string;
  fingerprint: string;
  private_key: string;
  region: string;
  compartment_ocid: string;
  api_base_url: string;
}

export interface CreateVPSRequest {
  template_id: number;
  network_id: number;
  display_name: string;
  shape?: string;
  ocpu?: number;
  memory_gb?: number;
  boot_volume_size_gb?: number;
  custom_playbook_yaml?: string;
}

export interface UpdateSettingsRequest {
  tenancy_ocid: string;
  user_ocid: string;
  fingerprint: string;
  private_key: string;
  region: string;
  compartment_ocid: string;
  api_base_url: string;
  api_token: string;
}

export const auth = {
  signup(email: string, password: string): Promise<User> {
    return apiFetch<User>("/auth/signup", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
  },

  login(email: string, password: string): Promise<User> {
    return apiFetch<User>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
  },

  logout(): Promise<undefined> {
    return apiFetch<undefined>("/auth/logout", { method: "POST" });
  },
};

export const vps = {
  list(): Promise<VPS[]> {
    return apiFetch<VPS[]>("/vps");
  },

  get(id: number): Promise<VPS> {
    return apiFetch<VPS>(`/vps/${String(id)}`);
  },

  create(req: CreateVPSRequest): Promise<VPS> {
    return apiFetch<VPS>("/vps", {
      method: "POST",
      body: JSON.stringify(req),
    });
  },

  delete(id: number): Promise<undefined> {
    return apiFetch<undefined>(`/vps/${String(id)}`, { method: "DELETE" });
  },

  terminate(id: number): Promise<{ status: string }> {
    return apiFetch<{ status: string }>(`/vps/${String(id)}/terminate`, { method: "POST" });
  },

  action(id: number, action: "start" | "stop"): Promise<VPS> {
    return apiFetch<VPS>(`/vps/${String(id)}/${action}`, { method: "POST" });
  },

  restart(id: number): Promise<VPS> {
    return apiFetch<VPS>(`/vps/${String(id)}/restart`, { method: "POST" });
  },

  reset(id: number): Promise<VPS> {
    return apiFetch<VPS>(`/vps/${String(id)}/reset`, { method: "POST" });
  },

  resetPassword(id: number, password: string): Promise<VPS> {
    return apiFetch<VPS>(`/vps/${String(id)}/reset-password`, {
      method: "POST",
      body: JSON.stringify({ password }),
    });
  },

  refreshIPs(id: number): Promise<VPS> {
    return apiFetch<VPS>(`/vps/${String(id)}/refresh-ips`, { method: "POST" });
  },

  getFirewall(id: number): Promise<FirewallRules> {
    return apiFetch<FirewallRules>(`/vps/${String(id)}/firewall`);
  },

  updateFirewall(id: number, rules: FirewallRule[]): Promise<FirewallRules> {
    return apiFetch<FirewallRules>(`/vps/${String(id)}/firewall`, {
      method: "POST",
      body: JSON.stringify({ rules }),
    });
  },
};

export interface ShapeSpec {
  name: string;
  processor: string;
  min_ocpu: number;
  max_ocpu: number;
  min_memory: number;
  max_memory: number;
  max_network: string;
  description: string;
}

export interface ShapeGroup {
  label: string;
  description: string;
  shapes: ShapeSpec[];
}

export const shapes = {
  groups(): Promise<ShapeGroup[]> {
    return apiFetch<ShapeGroup[]>("/shapes");
  },
};

export const templates = {
  list(): Promise<Template[]> {
    return apiFetch<Template[]>("/templates");
  },

  create(req: {
    name: string;
    description: string;
    shape: string;
    default_ocpu: number;
    default_memory: number;
    boot_volume_size_gb: number;
    cloud_init_yaml: string;
    logo_url?: string;
  }): Promise<Template> {
    return apiFetch<Template>("/templates", {
      method: "POST",
      body: JSON.stringify(req),
    });
  },
};

export const settings = {
  get(): Promise<Settings> {
    return apiFetch<Settings>("/settings");
  },

  update(req: UpdateSettingsRequest): Promise<Settings> {
    return apiFetch<Settings>("/settings", {
      method: "PUT",
      body: JSON.stringify(req),
    });
  },
};

export const regions = {
  groups(): Promise<RegionGroup[]> {
    return apiFetch<RegionGroup[]>("/regions");
  },
};

export const networks = {
  list(): Promise<NetworkListResponse> {
    return apiFetch<NetworkListResponse>("/networks");
  },

  get(id: number): Promise<Network> {
    return apiFetch<Network>(`/networks/${String(id)}`);
  },

  create(name: string, region: string): Promise<Network> {
    return apiFetch<Network>("/networks", {
      method: "POST",
      body: JSON.stringify({ name, region }),
    });
  },

  delete(id: number): Promise<undefined> {
    return apiFetch<undefined>(`/networks/${String(id)}`, { method: "DELETE" });
  },

  provision(id: number): Promise<{ status: string }> {
    return apiFetch<{ status: string }>(`/networks/${String(id)}/provision`, {
      method: "POST",
    });
  },
};
