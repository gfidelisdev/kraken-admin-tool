"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import Link from "next/link";
import {
  Server,
  Trash2,
  FileText,
  Search,
  RefreshCw,
  AlertCircle,
  Filter,
  CheckCircle2,
} from "lucide-react";
import { api } from "@/lib/api";
import { K8sPod } from "@/types";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { StatusBadge, Badge } from "@/components/ui/Badge";
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
  TableEmpty,
} from "@/components/ui/Table";
import { Select } from "@/components/ui/Select";
import { ConfirmActionModal } from "@/components/modals/ConfirmActionModal";
import { useServer } from "@/context/ServerContext";

export default function PodsPage() {
  const { activeServer } = useServer();
  const [pods, setPods] = useState<K8sPod[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [selectedNamespace, setSelectedNamespace] = useState<string>("all");
  const [search, setSearch] = useState("");
  const [error, setError] = useState<string | null>(null);

  const [deleteTarget, setDeleteTarget] = useState<K8sPod | null>(null);

  const fetchPods = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const url =
        selectedNamespace && selectedNamespace !== "all"
          ? `/api/k8s/pods?namespace=${encodeURIComponent(selectedNamespace)}`
          : "/api/k8s/pods";
      const data = await api.get<K8sPod[]>(url);
      setPods(Array.isArray(data) ? data : []);
    } catch (err: any) {
      setError(err?.message || "Failed to load Kubernetes pods");
    } finally {
      setIsLoading(false);
    }
  }, [selectedNamespace]);

  useEffect(() => {
    fetchPods();
  }, [fetchPods, activeServer?.id]);

  const handleDeletePod = async () => {
    if (!deleteTarget) return;
    try {
      await api.delete(
        `/api/k8s/pods/${encodeURIComponent(deleteTarget.namespace)}/${encodeURIComponent(
          deleteTarget.name
        )}`
      );
      setDeleteTarget(null);
      await fetchPods();
    } catch (err: any) {
      setError(err?.message || "Failed to delete Kubernetes pod");
    }
  };

  // Derive unique namespaces from pods list or fallback
  const namespaceOptions = useMemo(() => {
    const set = new Set<string>(["default", "kube-system", "kube-public"]);
    pods.forEach((p) => {
      if (p.namespace) set.add(p.namespace);
    });
    return [
      { label: "All Namespaces", value: "all" },
      ...Array.from(set).map((ns) => ({ label: ns, value: ns })),
    ];
  }, [pods]);

  // Search filter
  const filteredPods = useMemo(() => {
    if (!search.trim()) return pods;
    const q = search.toLowerCase();
    return pods.filter((p) => {
      const nameMatch = p.name?.toLowerCase().includes(q);
      const nodeMatch = p.node?.toLowerCase().includes(q);
      const ipMatch = p.ip?.toLowerCase().includes(q);
      return nameMatch || nodeMatch || ipMatch;
    });
  }, [pods, search]);

  return (
    <div className="space-y-6 animate-in fade-in duration-300">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <div className="flex items-center space-x-2">
            <Server className="h-6 w-6 text-indigo-400" />
            <h1 className="text-2xl font-bold tracking-tight text-white">Kubernetes Pods</h1>
            {activeServer && (
              <Badge variant="outline" className="border-indigo-500/30 text-indigo-400 bg-indigo-500/10 text-[11px]">
                {activeServer.name}
              </Badge>
            )}
          </div>
          <p className="text-xs text-slate-400 mt-1">
            Real-time status, restart counts and container telemetry across all cluster nodes.
          </p>
        </div>

        <Button
          variant="outline"
          size="sm"
          onClick={fetchPods}
          isLoading={isLoading}
          className="text-xs"
        >
          <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          Refresh
        </Button>
      </div>

      {error && (
        <div className="flex items-center space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3.5 text-xs text-rose-400">
          <AlertCircle className="h-4 w-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* Namespace Filter & Search Row */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div className="w-full sm:w-56">
          <Select
            value={selectedNamespace}
            onChange={(e) => setSelectedNamespace(e.target.value)}
            options={namespaceOptions}
          />
        </div>

        <div className="w-full sm:w-80">
          <Input
            placeholder="Search pod by name, node or IP..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            icon={<Search className="h-4 w-4 text-slate-500" />}
          />
        </div>
      </div>

      {/* Pods Table */}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Pod Name</TableHead>
            <TableHead>Namespace</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Restarts</TableHead>
            <TableHead>Node & IP</TableHead>
            <TableHead>Age</TableHead>
            <TableHead className="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {filteredPods.length === 0 ? (
            <TableEmpty
              colSpan={7}
              icon={<Server className="h-8 w-8" />}
              message={
                pods.length === 0
                  ? "No Kubernetes pods found in this namespace."
                  : "No pods match your search query."
              }
            />
          ) : (
            filteredPods.map((pod) => (
              <TableRow key={`${pod.namespace}-${pod.name}`}>
                {/* Pod Name */}
                <TableCell>
                  <p className="font-semibold text-white text-xs">{pod.name}</p>
                  {pod.containers && pod.containers.length > 0 && (
                    <p className="text-[10px] text-slate-500 font-mono">
                      Containers: {pod.containers.join(", ")}
                    </p>
                  )}
                </TableCell>

                {/* Namespace */}
                <TableCell>
                  <span className="font-mono text-xs text-indigo-400 bg-indigo-500/10 px-2 py-0.5 rounded border border-indigo-500/20">
                    {pod.namespace}
                  </span>
                </TableCell>

                {/* Status */}
                <TableCell>
                  <StatusBadge status={pod.status} />
                </TableCell>

                {/* Restarts */}
                <TableCell className="font-mono text-xs">
                  <span
                    className={
                      pod.restarts > 0
                        ? "text-amber-400 font-semibold"
                        : "text-slate-400"
                    }
                  >
                    {pod.restarts}
                  </span>
                </TableCell>

                {/* Node & IP */}
                <TableCell className="text-xs text-slate-300 font-mono">
                  <div>{pod.node || "N/A"}</div>
                  <div className="text-[11px] text-slate-500">{pod.ip || "-"}</div>
                </TableCell>

                {/* Age */}
                <TableCell className="text-xs text-slate-400 font-mono">
                  {pod.age || "-"}
                </TableCell>

                {/* Actions */}
                <TableCell className="text-right">
                  <div className="flex items-center justify-end space-x-1.5">
                    {/* Log viewer link */}
                    <Link
                      href={`/logs?source=k8s&namespace=${encodeURIComponent(
                        pod.namespace
                      )}&pod=${encodeURIComponent(pod.name)}`}
                      title="Stream pod logs"
                    >
                      <Button variant="ghost" size="icon" className="h-8 w-8 text-indigo-400">
                        <FileText className="h-4 w-4" />
                      </Button>
                    </Link>

                    {/* Delete Pod */}
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-rose-400 hover:text-rose-300"
                      title="Delete pod"
                      onClick={() => setDeleteTarget(pod)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>

      {/* Delete Pod Confirmation Modal */}
      <ConfirmActionModal
        isOpen={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        title="Delete Kubernetes Pod"
        description={`Are you sure you want to terminate pod "${deleteTarget?.name}" in namespace "${deleteTarget?.namespace}"? If managed by a ReplicaSet/Deployment, a new pod will be automatically spawned.`}
        confirmLabel="Terminate Pod"
        confirmVariant="destructive"
        onConfirm={handleDeletePod}
      />
    </div>
  );
}
