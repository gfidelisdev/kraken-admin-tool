import {
  DockerPullMessage,
  ServerDiagnosticLog,
  TestServerPayload,
  TestServerResult,
  ComposeStack,
  DeployStackRequest,
  ComposeStackYAMLResponse,
  UpdateStackYAMLRequest,
  ImageDockerfileResponse,
  BuildImageRequest,
  DockerBuildMessage,
  HostPackageStatus,
  PackageUpgradeLog,
  PackageUpgradeResult,
  ActiveUpgradeJob,
  OTPSetupResponse,
  RecoverPasswordOTPPayload,
  ProvisionKeyPayload,
  ProvisionKeyResult,
  SourceSearchDir,
  CreateSourceDirRequest,
  UpdateSourceDirRequest,
  SourceLink,
  SourceScanProgress,
  ImageUpdateInfo,
  UpdateImageRequest,
  UpdateImageResult,
  BindSourceRequest,
  UnbindSourceRequest,
  InstalledPlugin,
  PluginUIExtensions,
  PluginInspectResponse,
  PluginCatalogItem,
  PluginCatalogSource,
  PluginTrustedKey,
  PluginCallResponse,
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

export async function getComposeStackYAML(name: string, serverId?: number): Promise<ComposeStackYAMLResponse> {
  const headers: Record<string, string> = {};
  if (serverId) headers["X-Server-ID"] = serverId.toString();
  return api.get<ComposeStackYAMLResponse>(`/api/docker/stacks/${name}/yaml`, { headers });
}

export async function updateComposeStackYAML(
  name: string,
  payload: UpdateStackYAMLRequest,
  serverId?: number
): Promise<{ message: string }> {
  const headers: Record<string, string> = {};
  if (serverId) headers["X-Server-ID"] = serverId.toString();
  return api.put<{ message: string }>(`/api/docker/stacks/${name}/yaml`, payload, { headers });
}

// Docker Image Dockerfile & Build
export async function getImageDockerfile(idOrTag: string, serverId?: number): Promise<ImageDockerfileResponse> {
  const headers: Record<string, string> = {};
  if (serverId) headers["X-Server-ID"] = serverId.toString();
  return api.get<ImageDockerfileResponse>(`/api/docker/images/dockerfile?id=${encodeURIComponent(idOrTag)}`, { headers });
}

export async function saveImageDockerfile(
  payload: { tag: string; image_id?: string; dockerfile: string },
  serverId?: number
): Promise<{ message: string }> {
  const headers: Record<string, string> = {};
  if (serverId) headers["X-Server-ID"] = serverId.toString();
  return api.put<{ message: string }>("/api/docker/images/dockerfile", payload, { headers });
}

export async function buildImageStream(
  payload: BuildImageRequest,
  onMessage: (msg: DockerBuildMessage) => void,
  signal?: AbortSignal,
  serverId?: number
): Promise<void> {
  const baseUrl = getBaseApiUrl();
  const token = getToken();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }
  const activeServerId = serverId || getActiveServerId();
  if (activeServerId) {
    headers["X-Server-ID"] = activeServerId.toString();
  }

  const response = await fetch(`${baseUrl}/api/docker/images/build/stream`, {
    method: "POST",
    headers,
    body: JSON.stringify(payload),
    signal,
  });

  if (!response.ok) {
    let errText = `Failed to build image (${response.status})`;
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
        const parsed = JSON.parse(trimmed) as DockerBuildMessage;
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
      const parsed = JSON.parse(buffer.trim()) as DockerBuildMessage;
      onMessage(parsed);
      if (parsed.error) {
        throw new ApiError(parsed.error, 500, parsed);
      }
    } catch (e) {
      if (e instanceof ApiError) throw e;
    }
  }
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
  packageManager?: string,
  saveSudoPolicy?: string
): Promise<HostPackageStatus> {
  const headers: Record<string, string> = {};
  if (sudoPassword) headers["X-Sudo-Password"] = sudoPassword;
  return api.post<HostPackageStatus>(
    `/api/hosts/${serverId}/packages/check`,
    { sudo_password: sudoPassword, package_manager: packageManager, save_sudo_policy: saveSudoPolicy },
    { headers }
  );
}

export async function upgradeHostPackages(
  serverId: number,
  packages?: string[],
  sudoPassword?: string,
  packageManager?: string,
  saveSudoPolicy?: string
): Promise<{ message: string }> {
  const headers: Record<string, string> = {};
  if (sudoPassword) headers["X-Sudo-Password"] = sudoPassword;
  return api.post<{ message: string }>(
    `/api/hosts/${serverId}/packages/upgrade`,
    { packages, sudo_password: sudoPassword, package_manager: packageManager, save_sudo_policy: saveSudoPolicy },
    { headers }
  );
}

export async function upgradeHostPackagesStream(
  serverId: number,
  packages?: string[],
  sudoPassword?: string,
  packageManager?: string,
  onLog?: (log: PackageUpgradeLog) => void,
  signal?: AbortSignal,
  saveSudoPolicy?: string,
  method: "GET" | "POST" = "POST"
): Promise<PackageUpgradeResult> {
  const baseUrl = getBaseApiUrl();
  const token = getToken();
  const headers: Record<string, string> = {};
  if (method === "POST") {
    headers["Content-Type"] = "application/json";
  }
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }
  if (sudoPassword) {
    headers["X-Sudo-Password"] = sudoPassword;
  }

  const response = await fetch(`${baseUrl}/api/hosts/${serverId}/packages/upgrade/stream`, {
    method,
    headers,
    body: method === "POST" ? JSON.stringify({
      packages,
      sudo_password: sudoPassword,
      package_manager: packageManager,
      save_sudo_policy: saveSudoPolicy,
    }) : undefined,
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

export async function getActiveHostUpgradeJob(serverId: number): Promise<ActiveUpgradeJob> {
  return api.get<ActiveUpgradeJob>(`/api/hosts/${serverId}/packages/upgrade/active`);
}

export async function listActiveHostUpgradeJobs(): Promise<Array<{
  server_id: number;
  server_name: string;
  status: string;
  package_manager: string;
  started_at: string;
  logs_count: number;
}>> {
  return api.get(`/api/hosts/packages/upgrade/active`);
}

export async function getOTPSetup(): Promise<OTPSetupResponse> {
  return api.get<OTPSetupResponse>("/api/auth/otp/setup");
}

export async function verifyOTPSetup(code: string): Promise<{ message: string; otp_enabled: boolean }> {
  return api.post<{ message: string; otp_enabled: boolean }>("/api/auth/otp/verify-setup", { code });
}

export async function disableOTP(password: string, code: string): Promise<{ message: string; otp_enabled: boolean }> {
  return api.post<{ message: string; otp_enabled: boolean }>("/api/auth/otp/disable", { password, code });
}

export async function recoverPasswordOTP(payload: RecoverPasswordOTPPayload): Promise<{ message: string }> {
  return api.post<{ message: string }>("/api/auth/recover-password/otp", payload);
}




// Configures key-based SSH access on a remote host using a one-time user/password (never persisted)
export async function provisionHostKey(payload: ProvisionKeyPayload): Promise<ProvisionKeyResult> {
  return api.post<ProvisionKeyResult>("/api/hosts/provision-key", payload);
}

// Source Search Directories & Host File Discovery
export async function listSourceDirs(serverId: number): Promise<SourceSearchDir[]> {
  return api.get<SourceSearchDir[]>(`/api/hosts/${serverId}/source-dirs`);
}

export async function createSourceDir(serverId: number, payload: CreateSourceDirRequest): Promise<SourceSearchDir> {
  return api.post<SourceSearchDir>(`/api/hosts/${serverId}/source-dirs`, payload);
}

export async function updateSourceDir(serverId: number, dirId: number, payload: UpdateSourceDirRequest): Promise<SourceSearchDir> {
  return api.put<SourceSearchDir>(`/api/hosts/${serverId}/source-dirs/${dirId}`, payload);
}

export async function deleteSourceDir(serverId: number, dirId: number): Promise<{ message: string }> {
  return api.delete<{ message: string }>(`/api/hosts/${serverId}/source-dirs/${dirId}`);
}

export async function listSourceLinks(serverId: number, kind?: string, ref?: string): Promise<SourceLink[]> {
  let url = `/api/hosts/${serverId}/sources/links`;
  const params: string[] = [];
  if (kind) params.push(`kind=${encodeURIComponent(kind)}`);
  if (ref) params.push(`ref=${encodeURIComponent(ref)}`);
  if (params.length > 0) url += `?${params.join("&")}`;
  return api.get<SourceLink[]>(url);
}

export async function confirmSourceLink(serverId: number, linkId: number): Promise<{ message: string }> {
  return api.post<{ message: string }>(`/api/hosts/${serverId}/sources/links/${linkId}/confirm`, {});
}

export async function rejectSourceLink(serverId: number, linkId: number): Promise<{ message: string }> {
  return api.post<{ message: string }>(`/api/hosts/${serverId}/sources/links/${linkId}/reject`, {});
}

export async function listDiscoveredFiles(serverId: number, kind?: string): Promise<SourceLink[]> {
  let url = `/api/hosts/${serverId}/sources/files`;
  if (kind) url += `?kind=${encodeURIComponent(kind)}`;
  return api.get<SourceLink[]>(url);
}

export async function getSourceContent(serverId: number, path: string): Promise<{ file_path: string; content: string; is_read_only: boolean }> {
  return api.get<{ file_path: string; content: string; is_read_only: boolean }>(
    `/api/hosts/${serverId}/sources/content?path=${encodeURIComponent(path)}`
  );
}

export async function bindSource(serverId: number, payload: BindSourceRequest): Promise<SourceLink> {
  return api.post<SourceLink>(`/api/hosts/${serverId}/sources/bind`, payload);
}

export async function unbindSource(serverId: number, target: number | UnbindSourceRequest): Promise<{ message: string }> {
  const payload = typeof target === "number" ? { link_id: target } : target;
  return api.post<{ message: string }>(`/api/hosts/${serverId}/sources/unbind`, payload);
}

export async function scanSourcesStream(
  serverId: number,
  onProgress?: (p: SourceScanProgress) => void,
  sudoPassword?: string,
  signal?: AbortSignal
): Promise<{ total_files_found: number; image_matches: number; stack_matches: number }> {
  const baseUrl = getBaseApiUrl();
  const token = getToken();
  const headers: Record<string, string> = {
    Accept: "text/event-stream",
  };
  if (token) headers["Authorization"] = `Bearer ${token}`;
  if (sudoPassword) headers["X-Sudo-Password"] = sudoPassword;

  const res = await fetch(`${baseUrl}/api/hosts/${serverId}/sources/scan`, {
    method: "POST",
    headers,
    signal,
  });

  if (!res.ok) {
    throw new Error(`Failed to scan host sources (${res.status})`);
  }

  let finalSummary = { total_files_found: 0, image_matches: 0, stack_matches: 0 };
  const reader = res.body?.getReader();
  if (!reader) return finalSummary;

  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split("\n\n");
    buffer = lines.pop() || "";

    for (const chunk of lines) {
      const dataLine = chunk.trim();
      if (dataLine.startsWith("data:")) {
        try {
          const raw = dataLine.slice(5).trim();
          const parsed: SourceScanProgress = JSON.parse(raw);
          if (onProgress) onProgress(parsed);
          finalSummary = {
            total_files_found: parsed.files_found || finalSummary.total_files_found,
            image_matches: parsed.matches_made || finalSummary.image_matches,
            stack_matches: 0,
          };
        } catch {
          // ignore malformed SSE line
        }
      }
    }
  }

  return finalSummary;
}

// Docker Hub Image Updates
export async function checkImageUpdates(
  serverId: number,
  imageIdOrTag?: string
): Promise<ImageUpdateInfo | { data: ImageUpdateInfo[] }> {
  const headers: Record<string, string> = {
    "X-Server-ID": serverId.toString(),
  };
  const url = imageIdOrTag
    ? `/api/docker/images/updates?id=${encodeURIComponent(imageIdOrTag)}`
    : `/api/docker/images/updates`;
  return api.get<ImageUpdateInfo | { data: ImageUpdateInfo[] }>(url, { headers });
}

export async function updateImage(payload: UpdateImageRequest): Promise<UpdateImageResult> {
  const headers: Record<string, string> = {
    "X-Server-ID": payload.server_id.toString(),
  };
  return api.post<UpdateImageResult>("/api/docker/images/update", payload, { headers });
}

// ==========================================
// Plugin System & Store APIs
// ==========================================

export async function getPlugins(): Promise<{ plugins: InstalledPlugin[]; ui_extensions: PluginUIExtensions }> {
  return api.get<{ plugins: InstalledPlugin[]; ui_extensions: PluginUIExtensions }>("/api/plugins");
}

export async function getStorePlugins(): Promise<{ plugins: PluginCatalogItem[] }> {
  return api.get<{ plugins: PluginCatalogItem[] }>("/api/plugins/store");
}

export async function inspectPluginUpload(file: File): Promise<PluginInspectResponse> {
  const formData = new FormData();
  formData.append("file", file);
  const baseUrl = getBaseApiUrl();
  const token = getToken();
  const headers: Record<string, string> = {};
  if (token) headers["Authorization"] = `Bearer ${token}`;

  const res = await fetch(`${baseUrl}/api/plugins/inspect-upload`, {
    method: "POST",
    headers,
    body: formData,
  });

  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new ApiError(err.error || `Falha ao inspecionar plugin (${res.status})`, res.status, err);
  }
  return res.json();
}

export async function inspectPluginURL(url: string): Promise<PluginInspectResponse> {
  return api.post<PluginInspectResponse>("/api/plugins/inspect-url", { url });
}

export async function installPluginUpload(
  file: File,
  expectedSHA256?: string,
  permissions?: string[],
  enableImmediately?: boolean
): Promise<InstalledPlugin> {
  const formData = new FormData();
  formData.append("file", file);
  if (expectedSHA256) formData.append("expected_sha256", expectedSHA256);
  if (permissions) formData.append("permissions", JSON.stringify(permissions));
  if (enableImmediately) formData.append("enable_immediately", "true");

  const baseUrl = getBaseApiUrl();
  const token = getToken();
  const headers: Record<string, string> = {};
  if (token) headers["Authorization"] = `Bearer ${token}`;

  const res = await fetch(`${baseUrl}/api/plugins/install-upload`, {
    method: "POST",
    headers,
    body: formData,
  });

  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new ApiError(err.error || `Falha ao instalar plugin (${res.status})`, res.status, err);
  }
  return res.json();
}

export async function installPluginURL(
  url: string,
  expectedSHA256?: string,
  grantedPermissions?: string[],
  enableImmediately?: boolean
): Promise<InstalledPlugin> {
  return api.post<InstalledPlugin>("/api/plugins/install-url", {
    url,
    expected_sha256: expectedSHA256,
    granted_permissions: grantedPermissions,
    enable_immediately: enableImmediately,
  });
}

export async function enablePlugin(pluginId: string): Promise<{ message: string }> {
  return api.post<{ message: string }>(`/api/plugins/${encodeURIComponent(pluginId)}/enable`);
}

export async function disablePlugin(pluginId: string): Promise<{ message: string }> {
  return api.post<{ message: string }>(`/api/plugins/${encodeURIComponent(pluginId)}/disable`);
}

export async function rebuildPlugin(pluginId: string): Promise<{ message: string; result: any }> {
  return api.post<{ message: string; result: any }>(`/api/plugins/${encodeURIComponent(pluginId)}/rebuild`);
}

export async function deletePlugin(pluginId: string): Promise<{ message: string }> {
  return api.delete<{ message: string }>(`/api/plugins/${encodeURIComponent(pluginId)}`);
}

export async function callPlugin<T = any>(pluginId: string, action: string, payload?: any): Promise<PluginCallResponse<T>> {
  return api.post<PluginCallResponse<T>>(`/api/plugins/${encodeURIComponent(pluginId)}/call`, {
    action,
    payload,
  });
}

export async function getPluginWorkerJS(pluginId: string): Promise<string> {
  const baseUrl = getBaseApiUrl();
  const token = getToken();
  const headers: Record<string, string> = {};
  if (token) headers["Authorization"] = `Bearer ${token}`;

  const res = await fetch(`${baseUrl}/api/plugins/${encodeURIComponent(pluginId)}/worker.js`, {
    headers,
  });
  if (!res.ok) {
    throw new ApiError(`Falha ao obter worker.js do plugin (${res.status})`, res.status);
  }
  return res.text();
}

export async function getCatalogSources(): Promise<PluginCatalogSource[]> {
  return api.get<PluginCatalogSource[]>("/api/plugins/catalogs");
}

export async function addCatalogSource(name: string, url: string): Promise<PluginCatalogSource> {
  return api.post<PluginCatalogSource>("/api/plugins/catalogs", { name, url });
}

export async function deleteCatalogSource(id: number): Promise<{ message: string }> {
  return api.delete<{ message: string }>(`/api/plugins/catalogs/${id}`);
}

export async function syncCatalogSource(id: number): Promise<any> {
  return api.post<any>(`/api/plugins/catalogs/${id}/sync`);
}

export async function getTrustedKeys(): Promise<PluginTrustedKey[]> {
  return api.get<PluginTrustedKey[]>("/api/plugins/trusted-keys");
}

export async function addTrustedKey(name: string, publicKeyEd25519: string): Promise<PluginTrustedKey> {
  return api.post<PluginTrustedKey>("/api/plugins/trusted-keys", {
    name,
    public_key_ed25519: publicKeyEd25519,
  });
}

export async function deleteTrustedKey(id: number): Promise<{ message: string }> {
  return api.delete<{ message: string }>(`/api/plugins/trusted-keys/${id}`);
}

export async function getAIGuide(): Promise<{ filename: string; content: string }> {
  return api.get<{ filename: string; content: string }>("/api/plugins/ai-guide?format=json");
}
