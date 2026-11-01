"use client";

import React, { createContext, useContext, useEffect, useState, useCallback } from "react";
import { DockerServer } from "@/types";
import { api, getActiveServerId, setActiveServerId, ServerStatusStreamUpdate } from "@/lib/api";

interface ServerContextType {
  servers: DockerServer[];
  activeServer: DockerServer | null;
  isLoading: boolean;
  setActiveServer: (server: DockerServer) => void;
  refreshServers: () => Promise<void>;
  updateServerState: (update: ServerStatusStreamUpdate) => void;
  setAllServersStatus: (status: string) => void;
}

const ServerContext = createContext<ServerContextType | undefined>(undefined);

export function ServerProvider({ children }: { children: React.ReactNode }) {
  const [servers, setServers] = useState<DockerServer[]>([]);
  const [activeServer, setActiveServerState] = useState<DockerServer | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(true);

  const refreshServers = useCallback(async () => {
    try {
      const data = await api.get<DockerServer[]>("/api/docker/servers");
      const list = Array.isArray(data) ? data : [];
      setServers(list);

      if (list.length > 0) {
        const savedId = getActiveServerId();
        let target = list.find((s) => s.id === savedId);

        if (!target) {
          target = list.find((s) => s.is_default) || list[0];
        }

        if (target) {
          setActiveServerState(target);
          setActiveServerId(target.id);
        }
      } else {
        setActiveServerState(null);
        setActiveServerId(null);
      }
    } catch (err) {
      console.warn("Failed to fetch docker servers:", err);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    refreshServers();
  }, [refreshServers]);

  const updateServerState = useCallback((update: ServerStatusStreamUpdate) => {
    setServers((prev) =>
      prev.map((s) => {
        if (s.id !== update.server_id) return s;
        return {
          ...s,
          status: update.status,
          version: update.version !== undefined ? update.version : s.version,
          containers_count: update.containers_count !== undefined ? update.containers_count : s.containers_count,
          images_count: update.images_count !== undefined ? update.images_count : s.images_count,
          err_msg: update.err_msg !== undefined ? update.err_msg : s.err_msg,
        };
      })
    );
    setActiveServerState((prev) => {
      if (!prev || prev.id !== update.server_id) return prev;
      return {
        ...prev,
        status: update.status,
        version: update.version !== undefined ? update.version : prev.version,
        containers_count: update.containers_count !== undefined ? update.containers_count : prev.containers_count,
        images_count: update.images_count !== undefined ? update.images_count : prev.images_count,
        err_msg: update.err_msg !== undefined ? update.err_msg : prev.err_msg,
      };
    });
  }, []);

  const setAllServersStatus = useCallback((status: string) => {
    setServers((prev) => prev.map((s) => ({ ...s, status })));
  }, []);

  const setActiveServer = (server: DockerServer) => {
    setActiveServerState(server);
    setActiveServerId(server.id);
  };

  return (
    <ServerContext.Provider
      value={{
        servers,
        activeServer,
        isLoading,
        setActiveServer,
        refreshServers,
        updateServerState,
        setAllServersStatus,
      }}
    >
      {children}
    </ServerContext.Provider>
  );
}

export function useServer(): ServerContextType {
  const context = useContext(ServerContext);
  if (!context) {
    throw new Error("useServer must be used within a ServerProvider");
  }
  return context;
}
