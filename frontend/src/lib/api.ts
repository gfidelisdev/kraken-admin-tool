import {
  DockerPullMessage,
  ServerDiagnosticLog,
  TestServerPayload,
  TestServerResult,
  ComposeStack,
  DeployStackRequest,
  HostPackageStatus,
  PackageUpgradeLog,
  PackageUpgradeResult,
} from "@/types";

const TOKEN_KEY = "kraken_auth_token";
const ACTIVE_SERVER_KEY = "kraken_active_server_id";

export class ApiError extends Error {
  status: number;
  data: unknown;

  constructor(message: string, status: number, data?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.data = data;
  }
}

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  if (typeof window === "undefined") return;
  localStorage.setItem(TOKEN_KEY, token);
}

export function removeToken(): void {
  if (typeof window === "undefined") return;
  localStorage.removeItem(TOKEN_KEY);
}

export function getActiveServerId(): number | null {
  if (typeof window === "undefined") return null;
  const raw = localStorage.getItem(ACTIVE_SERVER_KEY);
  if (!raw) return null;
  const id = parseInt(raw, 10);
  return isNaN(id) ? null : id;
}

export function setActiveServerId(id: number | null): void {
  if (typeof window === "undefined") return;
  if (id === null) {
    localStorage.removeItem(ACTIVE_SERVER_KEY);
  } else {
    localStorage.setItem(ACTIVE_SERVER_KEY, id.toString());
  }
}

export function getBaseApiUrl(): string {
  if (process.env.NEXT_PUBLIC_API_URL) {
    return process.env.NEXT_PUBLIC_API_URL.replace(/\/$/, "");
  }
  if (typeof window !== "undefined") {
    if (window.location.hostname === "localhost" || window.location.hostname === "127.0.0.1") {
      return "http://localhost:8080";
    }
  }
  return "";
}

async function request<T>(
  endpoint: string,
  options: RequestInit = {}
): Promise<T> {
  const token = getToken();
  const activeServerId = getActiveServerId();
  const headers = new Headers(options.headers || {});

  if (!headers.has("Content-Type") && !(options.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }

  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  if (activeServerId && !headers.has("X-Server-ID")) {
    headers.set("X-Server-ID", activeServerId.toString());
  }

  // Ensure leading slash
  const cleanEndpoint = endpoint.startsWith("/") ? endpoint : `/${endpoint}`;
  const baseUrl = getBaseApiUrl();
  const url = `${baseUrl}${cleanEndpoint}`;

  const config: RequestInit = {
    ...options,
    headers,
  };

  try {
    const response = await fetch(url, config);

    if (response.status === 401) {
      // Do not treat host-level operations (like sudo password prompts) as JWT authentication logout
      const isHostEndpoint = cleanEndpoint.startsWith("/api/hosts/");
      if (!isHostEndpoint && typeof window !== "undefined" && !window.location.pathname.startsWith("/login")) {
        removeToken();
        window.location.href = "/login";
      }
    }

    let responseData: any = null;
    const contentType = response.headers.get("content-type");
    if (contentType && contentType.includes("application/json")) {
      responseData = await response.json().catch(() => null);
    } else {
      responseData = await response.text().catch(() => null);
    }

    if (!response.ok) {
      const errorMessage =
        (typeof responseData === "object" && responseData?.error) ||
        (typeof responseData === "object" && responseData?.message) ||
        (typeof responseData === "string" && responseData) ||
        `Request failed with status ${response.status}: ${response.statusText}`;

      throw new ApiError(errorMessage, response.status, responseData);
    }

    // Backend list endpoints respond with a `{ "data": ... }` envelope; unwrap it
    // when it's the only key so callers receive the payload directly.
    if (
      responseData &&
      typeof responseData === "object" &&
      !Array.isArray(responseData) &&
      Object.keys(responseData).length === 1 &&
      "data" in responseData
    ) {
      return responseData.data as T;
    }

    return responseData as T;
  } catch (error) {
    if (error instanceof ApiError) {
      throw error;
    }
    throw new ApiError(
      error instanceof Error ? error.message : "Network error or connection refused",
      0
    );
  }
}

export const api = {
  get: <T>(url: string, options?: RequestInit) =>
    request<T>(url, { ...options, method: "GET" }),

  post: <T>(url: string, data?: unknown, options?: RequestInit) =>
    request<T>(url, {
      ...options,
      method: "POST",
      body: data !== undefined ? JSON.stringify(data) : undefined,
    }),

  put: <T>(url: string, data?: unknown, options?: RequestInit) =>
    request<T>(url, {
      ...options,
      method: "PUT",
      body: data !== undefined ? JSON.stringify(data) : undefined,
    }),

  delete: <T>(url: string, options?: RequestInit) =>
    request<T>(url, { ...options, method: "DELETE" }),
};

export async function pullImageStream(
  imageName: string,
  onMessage: (msg: DockerPullMessage) => void,
  signal?: AbortSignal
): Promise<void> {
  const baseUrl = getBaseApiUrl();
  const token = getToken();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }
  const activeServerId = getActiveServerId();
  if (activeServerId) {
    headers["X-Server-ID"] = activeServerId.toString();
  }

  const response = await fetch(`${baseUrl}/api/docker/images/pull/stream`, {
    method: "POST",
    headers,
    body: JSON.stringify({ image: imageName }),
    signal,
  });

  if (!response.ok) {
    let errText = `Failed to pull image (${response.status})`;
    try {
      const errJson = await response.json();
      errText = errJson.error || errJson.message || errText;
    } catch {
      // ignore
    }
    throw new ApiError(errText, response.status);
  }

  const reader = response.body?.getReader();
  if (!reader) {
    throw new ApiError("Streaming unsupported by browser", 0);
  }

  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n");
    buffer = lines.pop() || "";

    for (const line of lines) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        const parsed = JSON.parse(trimmed) as DockerPullMessage;
        onMessage(parsed);
        if (parsed.error) {
          throw new ApiError(parsed.error, 500, parsed);
        }
      } catch (e) {
        if (e instanceof ApiError) throw e;
      }
    }
  }

  if (buffer.trim()) {
    try {
      const parsed = JSON.parse(buffer.trim()) as DockerPullMessage;
      onMessage(parsed);
      if (parsed.error) {
        throw new ApiError(parsed.error, 500, parsed);
      }
    } catch (e) {
      if (e instanceof ApiError) throw e;
    }
  }
}

export async function testServerStream(
  payload: TestServerPayload,
  onLog: (log: ServerDiagnosticLog) => void,
  signal?: AbortSignal
): Promise<TestServerResult> {
  const baseUrl = getBaseApiUrl();
  const token = getToken();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const response = await fetch(`${baseUrl}/api/docker/servers/test/stream`, {
    method: "POST",
    headers,
    body: JSON.stringify(payload),
    signal,
  });

  if (!response.ok) {
    let errText = `Failed to test server connection (${response.status})`;
    try {
      const errJson = await response.json();
      errText = errJson.error || errJson.message || errText;
    } catch {
      // ignore
    }
    throw new ApiError(errText, response.status);
  }

  const reader = response.body?.getReader();
  if (!reader) {
    throw new ApiError("Streaming unsupported by browser", 0);
  }

  const decoder = new TextDecoder();
  let buffer = "";
  let finalResult: TestServerResult = {
    status: "UNREACHABLE",
    message: "No response received",
  };

  const processLine = (rawLine: string) => {
    const trimmed = rawLine.trim();
    if (!trimmed) return;
    try {
      const parsed = JSON.parse(trimmed);
      if (parsed.type === "result") {
        finalResult = {
          status: parsed.status || "UNREACHABLE",
          version: parsed.version,
          message: parsed.message,
          error: parsed.error,
        };
      } else if (parsed.level && parsed.message) {
        onLog(parsed as ServerDiagnosticLog);
      }
    } catch {
      // ignore
    }
  };

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n");
    buffer = lines.pop() || "";

    for (const line of lines) {
      processLine(line);
    }
  }

  if (buffer.trim()) {
    processLine(buffer.trim());
  }

  return finalResult;
}

export async function getWsTicket(): Promise<string | null> {
  try {
    const res = await request<{ ticket: string }>("/api/auth/ws-ticket", {
      method: "POST",
    });
    return res.ticket || null;
  } catch {
    return null;
  }
}

// Docker Compose Stacks
export async function getComposeStacks(serverId?: number): Promise<ComposeStack[]> {
  const headers: Record<string, string> = {};
  if (serverId) headers["X-Server-ID"] = serverId.toString();
  return api.get<ComposeStack[]>("/api/docker/stacks", { headers });
}

export async function getComposeStack(name: string, serverId?: number): Promise<ComposeStack> {
  const headers: Record<string, string> = {};
  if (serverId) headers["X-Server-ID"] = serverId.toString();
  return api.get<ComposeStack>(`/api/docker/stacks/${name}`, { headers });
}

export async function startComposeStack(name: string, serverId?: number): Promise<{ message: string }> {
  const headers: Record<string, string> = {};
  if (serverId) headers["X-Server-ID"] = serverId.toString();
  return api.post<{ message: string }>(`/api/docker/stacks/${name}/start`, {}, { headers });
}

export async function stopComposeStack(name: string, serverId?: number): Promise<{ message: string }> {
  const headers: Record<string, string> = {};
  if (serverId) headers["X-Server-ID"] = serverId.toString();
  return api.post<{ message: string }>(`/api/docker/stacks/${name}/stop`, {}, { headers });
}

export async function restartComposeStack(name: string, serverId?: number): Promise<{ message: string }> {
  const headers: Record<string, string> = {};
  if (serverId) headers["X-Server-ID"] = serverId.toString();
  return api.post<{ message: string }>(`/api/docker/stacks/${name}/restart`, {}, { headers });
}

export async function removeComposeStack(name: string, serverId?: number, removeVolumes: boolean = false): Promise<{ message: string }> {
  const headers: Record<string, string> = {};
  if (serverId) headers["X-Server-ID"] = serverId.toString();
  return api.delete<{ message: string }>(`/api/docker/stacks/${name}?volumes=${removeVolumes}`, { headers });
}

export async function deployComposeStack(payload: DeployStackRequest, serverId?: number): Promise<{ message: string }> {
  const headers: Record<string, string> = {};
  if (serverId) headers["X-Server-ID"] = serverId.toString();
  return api.post<{ message: string }>("/api/docker/stacks/deploy", payload, { headers });
}

// Host Packages & Sudo Policy
export async function getHostPackages(serverId: number): Promise<HostPackageStatus> {
  return api.get<HostPackageStatus>(`/api/hosts/${serverId}/packages`);
}

export async function updateHostPackageManager(
  serverId: number,
  packageManager: string
): Promise<{ message: string; package_manager: string }> {
  return api.put<{ message: string; package_manager: string }>(`/api/hosts/${serverId}/package-manager`, {
    package_manager: packageManager,
  });
}

export async function checkHostPackages(
  serverId: number,
  sudoPassword?: string,
  packageManager?: string
): Promise<HostPackageStatus> {
  const headers: Record<string, string> = {};
  if (sudoPassword) headers["X-Sudo-Password"] = sudoPassword;
  return api.post<HostPackageStatus>(
    `/api/hosts/${serverId}/packages/check`,
    { sudo_password: sudoPassword, package_manager: packageManager },
    { headers }
  );
}

export async function upgradeHostPackages(
  serverId: number,
  packages?: string[],
  sudoPassword?: string,
  packageManager?: string
): Promise<{ message: string }> {
  const headers: Record<string, string> = {};
  if (sudoPassword) headers["X-Sudo-Password"] = sudoPassword;
  return api.post<{ message: string }>(
    `/api/hosts/${serverId}/packages/upgrade`,
    { packages, sudo_password: sudoPassword, package_manager: packageManager },
    { headers }
  );
}

export async function upgradeHostPackagesStream(
  serverId: number,
  packages?: string[],
  sudoPassword?: string,
  packageManager?: string,
  onLog?: (log: PackageUpgradeLog) => void,
  signal?: AbortSignal
): Promise<PackageUpgradeResult> {
  const baseUrl = getBaseApiUrl();
  const token = getToken();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }
  if (sudoPassword) {
    headers["X-Sudo-Password"] = sudoPassword;
  }

  const response = await fetch(`${baseUrl}/api/hosts/${serverId}/packages/upgrade/stream`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      packages,
      sudo_password: sudoPassword,
      package_manager: packageManager,
    }),
    signal,
  });

  if (!response.ok) {
    let errText = `Falha ao iniciar atualização de pacotes (${response.status})`;
    let errData: any = null;
    try {
      errData = await response.json();
      errText = errData.error || errData.message || errText;
    } catch {
      // ignore
    }
    throw new ApiError(errText, response.status, errData);
  }

  const reader = response.body?.getReader();
  if (!reader) {
    throw new ApiError("Streaming não suportado pelo navegador", 0);
  }

  const decoder = new TextDecoder();
  let buffer = "";
  let finalResult: PackageUpgradeResult = {
    success: false,
    message: "Nenhuma resposta final recebida do servidor",
  };

  const processLine = (rawLine: string) => {
    const trimmed = rawLine.trim();
    if (!trimmed) return;
    try {
      const parsed = JSON.parse(trimmed);
      if (parsed.type === "result") {
        finalResult = {
          success: Boolean(parsed.success),
          message: parsed.message || (parsed.success ? "Atualização concluída com sucesso" : "Falha na atualização"),
          output: parsed.output,
          remaining_updates_count: parsed.remaining_updates_count,
          security_updates_count: parsed.security_updates_count,
          error: parsed.error,
          code: parsed.code,
        };
      } else if (parsed.type === "log" || parsed.message) {
        if (onLog) {
          onLog({
            type: "log",
            message: parsed.message,
            level: parsed.level || "stdout",
            timestamp: parsed.timestamp || new Date().toISOString(),
          });
        }
      }
    } catch {
      if (onLog && trimmed) {
        onLog({
          type: "log",
          message: trimmed,
          level: "stdout",
          timestamp: new Date().toISOString(),
        });
      }
    }
  };

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n");
    buffer = lines.pop() || "";

    for (const line of lines) {
      processLine(line);
    }
  }

  if (buffer.trim()) {
    processLine(buffer.trim());
  }

  return finalResult;
}

export async function updateHostSudoPolicy(serverId: number, sudoPolicy: string, sudoPassword?: string): Promise<{ message: string }> {
  return api.post<{ message: string }>(`/api/hosts/${serverId}/sudo-policy`, { sudo_policy: sudoPolicy, sudo_password: sudoPassword });
}

export async function clearHostSudoPassword(serverId: number): Promise<{ message: string }> {
  return api.delete<{ message: string }>(`/api/hosts/${serverId}/sudo-password`);
}


