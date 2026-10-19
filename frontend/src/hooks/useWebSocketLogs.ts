"use client";

import { useEffect, useRef, useState, useCallback } from "react";
import { getToken, getBaseApiUrl, getWsTicket } from "@/lib/api";

export type ConnectionStatus = "idle" | "connecting" | "connected" | "disconnected" | "error";

interface UseWebSocketLogsOptions {
  url?: string | null;
  maxLines?: number;
  autoReconnect?: boolean;
  reconnectInterval?: number;
}

export function useWebSocketLogs({
  url,
  maxLines = 2000,
  autoReconnect = true,
  reconnectInterval = 3000,
}: UseWebSocketLogsOptions) {
  const [logs, setLogs] = useState<string[]>([]);
  const [status, setStatus] = useState<ConnectionStatus>("idle");
  const [isPaused, setIsPaused] = useState(false);
  const isPausedRef = useRef(isPaused);
  isPausedRef.current = isPaused;

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const pausedBufferRef = useRef<string[]>([]);

  const clearLogs = useCallback(() => {
    setLogs([]);
    pausedBufferRef.current = [];
  }, []);

  const togglePause = useCallback(() => {
    setIsPaused((prev) => {
      const next = !prev;
      if (!next && pausedBufferRef.current.length > 0) {
        // flush buffered logs when unpausing
        setLogs((current) => {
          const combined = [...current, ...pausedBufferRef.current];
          return combined.slice(-maxLines);
        });
        pausedBufferRef.current = [];
      }
      return next;
    });
  }, [maxLines]);

  const connect = useCallback(async () => {
    if (!url) {
      setStatus("idle");
      return;
    }

    // Close any existing socket
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }

    setStatus("connecting");

    // Construct full ws:// or wss:// URL
    const token = getToken();
    let fullUrl = url;

    // Handle relative or absolute URLs
    if (url.startsWith("/")) {
      const baseApi = getBaseApiUrl();
      if (baseApi) {
        const wsBase = baseApi.replace(/^http/, "ws");
        fullUrl = `${wsBase}${url}`;
      } else if (typeof window !== "undefined") {
        const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
        fullUrl = `${protocol}//${window.location.host}${url}`;
      }
    }

    // Obtain single-use ticket to prevent leaking JWT in query logs
    const ticket = await getWsTicket();
    if (ticket) {
      const separator = fullUrl.includes("?") ? "&" : "?";
      fullUrl = `${fullUrl}${separator}ticket=${encodeURIComponent(ticket)}`;
    } else if (token && !fullUrl.includes("token=")) {
      // Fallback to token if ticket exchange is unavailable
      const separator = fullUrl.includes("?") ? "&" : "?";
      fullUrl = `${fullUrl}${separator}token=${encodeURIComponent(token)}`;
    }

    try {
      const socket = new WebSocket(fullUrl);
      wsRef.current = socket;

      socket.onopen = () => {
        setStatus("connected");
      };

      socket.onmessage = (event) => {
        const newLines = String(event.data)
          .split("\n")
          .filter((line) => line.length > 0);

        if (isPausedRef.current) {
          pausedBufferRef.current.push(...newLines);
          if (pausedBufferRef.current.length > maxLines) {
            pausedBufferRef.current = pausedBufferRef.current.slice(-maxLines);
          }
        } else {
          setLogs((prev) => {
            const next = [...prev, ...newLines];
            return next.length > maxLines ? next.slice(-maxLines) : next;
          });
        }
      };

      socket.onerror = () => {
        setStatus("error");
      };

      socket.onclose = (event) => {
        setStatus("disconnected");
        wsRef.current = null;

        // Auto reconnect if not closed normally and autoReconnect is true
        if (autoReconnect && event.code !== 1000) {
          reconnectTimeoutRef.current = setTimeout(() => {
            if (url) connect();
          }, reconnectInterval);
        }
      };
    } catch {
      setStatus("error");
    }
  }, [url, autoReconnect, reconnectInterval, maxLines]);

  useEffect(() => {
    connect();

    return () => {
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
      if (wsRef.current) {
        wsRef.current.close(1000, "Component unmounted");
        wsRef.current = null;
      }
    };
  }, [connect]);

  return {
    logs,
    status,
    isPaused,
    togglePause,
    clearLogs,
    reconnect: connect,
  };
}
