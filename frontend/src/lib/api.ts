import {
  DockerPullMessage,
  ServerDiagnosticLog,
  TestServerPayload,
  TestServerResult,
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
      if (typeof window !== "undefined" && !window.location.pathname.startsWith("/login")) {
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

