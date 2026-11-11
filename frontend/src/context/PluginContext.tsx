"use client";

import React, { createContext, useContext, useState, useEffect, useCallback } from "react";
import {
  InstalledPlugin,
  PluginUIExtensions,
  PluginSidebarExtension,
  PluginDashboardExtension,
} from "@/types";
import { getPlugins } from "@/lib/api";
import { useAuth } from "./AuthContext";

interface SidebarPluginItem {
  plugin: InstalledPlugin;
  ext: PluginSidebarExtension;
  href: string;
}

interface DashboardWidgetPluginItem {
  plugin: InstalledPlugin;
  ext: PluginDashboardExtension;
}

interface PluginContextType {
  plugins: InstalledPlugin[];
  enabledPlugins: InstalledPlugin[];
  uiExtensions: PluginUIExtensions;
  sidebarPlugins: SidebarPluginItem[];
  dashboardWidgets: DashboardWidgetPluginItem[];
  isLoading: boolean;
  refreshPlugins: () => Promise<void>;
}

const PluginContext = createContext<PluginContextType>({
  plugins: [],
  enabledPlugins: [],
  uiExtensions: {},
  sidebarPlugins: [],
  dashboardWidgets: [],
  isLoading: false,
  refreshPlugins: async () => {},
});

export function PluginProvider({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, user } = useAuth();
  const [plugins, setPlugins] = useState<InstalledPlugin[]>([]);
  const [uiExtensions, setUiExtensions] = useState<PluginUIExtensions>({});
  const [isLoading, setIsLoading] = useState(false);

  const refreshPlugins = useCallback(async () => {
    if (!isAuthenticated) return;
    try {
      setIsLoading(true);
      const res = await getPlugins();
      setPlugins(res.plugins || []);
      setUiExtensions(res.ui_extensions || {});
    } catch {
      // Ignored if offline or transient
    } finally {
      setIsLoading(false);
    }
  }, [isAuthenticated, user?.id]);

  useEffect(() => {
    refreshPlugins();
  }, [refreshPlugins]);

  const enabledPlugins = plugins.filter((p) => p.status === "enabled");

  // Build dynamic sidebar items
  const sidebarPlugins: SidebarPluginItem[] = [];
  for (const p of enabledPlugins) {
    let manifest: any = null;
    try {
      manifest = JSON.parse(p.manifest_json);
    } catch {
      continue;
    }
    if (manifest?.ui_extensions?.sidebar) {
      for (const s of manifest.ui_extensions.sidebar) {
        sidebarPlugins.push({
          plugin: p,
          ext: s,
          href: `/plugins/${encodeURIComponent(p.plugin_id)}`,
        });
      }
    }
  }

  // Build dashboard widgets
  const dashboardWidgets: DashboardWidgetPluginItem[] = [];
  for (const p of enabledPlugins) {
    let manifest: any = null;
    try {
      manifest = JSON.parse(p.manifest_json);
    } catch {
      continue;
    }
    if (manifest?.ui_extensions?.dashboard_widgets) {
      for (const w of manifest.ui_extensions.dashboard_widgets) {
        dashboardWidgets.push({
          plugin: p,
          ext: w,
        });
      }
    }
  }

  return (
    <PluginContext.Provider
      value={{
        plugins,
        enabledPlugins,
        uiExtensions,
        sidebarPlugins,
        dashboardWidgets,
        isLoading,
        refreshPlugins,
      }}
    >
      {children}
    </PluginContext.Provider>
  );
}

export function usePlugins() {
  return useContext(PluginContext);
}
