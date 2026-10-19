"use client";

import React, { useState, useEffect, useCallback } from "react";
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
} from "lucide-react";
import { api } from "@/lib/api";
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
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell, TableEmpty } from "@/components/ui/Table";
import { useServer } from "@/context/ServerContext";
import { PullImageModal } from "@/components/modals/PullImageModal";
import { formatDate } from "@/lib/utils";

export default function DashboardOverviewPage() {
  const { activeServer } = useServer();
  const [status, setStatus] = useState<SystemStatus | null>(null);
  const [containers, setContainers] = useState<DockerContainer[]>([]);
  const [images, setImages] = useState<DockerImage[]>([]);
  const [pods, setPods] = useState<K8sPod[]>([]);
  const [deployments, setDeployments] = useState<K8sDeployment[]>([]);
  const [auditLogs, setAuditLogs] = useState<AuditLog[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [pullModalOpen, setPullModalOpen] = useState(false);

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

  return (
    <div className="space-y-8 animate-in fade-in duration-300">
      {/* Top Banner & Refresh */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Dashboard Overview</h1>
          <p className="text-xs text-slate-400 mt-1">
            Real-time status of Docker engine, Kubernetes workloads and cluster operations.
          </p>
        </div>
        <div className="flex items-center space-x-2">
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

      {/* Metrics Row */}
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
            <div className="text-3xl font-extrabold text-white">
              {containers.length}
            </div>
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
            <div className="text-3xl font-extrabold text-white">
              {images.length}
            </div>
            <p className="mt-2 text-xs text-slate-400">
              Cached images in local Docker repository
            </p>
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
            <div className="text-3xl font-extrabold text-white">
              {pods.length}
            </div>
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
            <div className="text-3xl font-extrabold text-white">
              {deployments.length}
            </div>
            <p className="mt-2 text-xs text-slate-400">
              Configured application workloads
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Cluster & Engine Infrastructure Health */}
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

      {/* Quick Navigation Cards */}
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

      {/* Recent Audit Activities */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <ShieldCheck className="h-5 w-5 text-sky-400" />
            <h2 className="text-lg font-bold text-white">Recent Operations Audit</h2>
          </div>
          <Link
            href="/audit"
            className="text-xs text-sky-400 hover:text-sky-300 font-medium flex items-center gap-1"
          >
            View all logs <ArrowRight className="h-3 w-3" />
          </Link>
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>User</TableHead>
              <TableHead>Action</TableHead>
              <TableHead>Resource</TableHead>
              <TableHead>Resource ID</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Timestamp</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {auditLogs.length === 0 ? (
              <TableEmpty colSpan={6} message="No recent audit logs recorded." />
            ) : (
              auditLogs.slice(0, 6).map((log) => (
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

      {/* Pull Image Modal */}
      <PullImageModal
        isOpen={pullModalOpen}
        onClose={() => setPullModalOpen(false)}
        onSuccess={fetchDashboardData}
      />
    </div>
  );
}
