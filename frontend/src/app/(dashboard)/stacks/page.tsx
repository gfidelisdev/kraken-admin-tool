"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import {
  Layers,
  Play,
  Square,
  RotateCw,
  Trash2,
  Plus,
  RefreshCw,
  AlertCircle,
  ChevronDown,
  ChevronRight,
  Box,
  FileCode,
  CheckCircle2,
  XCircle,
  HelpCircle,
} from "lucide-react";
import {
  getComposeStacks,
  startComposeStack,
  stopComposeStack,
  restartComposeStack,
  removeComposeStack,
  deployComposeStack,
} from "@/lib/api";
import { ComposeStack, DeployStackRequest } from "@/types";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { StatusBadge, Badge } from "@/components/ui/Badge";
import { ConfirmActionModal } from "@/components/modals/ConfirmActionModal";
import { useServer } from "@/context/ServerContext";

export default function StacksPage() {
  const { activeServer } = useServer();
  const [stacks, setStacks] = useState<ComposeStack[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [actionLoadingName, setActionLoadingName] = useState<string | null>(null);
  const [expandedStacks, setExpandedStacks] = useState<Record<string, boolean>>({});

  // Deploy modal state
  const [isDeployModalOpen, setIsDeployModalOpen] = useState(false);
  const [deployName, setDeployName] = useState("");
  const [deployYaml, setDeployYaml] = useState("");
  const [isDeploying, setIsDeploying] = useState(false);
  const [deployError, setDeployError] = useState<string | null>(null);

  // Delete modal state
  const [deleteTarget, setDeleteTarget] = useState<ComposeStack | null>(null);
  const [removeVolumes, setRemoveVolumes] = useState(false);

  const fetchStacks = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const data = await getComposeStacks(activeServer?.id);
      setStacks(Array.isArray(data) ? data : []);
      // Expand all by default if <= 5 stacks
      if (Array.isArray(data) && data.length <= 5) {
        const initialExpanded: Record<string, boolean> = {};
        data.forEach((s) => {
          initialExpanded[s.name] = true;
        });
        setExpandedStacks(initialExpanded);
      }
    } catch (err: any) {
      setError(err?.message || "Failed to load Docker Compose stacks");
    } finally {
      setIsLoading(false);
    }
  }, [activeServer?.id]);

  useEffect(() => {
    fetchStacks();
  }, [fetchStacks]);

  const toggleExpand = (name: string) => {
    setExpandedStacks((prev) => ({
      ...prev,
      [name]: !prev[name],
    }));
  };

  const handleStart = async (name: string) => {
    setActionLoadingName(name);
    try {
      await startComposeStack(name, activeServer?.id);
      await fetchStacks();
    } catch (err: any) {
      setError(err?.message || `Failed to start stack ${name}`);
    } finally {
      setActionLoadingName(null);
    }
  };

  const handleStop = async (name: string) => {
    setActionLoadingName(name);
    try {
      await stopComposeStack(name, activeServer?.id);
      await fetchStacks();
    } catch (err: any) {
      setError(err?.message || `Failed to stop stack ${name}`);
    } finally {
      setActionLoadingName(null);
    }
  };

  const handleRestart = async (name: string) => {
    setActionLoadingName(name);
    try {
      await restartComposeStack(name, activeServer?.id);
      await fetchStacks();
    } catch (err: any) {
      setError(err?.message || `Failed to restart stack ${name}`);
    } finally {
      setActionLoadingName(null);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setActionLoadingName(deleteTarget.name);
    try {
      await removeComposeStack(deleteTarget.name, activeServer?.id, removeVolumes);
      setDeleteTarget(null);
      await fetchStacks();
    } catch (err: any) {
      setError(err?.message || `Failed to remove stack ${deleteTarget.name}`);
    } finally {
      setActionLoadingName(null);
    }
  };

  const handleDeploy = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!deployName.trim() || !deployYaml.trim()) {
      setDeployError("Stack name and Compose YAML are required");
      return;
    }

    setIsDeploying(true);
    setDeployError(null);
    try {
      const payload: DeployStackRequest = {
        name: deployName.trim(),
        compose_yaml: deployYaml,
      };
      await deployComposeStack(payload, activeServer?.id);
      setIsDeployModalOpen(false);
      setDeployName("");
      setDeployYaml("");
      await fetchStacks();
    } catch (err: any) {
      setDeployError(err?.message || "Failed to deploy stack");
    } finally {
      setIsDeploying(false);
    }
  };

  const filteredStacks = useMemo(() => {
    if (!search.trim()) return stacks;
    const q = search.toLowerCase();
    return stacks.filter(
      (s) =>
        s.name.toLowerCase().includes(q) ||
        s.services.some((svc) =>
          svc.name.toLowerCase().includes(q) || svc.image.toLowerCase().includes(q)
        )
    );
  }, [stacks, search]);

  const getStatusBadge = (status: string) => {
    switch (status) {
      case "running":
        return <Badge variant="running">All Running</Badge>;
      case "partially_running":
        return <Badge variant="pending">Partially Running</Badge>;
      case "stopped":
        return <Badge variant="stopped">Stopped</Badge>;
      default:
        return <Badge variant="default">{status}</Badge>;
    }
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white flex items-center gap-2.5">
            <Layers className="h-7 w-7 text-sky-400" />
            Docker Compose Stacks
          </h1>
          <p className="text-sm text-slate-400 mt-1">
            Manage multi-container application stacks grouped by Docker Compose projects
            {activeServer ? (
              <span className="ml-1 text-slate-300 font-medium">on {activeServer.name}</span>
            ) : null}
          </p>
        </div>

        <div className="flex items-center gap-3">
          <Button
            variant="outline"
            size="sm"
            onClick={fetchStacks}
            disabled={isLoading}
            className="border-slate-700 bg-slate-900/50 hover:bg-slate-800 text-slate-200"
          >
            <RefreshCw className={`h-4 w-4 mr-2 ${isLoading ? "animate-spin" : ""}`} />
            Refresh
          </Button>

          <Button
            size="sm"
            onClick={() => setIsDeployModalOpen(true)}
            className="bg-sky-500 hover:bg-sky-400 text-white shadow-lg shadow-sky-500/20"
          >
            <Plus className="h-4 w-4 mr-2" />
            Deploy Stack
          </Button>
        </div>
      </div>

      {/* Error alert */}
      {error && (
        <div className="p-4 rounded-xl bg-rose-500/10 border border-rose-500/20 text-rose-300 flex items-center justify-between text-sm">
          <div className="flex items-center gap-2.5">
            <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
            <span>{error}</span>
          </div>
          <button
            onClick={() => setError(null)}
            className="text-rose-400 hover:text-rose-200 text-xs font-semibold"
          >
            Dismiss
          </button>
        </div>
      )}

      {/* Filter / Search Bar */}
      <div className="flex items-center gap-4 bg-slate-900/60 p-3 rounded-xl border border-slate-800">
        <Input
          placeholder="Filter stacks or services..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="max-w-md bg-slate-950/70 border-slate-800 text-slate-200 placeholder:text-slate-500"
        />
        <div className="text-xs text-slate-400 ml-auto">
          {filteredStacks.length} stack{filteredStacks.length === 1 ? "" : "s"} found
        </div>
      </div>

      {/* Stacks List */}
      {isLoading && stacks.length === 0 ? (
        <div className="flex flex-col items-center justify-center p-12 text-slate-400 border border-slate-800 rounded-2xl bg-slate-900/30">
          <RefreshCw className="h-8 w-8 animate-spin text-sky-400 mb-3" />
          <p className="text-sm">Discovering Compose stacks...</p>
        </div>
      ) : filteredStacks.length === 0 ? (
        <div className="flex flex-col items-center justify-center p-12 text-slate-400 border border-slate-800 rounded-2xl bg-slate-900/30">
          <Layers className="h-12 w-12 text-slate-600 mb-3" />
          <h3 className="text-base font-semibold text-slate-300">No Compose Stacks Discovered</h3>
          <p className="text-sm text-slate-500 max-w-sm text-center mt-1">
            {search
              ? "No stacks match your search query."
              : "No containers with com.docker.compose.project labels were found on this host."}
          </p>
          {!search && (
            <Button
              size="sm"
              onClick={() => setIsDeployModalOpen(true)}
              className="mt-4 bg-sky-500 hover:bg-sky-400 text-white"
            >
              <Plus className="h-4 w-4 mr-2" />
              Deploy Your First Stack
            </Button>
          )}
        </div>
      ) : (
        <div className="space-y-4">
          {filteredStacks.map((stack) => {
            const isExpanded = !!expandedStacks[stack.name];
            const isBusy = actionLoadingName === stack.name;

            return (
              <div
                key={stack.name}
                className="rounded-xl border border-slate-800 bg-slate-900/50 overflow-hidden shadow-sm transition hover:border-slate-700/80"
              >
                {/* Stack Header Row */}
                <div className="flex flex-col sm:flex-row sm:items-center justify-between p-4 gap-3 bg-slate-950/40 border-b border-slate-800/80">
                  <div className="flex items-center gap-3">
                    <button
                      onClick={() => toggleExpand(stack.name)}
                      className="text-slate-400 hover:text-slate-200 transition"
                      aria-label="Toggle details"
                    >
                      {isExpanded ? (
                        <ChevronDown className="h-5 w-5" />
                      ) : (
                        <ChevronRight className="h-5 w-5" />
                      )}
                    </button>

                    <div>
                      <div className="flex items-center gap-2.5">
                        <span className="font-semibold text-white text-base tracking-tight">
                          {stack.name}
                        </span>
                        {getStatusBadge(stack.status)}
                      </div>
                      <div className="text-xs text-slate-400 flex items-center gap-3 mt-1 font-mono">
                        <span>
                          {stack.services.length} service{stack.services.length === 1 ? "" : "s"}
                        </span>
                        <span>•</span>
                        <span>
                          {stack.running_containers}/{stack.total_containers} running
                        </span>
                        {stack.working_dir && (
                          <>
                            <span>•</span>
                            <span className="text-slate-500 truncate max-w-xs" title={stack.working_dir}>
                              {stack.working_dir}
                            </span>
                          </>
                        )}
                      </div>
                    </div>
                  </div>

                  {/* Actions Bar */}
                  <div className="flex items-center gap-2 shrink-0">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handleStart(stack.name)}
                      disabled={isBusy}
                      title="Start all services in stack"
                      className="border-slate-700 bg-slate-900/50 hover:bg-slate-800 text-emerald-400"
                    >
                      <Play className="h-3.5 w-3.5 mr-1.5" />
                      Start
                    </Button>

                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handleStop(stack.name)}
                      disabled={isBusy}
                      title="Stop all services in stack"
                      className="border-slate-700 bg-slate-900/50 hover:bg-slate-800 text-amber-400"
                    >
                      <Square className="h-3.5 w-3.5 mr-1.5" />
                      Stop
                    </Button>

                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => handleRestart(stack.name)}
                      disabled={isBusy}
                      title="Restart all services in stack"
                      className="border-slate-700 bg-slate-900/50 hover:bg-slate-800 text-sky-400"
                    >
                      <RotateCw className="h-3.5 w-3.5 mr-1.5" />
                      Restart
                    </Button>

                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setDeleteTarget(stack);
                        setRemoveVolumes(false);
                      }}
                      disabled={isBusy}
                      title="Remove stack containers"
                      className="border-rose-900/40 bg-rose-950/20 hover:bg-rose-900/40 text-rose-400"
                    >
                      <Trash2 className="h-3.5 w-3.5 mr-1.5" />
                      Remove
                    </Button>
                  </div>
                </div>

                {/* Expanded Services Breakdown */}
                {isExpanded && (
                  <div className="p-4 space-y-3 bg-slate-950/20">
                    <div className="text-xs font-semibold text-slate-400 uppercase tracking-wider mb-2">
                      Services ({stack.services.length})
                    </div>
                    <div className="grid grid-cols-1 gap-2.5">
                      {stack.services.map((svc) => (
                        <div
                          key={svc.name}
                          className="flex flex-col sm:flex-row sm:items-center justify-between p-3 rounded-lg border border-slate-800/80 bg-slate-900/40 gap-3"
                        >
                          <div className="flex items-center gap-3">
                            <Box className="h-4 w-4 text-sky-400 shrink-0" />
                            <div>
                              <div className="flex items-center gap-2">
                                <span className="font-medium text-slate-200 text-sm">{svc.name}</span>
                                <span className="text-[11px] font-mono px-2 py-0.5 rounded bg-slate-800 text-slate-300">
                                  {svc.running_replicas}/{svc.replicas} replicas
                                </span>
                              </div>
                              <div className="text-xs text-slate-500 font-mono mt-0.5">
                                Image: <span className="text-slate-400">{svc.image}</span>
                              </div>
                            </div>
                          </div>

                          {/* Containers in Service */}
                          <div className="flex flex-wrap items-center gap-2">
                            {svc.containers.map((c) => {
                              const isRunning = c.state.toLowerCase() === "running";
                              const nameStr = c.names && c.names.length > 0 ? c.names[0].replace(/^\//, "") : (c.id || "").slice(0, 12);
                              const portStrs = (c.ports || [])
                                .filter((p) => p.public_port)
                                .map((p) => `${p.public_port}:${p.private_port}`)
                                .join(", ");

                              return (
                                <div
                                  key={c.id}
                                  className="flex items-center gap-2 text-xs font-mono px-2.5 py-1 rounded bg-slate-950/80 border border-slate-800"
                                >
                                  {isRunning ? (
                                    <span className="h-2 w-2 rounded-full bg-emerald-400" />
                                  ) : (
                                    <span className="h-2 w-2 rounded-full bg-slate-500" />
                                  )}
                                  <span className="text-slate-300 truncate max-w-[140px]" title={nameStr}>
                                    {nameStr}
                                  </span>
                                  {portStrs && (
                                    <span className="text-sky-400/80 text-[10px]">({portStrs})</span>
                                  )}
                                </div>
                              );
                            })}
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {/* Deploy Stack Modal */}
      {isDeployModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/70 backdrop-blur-sm animate-in fade-in duration-150">
          <div className="w-full max-w-2xl rounded-2xl bg-slate-900 border border-slate-800 p-6 shadow-2xl space-y-4">
            <div className="flex items-center justify-between pb-3 border-b border-slate-800">
              <h3 className="text-lg font-bold text-white flex items-center gap-2">
                <FileCode className="h-5 w-5 text-sky-400" />
                Deploy Docker Compose Stack
              </h3>
              <button
                onClick={() => setIsDeployModalOpen(false)}
                className="text-slate-400 hover:text-slate-200"
              >
                ✕
              </button>
            </div>

            {deployError && (
              <div className="p-3 rounded-lg bg-rose-500/10 border border-rose-500/20 text-rose-300 text-xs flex items-center gap-2">
                <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
                <span>{deployError}</span>
              </div>
            )}

            <form onSubmit={handleDeploy} className="space-y-4">
              <div>
                <label className="block text-xs font-semibold text-slate-300 uppercase tracking-wider mb-1.5">
                  Stack Project Name
                </label>
                <Input
                  placeholder="e.g. my-webapp"
                  value={deployName}
                  onChange={(e) => setDeployName(e.target.value)}
                  className="bg-slate-950/70 border-slate-800 text-slate-200 font-mono"
                  required
                />
                <p className="text-[11px] text-slate-500 mt-1">
                  Only alphanumeric characters, hyphens, and underscores are allowed.
                </p>
              </div>

              <div>
                <div className="flex items-center justify-between mb-1.5">
                  <label className="block text-xs font-semibold text-slate-300 uppercase tracking-wider">
                    Compose YAML Configuration
                  </label>
                  <label className="text-xs text-sky-400 hover:underline cursor-pointer">
                    Upload .yml file
                    <input
                      type="file"
                      accept=".yml,.yaml"
                      className="hidden"
                      onChange={(e) => {
                        const file = e.target.files?.[0];
                        if (file) {
                          const reader = new FileReader();
                          reader.onload = (evt) => {
                            if (typeof evt.target?.result === "string") {
                              setDeployYaml(evt.target.result);
                              if (!deployName) {
                                setDeployName(
                                  file.name.replace(/\.(yml|yaml)$/, "").toLowerCase()
                                );
                              }
                            }
                          };
                          reader.readAsText(file);
                        }
                      }}
                    />
                  </label>
                </div>
                <textarea
                  rows={12}
                  placeholder={`version: '3.8'\nservices:\n  web:\n    image: nginx:alpine\n    ports:\n      - "8080:80"`}
                  value={deployYaml}
                  onChange={(e) => setDeployYaml(e.target.value)}
                  className="w-full font-mono text-xs rounded-xl border border-slate-800 bg-slate-950/80 p-3 text-slate-200 focus:outline-none focus:border-sky-500 resize-y"
                  required
                />
              </div>

              <div className="flex items-center justify-end gap-3 pt-3 border-t border-slate-800">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setIsDeployModalOpen(false)}
                  disabled={isDeploying}
                  className="border-slate-700 bg-slate-900 hover:bg-slate-800 text-slate-300"
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={isDeploying}
                  className="bg-sky-500 hover:bg-sky-400 text-white"
                >
                  {isDeploying ? (
                    <>
                      <RefreshCw className="h-4 w-4 mr-2 animate-spin" />
                      Deploying Stack...
                    </>
                  ) : (
                    "Deploy Stack"
                  )}
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Delete Stack Confirmation Modal */}
      {deleteTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/70 backdrop-blur-sm animate-in fade-in duration-150">
          <div className="w-full max-w-md rounded-2xl bg-slate-900 border border-slate-800 p-6 shadow-2xl space-y-4">
            <h3 className="text-lg font-bold text-white flex items-center gap-2">
              <Trash2 className="h-5 w-5 text-rose-400" />
              Remove Stack: {deleteTarget.name}?
            </h3>

            <p className="text-sm text-slate-300">
              This will remove all <strong className="text-white">{deleteTarget.total_containers}</strong>{" "}
              containers belonging to the stack <strong className="text-white">{deleteTarget.name}</strong>.
            </p>

            <div className="flex items-center gap-2 p-3 rounded-xl bg-slate-950/80 border border-slate-800">
              <input
                type="checkbox"
                id="remove-vols"
                checked={removeVolumes}
                onChange={(e) => setRemoveVolumes(e.target.checked)}
                className="rounded border-slate-700 text-rose-500 focus:ring-rose-500"
              />
              <label htmlFor="remove-vols" className="text-xs text-slate-300 cursor-pointer">
                Also remove associated volumes (Data will be permanently deleted)
              </label>
            </div>

            <div className="flex items-center justify-end gap-3 pt-3 border-t border-slate-800">
              <Button
                variant="outline"
                onClick={() => setDeleteTarget(null)}
                className="border-slate-700 bg-slate-900 hover:bg-slate-800 text-slate-300"
              >
                Cancel
              </Button>
              <Button
                onClick={handleDelete}
                className="bg-rose-600 hover:bg-rose-500 text-white shadow-lg shadow-rose-600/20"
              >
                Confirm Removal
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
