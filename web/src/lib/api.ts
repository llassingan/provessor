const API_BASE = import.meta.env.VITE_API_BASE_URL as string | undefined ?? '/api';

interface ApiError {
  error?: string;
}

async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
    ...options,
  });

  if (res.status === 401) {
    throw new Error("Unauthorized");
  }

  if (!res.ok) {
    const err = (await res.json().catch(() => ({ error: res.statusText }))) as ApiError;
    throw new Error(err.error ?? `HTTP ${res.status}`);
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
  cidr_vcn: string;
  cidr_subnet: string;
  vcn_ocid: string;
  subnet_ocid: string;
  status: "pending" | "provisioning" | "ready" | "failed";
  created_at: string;
  updated_at: string;
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
  created_at: string;
  updated_at: string;
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

  logout(): Promise<void> {
    return apiFetch<void>("/auth/logout", { method: "POST" });
  },
};

export const vps = {
  list(): Promise<VPS[]> {
    return apiFetch<VPS[]>("/vps");
  },

  get(id: number): Promise<VPS> {
    return apiFetch<VPS>(`/vps/${id}`);
  },

  create(req: CreateVPSRequest): Promise<VPS> {
    return apiFetch<VPS>("/vps", {
      method: "POST",
      body: JSON.stringify(req),
    });
  },

  delete(id: number): Promise<void> {
    return apiFetch<void>(`/vps/${id}`, { method: "DELETE" });
  },

  action(id: number, action: "start" | "stop"): Promise<VPS> {
    return apiFetch<VPS>(`/vps/${id}/${action}`, { method: "POST" });
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

export const networks = {
  list(): Promise<Network[]> {
    return apiFetch<Network[]>("/networks");
  },

  get(id: number): Promise<Network> {
    return apiFetch<Network>(`/networks/${id}`);
  },

  create(name: string): Promise<Network> {
    return apiFetch<Network>("/networks", {
      method: "POST",
      body: JSON.stringify({ name }),
    });
  },

  delete(id: number): Promise<void> {
    return apiFetch<void>(`/networks/${id}`, { method: "DELETE" });
  },

  provision(id: number): Promise<{ status: string }> {
    return apiFetch<{ status: string }>(`/networks/${id}/provision`, {
      method: "POST",
    });
  },
};
