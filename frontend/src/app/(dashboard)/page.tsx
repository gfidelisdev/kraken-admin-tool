"use client";

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import Link from "next/link";
import {
  Box,
  Disc,
  Server,
  Layers,
  Activity,
  Cpu,
  Database,
  ArrowRight,
  Download,
  Terminal,
  RefreshCw,
  CheckCircle2,
  XCircle,
  Clock,
  ShieldCheck,
  Puzzle,
  GripVertical,
  ArrowUp,
  ArrowDown,
  Minimize2,
  Maximize2,
  EyeOff,
  SlidersHorizontal,
  LayoutGrid,
} from "lucide-react";
import { api, getDashboardLayout, saveDashboardLayout } from "@/lib/api";
import { useAuth } from "@/context/AuthContext";
import {
  SystemStatus,
  DockerContainer,
  DockerImage,
  K8sPod,
  K8sDeployment,
  AuditLog,
} from "@/types";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/Card";
import { Button } from "@/components/ui/Button";
import { StatusBadge, Badge } from "@/components/ui/Badge";
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  SortableTableHead,
  TableBody,
  TableCell,
  TableEmpty,
} from "@/components/ui/Table";
import { sortItems, SortDirection } from "@/lib/sort";
import { useServer } from "@/context/ServerContext";
import { usePlugins } from "@/context/PluginContext";
import { PluginSurfaceRenderer } from "@/components/plugins/PluginSurfaceRenderer";
import { PullImageModal } from "@/components/modals/PullImageModal";
import {
  CustomizeDashboardModal,
  DashboardItemDefinition,
} from "@/components/modals/CustomizeDashboardModal";
import { formatDate } from "@/lib/utils";

const STORAGE_KEY = "kraken_dashboard_layout_v1";

const DEFAULT_ORDER = [
  "metrics",
  "plugin_widgets",
  "infra_health",
  "quick_nav",
  "audit_logs",
];

const DEFAULT_VISIBILITY: Record<string, boolean> = {
  metrics: true,
  plugin_widgets: true,
  infra_health: true,
  quick_nav: true,
  audit_logs: true,
};

const DEFAULT_COLLAPSED: Record<string, boolean> = {
  metrics: false,
  plugin_widgets: false,
  infra_health: false,
  quick_nav: false,
  audit_logs: false,
};

export default function DashboardOverviewPage() {
  const { user } = useAuth();
  const { activeServer } = useServer();
  const { dashboardWidgets } = usePlugins();
  const [status, setStatus] = useState<SystemStatus | null>(null);
  const [containers, setContainers] = useState<DockerContainer[]>([]);
  const [images, setImages] = useState<DockerImage[]>([]);
  const [pods, setPods] = useState<K8sPod[]>([]);
  const [deployments, setDeployments] = useState<K8sDeployment[]>([]);
  const [auditLogs, setAuditLogs] = useState<AuditLog[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [pullModalOpen, setPullModalOpen] = useState(false);
  const [customizeModalOpen, setCustomizeModalOpen] = useState(false);
  const [auditSortKey, setAuditSortKey] = useState<string>("timestamp");
  const [auditSortDir, setAuditSortDir] = useState<SortDirection>("desc");

  const userStorageKey = user?.id ? `${STORAGE_KEY}_u${user.id}` : STORAGE_KEY;

  // Dashboard Layout State (Order, Visibility, Collapsed/Compact view)
  const [order, setOrder] = useState<string[]>(DEFAULT_ORDER);
  const [visibility, setVisibility] = useState<Record<string, boolean>>(DEFAULT_VISIBILITY);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(DEFAULT_COLLAPSED);

  // Independent Plugin Widgets State (Order, Visibility, Collapsed)
  const [widgetOrder, setWidgetOrder] = useState<string[]>([]);
  const [widgetVisibility, setWidgetVisibility] = useState<Record<string, boolean>>({});
  const [widgetCollapsed, setWidgetCollapsed] = useState<Record<string, boolean>>({});

  // Drag-and-drop state for main blocks and plugin widgets
  const [draggedId, setDraggedId] = useState<string | null>(null);
  const [dragOverId, setDragOverId] = useState<string | null>(null);
  const [draggedWidgetId, setDraggedWidgetId] = useState<string | null>(null);
  const [dragOverWidgetId, setDragOverWidgetId] = useState<string | null>(null);

  const getWidgetKey = useCallback(
    (item: { plugin: { plugin_id: string }; ext: { id: string } }) =>
      `${item.plugin.plugin_id}::${item.ext.id}`,
    []
  );

  const applyLayoutObject = useCallback((parsed: any) => {
    if (!parsed) return;
    if (Array.isArray(parsed.order)) {
      const mergedOrder = [
        ...parsed.order.filter((id: string) => DEFAULT_ORDER.includes(id)),
        ...DEFAULT_ORDER.filter((id) => !parsed.order.includes(id)),
      ];
      setOrder(mergedOrder);
    }
    if (parsed.visibility && typeof parsed.visibility === "object") {
      setVisibility({ ...DEFAULT_VISIBILITY, ...parsed.visibility });
    }
    if (parsed.collapsed && typeof parsed.collapsed === "object") {
      setCollapsed({ ...DEFAULT_COLLAPSED, ...parsed.collapsed });
    }
    if (Array.isArray(parsed.widgetOrder)) {
      setWidgetOrder(parsed.widgetOrder);
    }
    if (parsed.widgetVisibility && typeof parsed.widgetVisibility === "object") {
      setWidgetVisibility(parsed.widgetVisibility);
    }
    if (parsed.widgetCollapsed && typeof parsed.widgetCollapsed === "object") {
      setWidgetCollapsed(parsed.widgetCollapsed);
    }
  }, []);

  // Load preferences from backend API (with user-scoped localStorage fallback)
  useEffect(() => {
    let isMounted = true;
    const load = async () => {
      // 1. Try backend API
      try {
        const res = await getDashboardLayout();
        if (res?.layout_json && isMounted) {
          try {
            const parsed = JSON.parse(res.layout_json);
            applyLayoutObject(parsed);
            localStorage.setItem(userStorageKey, res.layout_json);
            return;
          } catch {
            // invalid JSON, ignore
          }
        }
      } catch {
        // network or offline
      }

      // 2. Fallback to localStorage
      try {
        const saved = localStorage.getItem(userStorageKey) || localStorage.getItem(STORAGE_KEY);
        if (saved && isMounted) {
          const parsed = JSON.parse(saved);
          applyLayoutObject(parsed);
        }
      } catch (e) {
        console.warn("Failed to load dashboard layout:", e);
      }
    };

    load();
    return () => {
      isMounted = false;
    };
  }, [userStorageKey, applyLayoutObject]);

  // Save preferences to backend and localStorage
  const saveLayout = useCallback(
    (
      newOrder: string[],
      newVisibility: Record<string, boolean>,
      newCollapsed: Record<string, boolean>,
      newWidgetOrder: string[] = widgetOrder,
      newWidgetVisibility: Record<string, boolean> = widgetVisibility,
      newWidgetCollapsed: Record<string, boolean> = widgetCollapsed
    ) => {
      const payload = {
        order: newOrder,
        visibility: newVisibility,
        collapsed: newCollapsed,
        widgetOrder: newWidgetOrder,
        widgetVisibility: newWidgetVisibility,
        widgetCollapsed: newWidgetCollapsed,
      };
      const jsonStr = JSON.stringify(payload);

      try {
        localStorage.setItem(userStorageKey, jsonStr);
      } catch (e) {
        console.warn("Failed to persist dashboard layout locally:", e);
      }

      saveDashboardLayout(jsonStr).catch((err) => {
        console.warn("Failed to persist dashboard layout to backend:", err);
      });
    },
    [userStorageKey, widgetOrder, widgetVisibility, widgetCollapsed]
  );

  // Synchronize available plugin widgets with saved widgetOrder
  useEffect(() => {
    if (!dashboardWidgets.length) return;
    const currentKeys = dashboardWidgets.map(getWidgetKey);
    setWidgetOrder((prev) => {
      const validExisting = prev.filter((k) => currentKeys.includes(k));
      const added = currentKeys.filter((k) => !validExisting.includes(k));
      const merged = [...validExisting, ...added];
      if (merged.length === prev.length && merged.every((v, i) => v === prev[i])) {
        return prev;
      }
      return merged;
    });
  }, [dashboardWidgets, getWidgetKey]);

  const handleAuditSort = (key: string) => {
    if (auditSortKey === key) {
      setAuditSortDir((prev) => (prev === "asc" ? "desc" : "asc"));
    } else {
      setAuditSortKey(key);
      setAuditSortDir(key === "timestamp" ? "desc" : "asc");
    }
  };

  const sortedAuditLogs = useMemo(() => {
    const customGetters = {
      user: (l: AuditLog) => l.username,
      action: (l: AuditLog) => l.action,
      resource: (l: AuditLog) => l.resource_type,
      resource_id: (l: AuditLog) => l.resource_id,
      status: (l: AuditLog) => l.status,
      timestamp: (l: AuditLog) => l.created_at || "",
    };
    return sortItems(auditLogs, auditSortKey, auditSortDir, customGetters);
  }, [auditLogs, auditSortKey, auditSortDir]);

  const fetchDashboardData = useCallback(async () => {
    setIsLoading(true);
    try {
      const [statusRes, containersRes, imagesRes, podsRes, deploymentsRes, auditRes] =
        await Promise.allSettled([
          api.get<SystemStatus>("/api/system/status"),
          api.get<DockerContainer[]>("/api/docker/containers?all=true"),
          api.get<DockerImage[]>("/api/docker/images"),
          api.get<K8sPod[]>("/api/k8s/pods"),
          api.get<K8sDeployment[]>("/api/k8s/deployments"),
          api.get<any>("/api/audit-logs"),
        ]);

      if (statusRes.status === "fulfilled") setStatus(statusRes.value);
      if (containersRes.status === "fulfilled" && Array.isArray(containersRes.value)) {
        setContainers(containersRes.value);
      }
      if (imagesRes.status === "fulfilled" && Array.isArray(imagesRes.value)) {
        setImages(imagesRes.value);
      }
      if (podsRes.status === "fulfilled" && Array.isArray(podsRes.value)) {
        setPods(podsRes.value);
      }
      if (deploymentsRes.status === "fulfilled" && Array.isArray(deploymentsRes.value)) {
        setDeployments(deploymentsRes.value);
      }
      if (auditRes.status === "fulfilled") {
        const data = auditRes.value;
        const list = Array.isArray(data) ? data : data?.data || data?.logs || [];
        setAuditLogs(list);
      }
    } catch (e) {
      console.error("Dashboard fetch error:", e);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchDashboardData();
  }, [fetchDashboardData, activeServer?.id]);

  const handlePullImage = async (imageName: string) => {
    await api.post("/api/docker/images/pull", { image: imageName });
    fetchDashboardData();
  };

  const runningContainers = containers.filter(
    (c) => c.state === "running" || c.status?.toLowerCase().includes("up")
  ).length;

  const runningPods = pods.filter(
    (p) => p.status?.toLowerCase() === "running"
  ).length;

  // Reorder Handler
  const handleReorder = (newOrder: string[]) => {
    setOrder(newOrder);
    saveLayout(newOrder, visibility, collapsed);
  };

  const handleMove = (id: string, direction: "up" | "down") => {
    const currentIndex = order.indexOf(id);
    if (currentIndex === -1) return;
    const targetIndex = direction === "up" ? currentIndex - 1 : currentIndex + 1;
    if (targetIndex < 0 || targetIndex >= order.length) return;

    const newOrder = [...order];
    const [moved] = newOrder.splice(currentIndex, 1);
    newOrder.splice(targetIndex, 0, moved);
    setOrder(newOrder);
    saveLayout(newOrder, visibility, collapsed);
  };

  // Visibility Handlers
  const handleToggleVisibility = (id: string) => {
    const next = { ...visibility, [id]: !visibility[id] };
    setVisibility(next);
    saveLayout(order, next, collapsed);
  };

  const handleSetAllVisibility = (visible: boolean) => {
    const next: Record<string, boolean> = {};
    order.forEach((id) => {
      next[id] = visible;
    });
    setVisibility(next);
    saveLayout(order, next, collapsed);
  };

  // Collapse/Expand Handlers
  const handleToggleCollapse = (id: string) => {
    const next = { ...collapsed, [id]: !collapsed[id] };
    setCollapsed(next);
    saveLayout(order, visibility, next);
  };

  const handleSetAllCollapse = (isCollapsed: boolean) => {
    const next: Record<string, boolean> = {};
    order.forEach((id) => {
      next[id] = isCollapsed;
    });
    setCollapsed(next);
    saveLayout(order, visibility, next);
  };

  // Reset to default
  const handleResetLayout = () => {
    const defaultWidgetKeys = dashboardWidgets.map(getWidgetKey);
    setOrder(DEFAULT_ORDER);
    setVisibility(DEFAULT_VISIBILITY);
    setCollapsed(DEFAULT_COLLAPSED);
    setWidgetOrder(defaultWidgetKeys);
    setWidgetVisibility({});
    setWidgetCollapsed({});
    saveLayout(DEFAULT_ORDER, DEFAULT_VISIBILITY, DEFAULT_COLLAPSED, defaultWidgetKeys, {}, {});
  };

  // Independent Plugin Widget Handlers
  const handleReorderWidgets = (newWidgetOrder: string[]) => {
    setWidgetOrder(newWidgetOrder);
    saveLayout(order, visibility, collapsed, newWidgetOrder, widgetVisibility, widgetCollapsed);
  };

  const handleMoveWidget = (wId: string, direction: "up" | "down") => {
    const currentIndex = widgetOrder.indexOf(wId);
    if (currentIndex === -1) return;
    const targetIndex = direction === "up" ? currentIndex - 1 : currentIndex + 1;
    if (targetIndex < 0 || targetIndex >= widgetOrder.length) return;

    const newWidgetOrder = [...widgetOrder];
    const [moved] = newWidgetOrder.splice(currentIndex, 1);
    newWidgetOrder.splice(targetIndex, 0, moved);
    setWidgetOrder(newWidgetOrder);
    saveLayout(order, visibility, collapsed, newWidgetOrder, widgetVisibility, widgetCollapsed);
  };

  const handleToggleWidgetVisibility = (wId: string) => {
    const isCurrentlyVisible = widgetVisibility[wId] !== false;
    const next = { ...widgetVisibility, [wId]: !isCurrentlyVisible };
    setWidgetVisibility(next);
    saveLayout(order, visibility, collapsed, widgetOrder, next, widgetCollapsed);
  };

  const handleToggleWidgetCollapse = (wId: string) => {
    const next = { ...widgetCollapsed, [wId]: !widgetCollapsed[wId] };
    setWidgetCollapsed(next);
    saveLayout(order, visibility, collapsed, widgetOrder, widgetVisibility, next);
  };

  const handleSetAllWidgetVisibility = (visible: boolean) => {
    const next: Record<string, boolean> = {};
    dashboardWidgets.forEach((item) => {
      next[getWidgetKey(item)] = visible;
    });
    setWidgetVisibility(next);
    saveLayout(order, visibility, collapsed, widgetOrder, next, widgetCollapsed);
  };

  // Plugin Widget Drag-and-drop handlers
  const handleWidgetDragStart = (e: React.DragEvent, wId: string) => {
    e.stopPropagation();
    e.dataTransfer.setData("application/x-kraken-widget", wId);
    e.dataTransfer.effectAllowed = "move";
    setDraggedWidgetId(wId);
  };

  const handleWidgetDragOver = (e: React.DragEvent, wId: string) => {
    if (!draggedWidgetId) return;
    e.preventDefault();
    e.stopPropagation();
    if (draggedWidgetId !== wId) {
      setDragOverWidgetId(wId);
    }
  };

  const handleWidgetDragLeave = (e: React.DragEvent) => {
    if (!draggedWidgetId) return;
    e.stopPropagation();
    setDragOverWidgetId(null);
  };

  const handleWidgetDrop = (e: React.DragEvent, targetWId: string) => {
    if (!draggedWidgetId) return;
    e.preventDefault();
    e.stopPropagation();
    const sourceWId = e.dataTransfer.getData("application/x-kraken-widget") || draggedWidgetId;
    setDragOverWidgetId(null);
    setDraggedWidgetId(null);
    if (!sourceWId || sourceWId === targetWId) return;

    const fromIndex = widgetOrder.indexOf(sourceWId);
    const toIndex = widgetOrder.indexOf(targetWId);
    if (fromIndex === -1 || toIndex === -1) return;

    const newWidgetOrder = [...widgetOrder];
    const [moved] = newWidgetOrder.splice(fromIndex, 1);
    newWidgetOrder.splice(toIndex, 0, moved);
    setWidgetOrder(newWidgetOrder);
    saveLayout(order, visibility, collapsed, newWidgetOrder, widgetVisibility, widgetCollapsed);
  };

  const handleWidgetDragEnd = (e: React.DragEvent) => {
    e.stopPropagation();
    setDraggedWidgetId(null);
    setDragOverWidgetId(null);
  };

  const pluginWidgetDefinitions: DashboardItemDefinition[] = useMemo(
    () =>
      dashboardWidgets.map((item) => ({
        id: getWidgetKey(item),
        title: item.ext.title,
        description: `Plugin: ${item.plugin.name} (${item.plugin.plugin_id})`,
        icon: <Puzzle className="h-4 w-4" />,
      })),
    [dashboardWidgets, getWidgetKey]
  );

  // Drag and drop handlers
  const handleDragStart = (e: React.DragEvent, id: string) => {
    e.dataTransfer.setData("text/plain", id);
    e.dataTransfer.effectAllowed = "move";
    setDraggedId(id);
  };

  const handleDragOver = (e: React.DragEvent, id: string) => {
    e.preventDefault();
    if (draggedId && draggedId !== id) {
      setDragOverId(id);
    }
  };

  const handleDragLeave = () => {
    setDragOverId(null);
  };

  const handleDrop = (e: React.DragEvent, targetId: string) => {
    e.preventDefault();
    setDragOverId(null);
    setDraggedId(null);
    const sourceId = e.dataTransfer.getData("text/plain");
    if (!sourceId || sourceId === targetId) return;

    const fromIndex = order.indexOf(sourceId);
    const toIndex = order.indexOf(targetId);
    if (fromIndex === -1 || toIndex === -1) return;

    const newOrder = [...order];
    const [moved] = newOrder.splice(fromIndex, 1);
    newOrder.splice(toIndex, 0, moved);
    setOrder(newOrder);
    saveLayout(newOrder, visibility, collapsed);
  };

  const handleDragEnd = () => {
    setDraggedId(null);
    setDragOverId(null);
  };

  // Item definitions for customizing
  const itemDefinitions: DashboardItemDefinition[] = useMemo(
    () => [
      {
        id: "metrics",
        title: "Métricas de Containers & Workloads",
        description: "Contadores de containers Docker, imagens locais, pods Kubernetes e deployments.",
        icon: <Box className="h-4 w-4" />,
      },
      {
        id: "plugin_widgets",
        title: "Widgets de Plugins Ativos",
        description: "Extensões e widgets declarativos fornecidos pelos plugins instalados.",
        icon: <Puzzle className="h-4 w-4" />,
      },
      {
        id: "infra_health",
        title: "Saúde da Infraestrutura",
        description: "Conexão com Docker Engine, cluster Kubernetes e banco de dados PostgreSQL.",
        icon: <Activity className="h-4 w-4" />,
      },
      {
        id: "quick_nav",
        title: "Acesso Rápido a Recursos",
        description: "Atalhos rápidos para gerenciar containers, pods e transmissão ao vivo de logs.",
        icon: <ArrowRight className="h-4 w-4" />,
      },
      {
        id: "audit_logs",
        title: "Auditoria Recente de Operações",
        description: "Histórico das últimas operações e ações executadas no cluster.",
        icon: <ShieldCheck className="h-4 w-4" />,
      },
    ],
    []
  );

  // Section Renders (Full vs Compact)
  const renderMetrics = (isCompact: boolean) => {
    if (isCompact) {
      return (
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 py-1">
          <div className="flex items-center justify-between p-3 rounded-lg bg-slate-900/90 border border-slate-800">
            <div>
              <span className="text-[11px] text-slate-400 block font-medium">Containers</span>
              <span className="text-lg font-bold text-white">{containers.length}</span>
            </div>
            <span className="flex items-center text-[11px] text-emerald-400 font-medium">
              <span className="h-2 w-2 rounded-full bg-emerald-400 mr-1.5 animate-pulse" />
              {runningContainers}
            </span>
          </div>

          <div className="flex items-center justify-between p-3 rounded-lg bg-slate-900/90 border border-slate-800">
            <div>
              <span className="text-[11px] text-slate-400 block font-medium">Imagens</span>
              <span className="text-lg font-bold text-white">{images.length}</span>
            </div>
            <Disc className="h-4 w-4 text-sky-400" />
          </div>

          <div className="flex items-center justify-between p-3 rounded-lg bg-slate-900/90 border border-slate-800">
            <div>
              <span className="text-[11px] text-slate-400 block font-medium">Pods K8s</span>
              <span className="text-lg font-bold text-white">{pods.length}</span>
            </div>
            <span className="flex items-center text-[11px] text-indigo-400 font-medium">
              <span className="h-2 w-2 rounded-full bg-indigo-400 mr-1.5 animate-pulse" />
              {runningPods}
            </span>
          </div>

          <div className="flex items-center justify-between p-3 rounded-lg bg-slate-900/90 border border-slate-800">
            <div>
              <span className="text-[11px] text-slate-400 block font-medium">Deployments</span>
              <span className="text-lg font-bold text-white">{deployments.length}</span>
            </div>
            <Layers className="h-4 w-4 text-indigo-400" />
          </div>
        </div>
      );
    }

    return (
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {/* Docker Containers */}
        <Card className="hover:border-sky-500/40 transition-all glow-blue">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-xs font-semibold text-slate-400 uppercase tracking-wider">
              Docker Containers
            </CardTitle>
            <Box className="h-5 w-5 text-sky-400" />
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-extrabold text-white">{containers.length}</div>
            <div className="mt-2 flex items-center text-xs text-slate-400">
              <span className="flex items-center text-emerald-400 font-semibold mr-2">
                <span className="h-2 w-2 rounded-full bg-emerald-400 mr-1.5 animate-pulse" />
                {runningContainers} running
              </span>
              <span>• {containers.length - runningContainers} stopped</span>
            </div>
          </CardContent>
        </Card>

        {/* Docker Images */}
        <Card className="hover:border-sky-500/40 transition-all">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-xs font-semibold text-slate-400 uppercase tracking-wider">
              Local Images
            </CardTitle>
            <Disc className="h-5 w-5 text-sky-400" />
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-extrabold text-white">{images.length}</div>
            <p className="mt-2 text-xs text-slate-400">Cached images in local Docker repository</p>
          </CardContent>
        </Card>

        {/* Kubernetes Pods */}
        <Card className="hover:border-indigo-500/40 transition-all glow-indigo">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-xs font-semibold text-slate-400 uppercase tracking-wider">
              Kubernetes Pods
            </CardTitle>
            <Server className="h-5 w-5 text-indigo-400" />
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-extrabold text-white">{pods.length}</div>
            <div className="mt-2 flex items-center text-xs text-slate-400">
              <span className="flex items-center text-emerald-400 font-semibold mr-2">
                <span className="h-2 w-2 rounded-full bg-emerald-400 mr-1.5 animate-pulse" />
                {runningPods} active
              </span>
              <span>across all namespaces</span>
            </div>
          </CardContent>
        </Card>

        {/* K8s Deployments */}
        <Card className="hover:border-indigo-500/40 transition-all">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-xs font-semibold text-slate-400 uppercase tracking-wider">
              Deployments
            </CardTitle>
            <Layers className="h-5 w-5 text-indigo-400" />
          </CardHeader>
          <CardContent>
            <div className="text-3xl font-extrabold text-white">{deployments.length}</div>
            <p className="mt-2 text-xs text-slate-400">Configured application workloads</p>
          </CardContent>
        </Card>
      </div>
    );
  };

  const renderPluginWidgets = (isCompact: boolean) => {
    if (dashboardWidgets.length === 0) {
      return (
        <div className="p-4 rounded-xl border border-dashed border-slate-800 text-center text-xs text-slate-500">
          Nenhum widget de plugin ativo no momento. Instale plugins com widgets na aba de Plugins.
        </div>
      );
    }

    const widgetMap = new Map(dashboardWidgets.map((item) => [getWidgetKey(item), item]));
    const orderedKeys = [
      ...widgetOrder.filter((k) => widgetMap.has(k)),
      ...dashboardWidgets.map(getWidgetKey).filter((k) => !widgetOrder.includes(k)),
    ];
    const visibleWidgetKeys = orderedKeys.filter((k) => widgetVisibility[k] !== false);
    const hiddenWidgetsCount = orderedKeys.length - visibleWidgetKeys.length;

    if (isCompact) {
      return (
        <div className="flex items-center justify-between p-3 rounded-lg bg-slate-900/90 border border-slate-800 text-xs">
          <div className="flex items-center gap-2 text-slate-300">
            <Puzzle className="h-4 w-4 text-sky-400" />
            <span>
              <strong className="text-white">{visibleWidgetKeys.length}</strong> de {orderedKeys.length} widget(s) de
              plugins visíveis (
              {visibleWidgetKeys
                .map((k) => widgetMap.get(k)?.ext.title)
                .filter(Boolean)
                .join(", ") || "Nenhum visível"}
              )
            </span>
          </div>
          <div className="flex items-center gap-1.5">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setCustomizeModalOpen(true)}
              className="text-xs h-7 px-2"
            >
              Escolher / Reordenar
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => handleToggleCollapse("plugin_widgets")}
              className="text-xs text-sky-400 hover:text-sky-300 h-7"
            >
              Expandir
            </Button>
          </div>
        </div>
      );
    }

    return (
      <div className="space-y-3">
        {/* Sub-toolbar for managing individual plugin widgets */}
        <div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 rounded-lg bg-slate-900/50 border border-slate-800/70 text-xs">
          <span className="text-slate-400">
            Exibindo <strong className="text-slate-200">{visibleWidgetKeys.length}</strong> de{" "}
            <strong className="text-slate-200">{orderedKeys.length}</strong> widget(s) de plugins
            {hiddenWidgetsCount > 0 && (
              <span className="ml-1.5 text-amber-400">
                ({hiddenWidgetsCount} oculto{hiddenWidgetsCount > 1 ? "s" : ""})
              </span>
            )}
          </span>
          <div className="flex items-center gap-1.5">
            {hiddenWidgetsCount > 0 && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleSetAllWidgetVisibility(true)}
                className="text-[11px] h-7 px-2.5"
              >
                Exibir Todos os Widgets
              </Button>
            )}
            <Button
              variant="outline"
              size="sm"
              onClick={() => setCustomizeModalOpen(true)}
              className="text-[11px] h-7 px-2.5"
            >
              <SlidersHorizontal className="mr-1.5 h-3 w-3 text-sky-400" />
              Escolher / Reordenar Widgets
            </Button>
          </div>
        </div>

        {visibleWidgetKeys.length === 0 ? (
          <div className="p-6 rounded-xl border border-dashed border-slate-800 text-center space-y-3 bg-slate-950/30">
            <p className="text-xs text-slate-400">
              Todos os widgets de plugins foram ocultados individualmente.
            </p>
            <div className="flex items-center justify-center gap-2">
              <Button
                variant="default"
                size="sm"
                onClick={() => setCustomizeModalOpen(true)}
                className="text-xs"
              >
                Escolher Widgets para Exibir
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleSetAllWidgetVisibility(true)}
                className="text-xs"
              >
                Exibir Todos
              </Button>
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {visibleWidgetKeys.map((wKey, idx) => {
              const item = widgetMap.get(wKey);
              if (!item) return null;

              const isWCompact = Boolean(widgetCollapsed[wKey]);
              const isWDragging = draggedWidgetId === wKey;
              const isWDragOver = dragOverWidgetId === wKey;
              const orderIdx = orderedKeys.indexOf(wKey);

              return (
                <div
                  key={wKey}
                  draggable
                  onDragStart={(e) => handleWidgetDragStart(e, wKey)}
                  onDragOver={(e) => handleWidgetDragOver(e, wKey)}
                  onDragLeave={handleWidgetDragLeave}
                  onDrop={(e) => handleWidgetDrop(e, wKey)}
                  onDragEnd={handleWidgetDragEnd}
                  className={`transition-all duration-200 rounded-xl ${
                    isWDragging
                      ? "opacity-40 ring-2 ring-dashed ring-sky-500 scale-[0.99]"
                      : isWDragOver
                      ? "ring-2 ring-sky-500/70 scale-[1.005]"
                      : ""
                  }`}
                >
                  <Card className="border-slate-800 bg-slate-900/60 shadow-lg h-full">
                    <CardHeader className="pb-3 border-b border-slate-800/60 flex flex-row items-center justify-between gap-2 select-none">
                      <div className="flex items-center gap-2 min-w-0">
                        <button
                          type="button"
                          aria-label="Arrastar widget"
                          className="cursor-grab active:cursor-grabbing text-slate-500 hover:text-slate-300 p-1 rounded hover:bg-slate-800/60 transition shrink-0"
                          title="Arrastar para reordenar este widget"
                        >
                          <GripVertical className="h-4 w-4" />
                        </button>
                        <div className="min-w-0">
                          <CardTitle className="text-sm font-semibold text-white truncate">
                            {item.ext.title}
                          </CardTitle>
                          <CardDescription className="text-[11px] text-slate-400 truncate">
                            Plugin: {item.plugin.name}
                          </CardDescription>
                        </div>
                      </div>
                      <div className="flex items-center gap-1 shrink-0">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleMoveWidget(wKey, "up")}
                          disabled={orderIdx <= 0}
                          title="Mover widget para trás / cima"
                          className="h-7 w-7 p-0 text-slate-400 hover:text-white disabled:opacity-20"
                        >
                          <ArrowUp className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleMoveWidget(wKey, "down")}
                          disabled={orderIdx === -1 || orderIdx >= orderedKeys.length - 1}
                          title="Mover widget para frente / baixo"
                          className="h-7 w-7 p-0 text-slate-400 hover:text-white disabled:opacity-20"
                        >
                          <ArrowDown className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleToggleWidgetCollapse(wKey)}
                          title={isWCompact ? "Expandir widget" : "Compactar widget"}
                          className="h-7 w-7 p-0 text-slate-400 hover:text-sky-400"
                        >
                          {isWCompact ? (
                            <Maximize2 className="h-3.5 w-3.5" />
                          ) : (
                            <Minimize2 className="h-3.5 w-3.5" />
                          )}
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleToggleWidgetVisibility(wKey)}
                          title="Ocultar este widget de plugin"
                          className="h-7 w-7 p-0 text-slate-400 hover:text-rose-400"
                        >
                          <EyeOff className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </CardHeader>
                    {!isWCompact && (
                      <CardContent className="pt-4">
                        <PluginSurfaceRenderer
                          pluginId={item.plugin.plugin_id}
                          surface="widget"
                          contextParams={{ widgetId: item.ext.id }}
                        />
                      </CardContent>
                    )}
                  </Card>
                </div>
              );
            })}
          </div>
        )}
      </div>
    );
  };

  const renderInfraHealth = (isCompact: boolean) => {
    if (isCompact) {
      return (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 py-1">
          <div className="flex items-center justify-between p-3 rounded-lg bg-slate-900/90 border border-slate-800">
            <div className="flex items-center gap-2">
              <Cpu className="h-4 w-4 text-sky-400" />
              <span className="text-xs font-semibold text-white">Docker Engine</span>
            </div>
            {status?.docker_connected ? (
              <Badge variant="running" className="text-[10px]">Connected</Badge>
            ) : (
              <Badge variant="stopped" className="text-[10px]">Disconnected</Badge>
            )}
          </div>

          <div className="flex items-center justify-between p-3 rounded-lg bg-slate-900/90 border border-slate-800">
            <div className="flex items-center gap-2">
              <Activity className="h-4 w-4 text-indigo-400" />
              <span className="text-xs font-semibold text-white">Kubernetes</span>
            </div>
            {status?.k8s_connected ? (
              <Badge variant="k8s" className="text-[10px]">Connected</Badge>
            ) : (
              <Badge variant="stopped" className="text-[10px]">Unavailable</Badge>
            )}
          </div>

          <div className="flex items-center justify-between p-3 rounded-lg bg-slate-900/90 border border-slate-800">
            <div className="flex items-center gap-2">
              <Database className="h-4 w-4 text-emerald-400" />
              <span className="text-xs font-semibold text-white">PostgreSQL DB</span>
            </div>
            <Badge variant="running" className="text-[10px]">Healthy</Badge>
          </div>
        </div>
      );
    }

    return (
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {/* Docker Engine Card */}
        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <Cpu className="h-5 w-5 text-sky-400" />
                <CardTitle className="text-sm font-semibold">Docker Engine</CardTitle>
              </div>
              {status?.docker_connected ? (
                <Badge variant="running">Connected</Badge>
              ) : (
                <Badge variant="stopped">Disconnected</Badge>
              )}
            </div>
          </CardHeader>
          <CardContent className="space-y-2 text-xs">
            <div className="flex justify-between text-slate-400 border-b border-slate-800/80 pb-1.5">
              <span>Daemon Version:</span>
              <span className="font-mono text-slate-200">
                {status?.docker_version || "24.0.7-ce"}
              </span>
            </div>
            <div className="flex justify-between text-slate-400 border-b border-slate-800/80 pb-1.5">
              <span>Managed Containers:</span>
              <span className="font-mono text-slate-200">{containers.length}</span>
            </div>
            <div className="flex justify-between text-slate-400">
              <span>Socket Path:</span>
              <span className="font-mono text-slate-300">/var/run/docker.sock</span>
            </div>
          </CardContent>
        </Card>

        {/* Kubernetes Cluster Card */}
        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <Activity className="h-5 w-5 text-indigo-400" />
                <CardTitle className="text-sm font-semibold">Kubernetes Cluster</CardTitle>
              </div>
              {status?.k8s_connected ? (
                <Badge variant="k8s">Connected</Badge>
              ) : (
                <Badge variant="stopped">Unavailable</Badge>
              )}
            </div>
          </CardHeader>
          <CardContent className="space-y-2 text-xs">
            <div className="flex justify-between text-slate-400 border-b border-slate-800/80 pb-1.5">
              <span>Cluster Version:</span>
              <span className="font-mono text-slate-200">
                {status?.k8s_version || "v1.28.2"}
              </span>
            </div>
            <div className="flex justify-between text-slate-400 border-b border-slate-800/80 pb-1.5">
              <span>Discovered Pods:</span>
              <span className="font-mono text-slate-200">{pods.length}</span>
            </div>
            <div className="flex justify-between text-slate-400">
              <span>Config Source:</span>
              <span className="font-mono text-slate-300">InCluster / Kubeconfig</span>
            </div>
          </CardContent>
        </Card>

        {/* Persistence & Database */}
        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <Database className="h-5 w-5 text-emerald-400" />
                <CardTitle className="text-sm font-semibold">System Database</CardTitle>
              </div>
              <Badge variant="running">Healthy</Badge>
            </div>
          </CardHeader>
          <CardContent className="space-y-2 text-xs">
            <div className="flex justify-between text-slate-400 border-b border-slate-800/80 pb-1.5">
              <span>Database Engine:</span>
              <span className="font-mono text-slate-200">
                {status?.database_type || "PostgreSQL 16"}
              </span>
            </div>
            <div className="flex justify-between text-slate-400 border-b border-slate-800/80 pb-1.5">
              <span>Uptime:</span>
              <span className="font-mono text-slate-200">
                {status?.uptime || "Running"}
              </span>
            </div>
            <div className="flex justify-between text-slate-400">
              <span>Audit Logging:</span>
              <span className="text-emerald-400 font-semibold">Enabled</span>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  };

  const renderQuickNav = (isCompact: boolean) => {
    if (isCompact) {
      return (
        <div className="flex flex-wrap gap-2 py-1">
          <Link
            href="/containers"
            className="flex items-center gap-2 px-3 py-2 rounded-lg border border-slate-800 bg-slate-900/60 hover:bg-slate-800/70 hover:border-sky-500/50 text-xs font-medium text-slate-200 transition"
          >
            <Box className="h-4 w-4 text-sky-400" />
            <span>Gerenciar Containers</span>
          </Link>

          <Link
            href="/pods"
            className="flex items-center gap-2 px-3 py-2 rounded-lg border border-slate-800 bg-slate-900/60 hover:bg-slate-800/70 hover:border-indigo-500/50 text-xs font-medium text-slate-200 transition"
          >
            <Server className="h-4 w-4 text-indigo-400" />
            <span>Kubernetes Pods</span>
          </Link>

          <Link
            href="/logs"
            className="flex items-center gap-2 px-3 py-2 rounded-lg border border-slate-800 bg-slate-900/60 hover:bg-slate-800/70 hover:border-emerald-500/50 text-xs font-medium text-slate-200 transition"
          >
            <Terminal className="h-4 w-4 text-emerald-400" />
            <span>Live Log Stream</span>
          </Link>
        </div>
      );
    }

    return (
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <Link
          href="/containers"
          className="group flex items-center justify-between p-4 rounded-xl border border-slate-800 bg-slate-900/60 hover:bg-slate-800/70 hover:border-sky-500/50 transition-all"
        >
          <div className="flex items-center space-x-3">
            <div className="p-2.5 rounded-lg bg-sky-500/10 text-sky-400 group-hover:bg-sky-500/20">
              <Box className="h-5 w-5" />
            </div>
            <div>
              <p className="text-sm font-semibold text-white">Manage Containers</p>
              <p className="text-xs text-slate-400">Start, stop, and inspect</p>
            </div>
          </div>
          <ArrowRight className="h-4 w-4 text-slate-500 group-hover:text-white transition-transform group-hover:translate-x-1" />
        </Link>

        <Link
          href="/pods"
          className="group flex items-center justify-between p-4 rounded-xl border border-slate-800 bg-slate-900/60 hover:bg-slate-800/70 hover:border-indigo-500/50 transition-all"
        >
          <div className="flex items-center space-x-3">
            <div className="p-2.5 rounded-lg bg-indigo-500/10 text-indigo-400 group-hover:bg-indigo-500/20">
              <Server className="h-5 w-5" />
            </div>
            <div>
              <p className="text-sm font-semibold text-white">Kubernetes Pods</p>
              <p className="text-xs text-slate-400">Namespaces & statuses</p>
            </div>
          </div>
          <ArrowRight className="h-4 w-4 text-slate-500 group-hover:text-white transition-transform group-hover:translate-x-1" />
        </Link>

        <Link
          href="/logs"
          className="group flex items-center justify-between p-4 rounded-xl border border-slate-800 bg-slate-900/60 hover:bg-slate-800/70 hover:border-emerald-500/50 transition-all"
        >
          <div className="flex items-center space-x-3">
            <div className="p-2.5 rounded-lg bg-emerald-500/10 text-emerald-400 group-hover:bg-emerald-500/20">
              <Terminal className="h-5 w-5" />
            </div>
            <div>
              <p className="text-sm font-semibold text-white">Live Log Stream</p>
              <p className="text-xs text-slate-400">WebSocket terminal feed</p>
            </div>
          </div>
          <ArrowRight className="h-4 w-4 text-slate-500 group-hover:text-white transition-transform group-hover:translate-x-1" />
        </Link>
      </div>
    );
  };

  const renderAuditLogs = (isCompact: boolean) => {
    if (isCompact) {
      const latest = sortedAuditLogs[0];
      return (
        <div className="flex items-center justify-between p-3 rounded-lg bg-slate-900/90 border border-slate-800 text-xs">
          <div className="flex items-center gap-2 text-slate-300 truncate pr-2">
            <ShieldCheck className="h-4 w-4 text-sky-400 shrink-0" />
            <span className="truncate">
              Total de <strong className="text-white">{auditLogs.length}</strong> logs de auditoria
              {latest && (
                <span className="text-slate-400 ml-1.5">
                  (Última ação: <span className="font-mono text-slate-200">{latest.action}</span> por{" "}
                  <span className="text-white font-medium">{latest.username}</span> em{" "}
                  {formatDate(latest.created_at)})
                </span>
              )}
            </span>
          </div>
          <Link
            href="/audit"
            className="text-xs text-sky-400 hover:text-sky-300 font-medium shrink-0 flex items-center gap-1"
          >
            Ver todos <ArrowRight className="h-3 w-3" />
          </Link>
        </div>
      );
    }

    return (
      <div className="space-y-3">
        <div className="flex items-center justify-end">
          <Link
            href="/audit"
            className="text-xs text-sky-400 hover:text-sky-300 font-medium flex items-center gap-1"
          >
            View all logs <ArrowRight className="h-3 w-3" />
          </Link>
        </div>

        <Table aria-label="Recent operations audit trail">
          <TableHeader>
            <TableRow>
              <SortableTableHead
                sortKey="user"
                currentSortKey={auditSortKey}
                currentDirection={auditSortDir}
                onSort={handleAuditSort}
              >
                User
              </SortableTableHead>
              <SortableTableHead
                sortKey="action"
                currentSortKey={auditSortKey}
                currentDirection={auditSortDir}
                onSort={handleAuditSort}
              >
                Action
              </SortableTableHead>
              <SortableTableHead
                sortKey="resource"
                currentSortKey={auditSortKey}
                currentDirection={auditSortDir}
                onSort={handleAuditSort}
              >
                Resource
              </SortableTableHead>
              <TableHead>Resource ID</TableHead>
              <SortableTableHead
                sortKey="status"
                currentSortKey={auditSortKey}
                currentDirection={auditSortDir}
                onSort={handleAuditSort}
              >
                Status
              </SortableTableHead>
              <SortableTableHead
                sortKey="timestamp"
                currentSortKey={auditSortKey}
                currentDirection={auditSortDir}
                onSort={handleAuditSort}
              >
                Timestamp
              </SortableTableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sortedAuditLogs.length === 0 ? (
              <TableEmpty colSpan={6} message="No recent audit logs recorded." />
            ) : (
              sortedAuditLogs.slice(0, 6).map((log) => (
                <TableRow key={log.id}>
                  <TableCell className="font-medium text-white">{log.username}</TableCell>
                  <TableCell>
                    <Badge variant="outline" className="font-mono text-[11px]">
                      {log.action}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-slate-400 text-xs">
                    {log.resource_type}
                  </TableCell>
                  <TableCell className="font-mono text-xs text-slate-300 max-w-[150px] truncate">
                    {log.resource_id}
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={log.status} />
                  </TableCell>
                  <TableCell className="text-slate-400 text-xs">
                    {formatDate(log.created_at)}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    );
  };

  // Section render router
  const renderSectionContent = (id: string, isCompact: boolean) => {
    switch (id) {
      case "metrics":
        return renderMetrics(isCompact);
      case "plugin_widgets":
        return renderPluginWidgets(isCompact);
      case "infra_health":
        return renderInfraHealth(isCompact);
      case "quick_nav":
        return renderQuickNav(isCompact);
      case "audit_logs":
        return renderAuditLogs(isCompact);
      default:
        return null;
    }
  };

  const hiddenBlocksCount = order.filter((id) => visibility[id] === false).length;
  const hiddenWidgetsCount = dashboardWidgets.filter(
    (item) => widgetVisibility[getWidgetKey(item)] === false
  ).length;
  const hiddenCount = hiddenBlocksCount + hiddenWidgetsCount;
  const visibleItems = order.filter((id) => visibility[id] !== false);

  return (
    <div className="space-y-6 animate-in fade-in duration-300 pb-12">
      {/* Top Banner, Refresh, Pull Image & Customize Layout */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Dashboard Overview</h1>
          <p className="text-xs text-slate-400 mt-1">
            Real-time status of Docker engine, Kubernetes workloads and cluster operations.
          </p>
        </div>
        <div className="flex items-center space-x-2 flex-wrap">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setCustomizeModalOpen(true)}
            className="text-xs border-slate-700 bg-slate-900/80 hover:bg-slate-800 text-slate-200"
          >
            <SlidersHorizontal className="mr-1.5 h-3.5 w-3.5 text-sky-400" />
            Personalizar
            {hiddenCount > 0 && (
              <span className="ml-1.5 px-1.5 py-0.2 rounded-full bg-slate-800 text-slate-400 text-[10px] border border-slate-700">
                {hiddenCount} oculto{hiddenCount > 1 ? "s" : ""}
              </span>
            )}
          </Button>

          <Button
            variant="outline"
            size="sm"
            onClick={fetchDashboardData}
            isLoading={isLoading}
            className="text-xs"
          >
            <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
            Refresh
          </Button>

          <Button
            variant="docker"
            size="sm"
            onClick={() => setPullModalOpen(true)}
            className="text-xs"
          >
            <Download className="mr-1.5 h-3.5 w-3.5" />
            Pull Image
          </Button>
        </div>
      </div>

      {/* Main Dynamic Drag-and-Drop & Collapsible Dashboard Canvas */}
      {visibleItems.length === 0 ? (
        <div className="p-12 rounded-2xl border border-dashed border-slate-800 text-center space-y-4 bg-slate-950/40">
          <LayoutGrid className="h-10 w-10 text-slate-600 mx-auto" />
          <div>
            <h3 className="text-sm font-semibold text-white">Todos os blocos estão ocultos</h3>
            <p className="text-xs text-slate-400 max-w-sm mx-auto mt-1">
              Você ocultou todos os itens da dashboard principal. Reabra a personalização para escolher quais exibir.
            </p>
          </div>
          <div className="flex items-center justify-center gap-2 pt-2">
            <Button
              variant="default"
              size="sm"
              onClick={() => setCustomizeModalOpen(true)}
              className="text-xs"
            >
              <SlidersHorizontal className="mr-1.5 h-3.5 w-3.5" />
              Personalizar Dashboard
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={handleResetLayout}
              className="text-xs"
            >
              Restaurar Padrão
            </Button>
          </div>
        </div>
      ) : (
        <div className="space-y-6">
          {visibleItems.map((id, index) => {
            const def = itemDefinitions.find((d) => d.id === id);
            if (!def) return null;

            const isCompact = Boolean(collapsed[id]);
            const isDragging = draggedId === id;
            const isDragOver = dragOverId === id;
            const isFirst = index === 0;
            const isLast = index === visibleItems.length - 1;

            return (
              <section
                key={id}
                draggable
                onDragStart={(e) => handleDragStart(e, id)}
                onDragOver={(e) => handleDragOver(e, id)}
                onDragLeave={handleDragLeave}
                onDrop={(e) => handleDrop(e, id)}
                onDragEnd={handleDragEnd}
                className={`rounded-2xl border transition-all duration-200 p-4 ${
                  isDragging
                    ? "opacity-40 border-dashed border-sky-500 scale-[0.99]"
                    : isDragOver
                    ? "border-sky-500/80 bg-slate-800/40 ring-2 ring-sky-500/30 scale-[1.005]"
                    : "border-slate-800/80 bg-slate-950/30 hover:border-slate-700/80"
                }`}
              >
                {/* Section Header Controls */}
                <div className="flex items-center justify-between pb-3 border-b border-slate-800/60 mb-4 select-none">
                  <div className="flex items-center gap-2.5 min-w-0">
                    <button
                      type="button"
                      aria-label="Arrastar bloco"
                      className="cursor-grab active:cursor-grabbing text-slate-500 hover:text-slate-300 p-1 rounded hover:bg-slate-800/60 transition"
                      title="Arrastar e soltar para reordenar"
                    >
                      <GripVertical className="h-4 w-4" />
                    </button>
                    <div className="flex items-center gap-2 min-w-0">
                      <div className="text-sky-400 shrink-0">{def.icon}</div>
                      <h2 className="text-sm font-semibold text-white tracking-wide truncate">
                        {def.title}
                      </h2>
                    </div>
                    {isCompact && (
                      <Badge variant="outline" className="text-[10px] text-slate-400 py-0 px-1.5 uppercase">
                        Compacto
                      </Badge>
                    )}
                  </div>

                  {/* Header Actions */}
                  <div className="flex items-center gap-1 shrink-0">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleMove(id, "up")}
                      disabled={isFirst}
                      title="Mover para cima"
                      className="h-7 w-7 p-0 text-slate-400 hover:text-white disabled:opacity-20"
                    >
                      <ArrowUp className="h-3.5 w-3.5" />
                    </Button>

                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleMove(id, "down")}
                      disabled={isLast}
                      title="Mover para baixo"
                      className="h-7 w-7 p-0 text-slate-400 hover:text-white disabled:opacity-20"
                    >
                      <ArrowDown className="h-3.5 w-3.5" />
                    </Button>

                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleToggleCollapse(id)}
                      title={isCompact ? "Expandir visualização" : "Compactar visualização"}
                      className="h-7 w-7 p-0 text-slate-400 hover:text-sky-400"
                    >
                      {isCompact ? (
                        <Maximize2 className="h-3.5 w-3.5" />
                      ) : (
                        <Minimize2 className="h-3.5 w-3.5" />
                      )}
                    </Button>

                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleToggleVisibility(id)}
                      title="Ocultar item da dashboard"
                      className="h-7 w-7 p-0 text-slate-400 hover:text-rose-400"
                    >
                      <EyeOff className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>

                {/* Section Content */}
                <div className="transition-all duration-300">
                  {renderSectionContent(id, isCompact)}
                </div>
              </section>
            );
          })}
        </div>
      )}

      {/* Pull Image Modal */}
      <PullImageModal
        isOpen={pullModalOpen}
        onClose={() => setPullModalOpen(false)}
        onSuccess={fetchDashboardData}
      />

      {/* Customize Dashboard Modal */}
      <CustomizeDashboardModal
        isOpen={customizeModalOpen}
        onClose={() => setCustomizeModalOpen(false)}
        definitions={itemDefinitions}
        order={order}
        visibility={visibility}
        collapsed={collapsed}
        onReorder={handleReorder}
        onToggleVisibility={handleToggleVisibility}
        onToggleCollapse={handleToggleCollapse}
        onSetAllVisibility={handleSetAllVisibility}
        onSetAllCollapse={handleSetAllCollapse}
        onReset={handleResetLayout}
        pluginWidgets={pluginWidgetDefinitions}
        widgetOrder={widgetOrder}
        widgetVisibility={widgetVisibility}
        widgetCollapsed={widgetCollapsed}
        onReorderWidgets={handleReorderWidgets}
        onToggleWidgetVisibility={handleToggleWidgetVisibility}
        onToggleWidgetCollapse={handleToggleWidgetCollapse}
        onSetAllWidgetVisibility={handleSetAllWidgetVisibility}
      />
    </div>
  );
}
