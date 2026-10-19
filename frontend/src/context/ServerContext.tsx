"use client";

import React, { createContext, useContext, useEffect, useState, useCallback } from "react";
import { DockerServer } from "@/types";
import { api, getActiveServerId, setActiveServerId } from "@/lib/api";

interface ServerContextType {
  servers: DockerServer[];
  activeServer: DockerServer | null;
  isLoading: boolean;
  setActiveServer: (server: DockerServer) => void;
  refreshServers: () => Promise<void>;
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
