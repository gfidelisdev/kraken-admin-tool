"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import {
  Layers,
  RotateCw,
  Sliders,
  Search,
  RefreshCw,
  AlertCircle,
  CheckCircle2,
  Clock,
} from "lucide-react";
import { api } from "@/lib/api";
import { K8sDeployment } from "@/types";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Badge } from "@/components/ui/Badge";
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
import { ScaleDeploymentModal } from "@/components/modals/ScaleDeploymentModal";
import { ConfirmActionModal } from "@/components/modals/ConfirmActionModal";
import { useServer } from "@/context/ServerContext";

export default function DeploymentsPage() {
  const { activeServer } = useServer();
  const [deployments, setDeployments] = useState<K8sDeployment[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [selectedNamespace, setSelectedNamespace] = useState<string>("all");
  const [search, setSearch] = useState("");
  const [error, setError] = useState<string | null>(null);

  // Modals state
  const [scaleTarget, setScaleTarget] = useState<K8sDeployment | null>(null);
  const [restartTarget, setRestartTarget] = useState<K8sDeployment | null>(null);

  const fetchDeployments = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const url =
        selectedNamespace && selectedNamespace !== "all"
          ? `/api/k8s/deployments?namespace=${encodeURIComponent(selectedNamespace)}`
          : "/api/k8s/deployments";
      const data = await api.get<K8sDeployment[]>(url);
      setDeployments(Array.isArray(data) ? data : []);
    } catch (err: any) {
      setError(err?.message || "Failed to load Kubernetes deployments");
    } finally {
      setIsLoading(false);
    }
  }, [selectedNamespace]);

  useEffect(() => {
    fetchDeployments();
  }, [fetchDeployments, activeServer?.id]);

  const handleScale = async (namespace: string, name: string, replicas: number) => {
    await api.post(
      `/api/k8s/deployments/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/scale`,
      { replicas }
    );
    await fetchDeployments();
  };

  const handleRolloutRestart = async () => {
    if (!restartTarget) return;
    try {
      await api.post(
        `/api/k8s/deployments/${encodeURIComponent(
          restartTarget.namespace
        )}/${encodeURIComponent(restartTarget.name)}/restart`
      );
      setRestartTarget(null);
      await fetchDeployments();
    } catch (err: any) {
      setError(err?.message || "Failed to trigger rollout restart");
    }
  };

  const namespaceOptions = useMemo(() => {
    const set = new Set<string>(["default", "kube-system"]);
    deployments.forEach((d) => {
      if (d.namespace) set.add(d.namespace);
    });
    return [
      { label: "All Namespaces", value: "all" },
      ...Array.from(set).map((ns) => ({ label: ns, value: ns })),
    ];
  }, [deployments]);

  const filteredDeployments = useMemo(() => {
    if (!search.trim()) return deployments;
    const q = search.toLowerCase();
    return deployments.filter((d) => {
      const nameMatch = d.name?.toLowerCase().includes(q);
      const imgMatch = d.images?.some((i) => i.toLowerCase().includes(q));
      return nameMatch || imgMatch;
    });
  }, [deployments, search]);

  return (
    <div className="space-y-6 animate-in fade-in duration-300">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <div className="flex items-center space-x-2">
            <Layers className="h-6 w-6 text-indigo-400" />
            <h1 className="text-2xl font-bold tracking-tight text-white">
              Kubernetes Deployments
            </h1>
            {activeServer && (
              <Badge variant="outline" className="border-indigo-500/30 text-indigo-400 bg-indigo-500/10 text-[11px]">
                {activeServer.name}
              </Badge>
            )}
          </div>
          <p className="text-xs text-slate-400 mt-1">
            Declarative application workloads, replica scaling and rolling update restarts.
          </p>
        </div>

        <Button
          variant="outline"
          size="sm"
          onClick={fetchDeployments}
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

      {/* Namespace Filter & Search Bar */}
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
            placeholder="Search deployment by name or image..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            icon={<Search className="h-4 w-4 text-slate-500" />}
          />
        </div>
      </div>

      {/* Deployments Table */}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Deployment</TableHead>
            <TableHead>Namespace</TableHead>
            <TableHead>Replicas Status</TableHead>
            <TableHead>Workload Images</TableHead>
            <TableHead>Age</TableHead>
            <TableHead className="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {filteredDeployments.length === 0 ? (
            <TableEmpty
              colSpan={6}
              icon={<Layers className="h-8 w-8" />}
              message={
                deployments.length === 0
                  ? "No Kubernetes deployments found in this namespace."
                  : "No deployments match your search."
              }
            />
          ) : (
            filteredDeployments.map((dep) => {
              const isHealthy =
                dep.ready_replicas === dep.desired_replicas && dep.desired_replicas > 0;

              return (
                <TableRow key={`${dep.namespace}-${dep.name}`}>
                  {/* Deployment Name */}
                  <TableCell>
                    <p className="font-semibold text-white text-xs">{dep.name}</p>
                  </TableCell>

                  {/* Namespace */}
                  <TableCell>
                    <span className="font-mono text-xs text-indigo-400 bg-indigo-500/10 px-2 py-0.5 rounded border border-indigo-500/20">
                      {dep.namespace}
                    </span>
                  </TableCell>

                  {/* Replicas status */}
                  <TableCell>
                    <div className="space-y-1">
                      <div className="flex items-center space-x-2">
                        <span
                          className={`font-mono text-xs font-semibold ${
                            isHealthy ? "text-emerald-400" : "text-amber-400"
                          }`}
                        >
                          {dep.ready_replicas} / {dep.desired_replicas} Ready
                        </span>
                        <span className="text-[11px] text-slate-500 font-mono">
                          ({dep.available_replicas} Available)
                        </span>
                      </div>
                      {/* Visual replica indicator bar */}
                      <div className="h-1.5 w-24 bg-slate-800 rounded-full overflow-hidden">
                        <div
                          className={`h-full rounded-full transition-all duration-300 ${
                            isHealthy ? "bg-emerald-400" : "bg-amber-400"
                          }`}
                          style={{
                            width: `${
                              dep.desired_replicas > 0
                                ? Math.min(
                                    100,
                                    (dep.ready_replicas / dep.desired_replicas) * 100
                                  )
                                : 0
                            }%`,
                          }}
                        />
                      </div>
                    </div>
                  </TableCell>

                  {/* Images */}
                  <TableCell>
                    <div className="flex flex-col space-y-1">
                      {dep.images?.map((img, idx) => (
                        <span
                          key={idx}
                          className="font-mono text-xs text-slate-300 max-w-[220px] truncate"
                          title={img}
                        >
                          {img}
                        </span>
                      ))}
                    </div>
                  </TableCell>

                  {/* Age */}
                  <TableCell className="text-xs text-slate-400 font-mono">
                    {dep.age || "-"}
                  </TableCell>

                  {/* Actions */}
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end space-x-1.5">
                      {/* Scale */}
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-8 text-xs text-sky-400 hover:text-sky-300"
                        title="Scale Replicas"
                        onClick={() => setScaleTarget(dep)}
                      >
                        <Sliders className="h-3.5 w-3.5 mr-1" />
                        Scale
                      </Button>

                      {/* Rollout Restart */}
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 text-indigo-400 hover:text-indigo-300"
                        title="Rollout Restart"
                        onClick={() => setRestartTarget(dep)}
                      >
                        <RotateCw className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              );
            })
          )}
        </TableBody>
      </Table>

      {/* Scale Deployment Modal */}
      <ScaleDeploymentModal
        isOpen={!!scaleTarget}
        onClose={() => setScaleTarget(null)}
        deployment={scaleTarget}
        onScale={handleScale}
      />

      {/* Rollout Restart Confirmation Modal */}
      <ConfirmActionModal
        isOpen={!!restartTarget}
        onClose={() => setRestartTarget(null)}
        title="Rollout Restart Deployment"
        description={`Triggering a rollout restart for "${restartTarget?.name}" in namespace "${restartTarget?.namespace}" will recreate all pods in a rolling fashion with zero downtime.`}
        confirmLabel="Rollout Restart"
        confirmVariant="k8s"
        onConfirm={handleRolloutRestart}
      />
    </div>
  );
}
