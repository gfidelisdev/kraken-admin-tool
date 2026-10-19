"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import Link from "next/link";
import {
  Box,
  Play,
  Square,
  RotateCw,
  Trash2,
  FileText,
  Search,
  RefreshCw,
  AlertCircle,
  ExternalLink,
} from "lucide-react";
import { api } from "@/lib/api";
import { DockerContainer } from "@/types";
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
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { ConfirmActionModal } from "@/components/modals/ConfirmActionModal";
import { useServer } from "@/context/ServerContext";
import { formatDate } from "@/lib/utils";

export default function ContainersPage() {
  const { activeServer } = useServer();
  const [containers, setContainers] = useState<DockerContainer[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [tabFilter, setTabFilter] = useState("all");
  const [error, setError] = useState<string | null>(null);
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null);

  // Confirm delete modal state
  const [deleteTarget, setDeleteTarget] = useState<DockerContainer | null>(null);

  const fetchContainers = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const data = await api.get<DockerContainer[]>("/api/docker/containers?all=true");
      setContainers(Array.isArray(data) ? data : []);
    } catch (err: any) {
      setError(err?.message || "Failed to load Docker containers");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchContainers();
  }, [fetchContainers, activeServer?.id]);

  const handleStart = async (id: string) => {
    setActionLoadingId(id);
    try {
      await api.post(`/api/docker/containers/${id}/start`);
      await fetchContainers();
    } catch (err: any) {
      setError(err?.message || `Failed to start container ${id.slice(0, 12)}`);
    } finally {
      setActionLoadingId(null);
    }
  };

  const handleStop = async (id: string) => {
    setActionLoadingId(id);
    try {
      await api.post(`/api/docker/containers/${id}/stop`);
      await fetchContainers();
    } catch (err: any) {
      setError(err?.message || `Failed to stop container ${id.slice(0, 12)}`);
    } finally {
      setActionLoadingId(null);
    }
  };

  const handleRestart = async (id: string) => {
    setActionLoadingId(id);
    try {
      await api.post(`/api/docker/containers/${id}/restart`);
      await fetchContainers();
    } catch (err: any) {
      setError(err?.message || `Failed to restart container ${id.slice(0, 12)}`);
    } finally {
      setActionLoadingId(null);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await api.delete(`/api/docker/containers/${deleteTarget.id}?force=true`);
      setDeleteTarget(null);
      await fetchContainers();
    } catch (err: any) {
      setError(err?.message || `Failed to delete container`);
    }
  };

  // Filter logic
  const filteredContainers = useMemo(() => {
    return containers.filter((c) => {
      // Tab filter
      const isRunning =
        c.state === "running" || c.status?.toLowerCase().includes("up");
      if (tabFilter === "running" && !isRunning) return false;
      if (tabFilter === "stopped" && isRunning) return false;

      // Search filter
      if (search.trim()) {
        const query = search.toLowerCase();
        const namesMatch = c.names?.some((n) => n.toLowerCase().includes(query));
        const imageMatch = c.image?.toLowerCase().includes(query);
        const idMatch = c.id?.toLowerCase().includes(query);
        return namesMatch || imageMatch || idMatch;
      }

      return true;
    });
  }, [containers, tabFilter, search]);

  const runningCount = containers.filter(
    (c) => c.state === "running" || c.status?.toLowerCase().includes("up")
  ).length;
  const stoppedCount = containers.length - runningCount;

  return (
    <div className="space-y-6 animate-in fade-in duration-300">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <div className="flex items-center space-x-2">
            <Box className="h-6 w-6 text-sky-400" />
            <h1 className="text-2xl font-bold tracking-tight text-white">
              Docker Containers
            </h1>
          </div>
          <p className="text-xs text-slate-400 mt-1">
            Monitor, inspect, control lifecycle and stream logs of local Docker containers.
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={fetchContainers}
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

      {/* Filter and Search Bar */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <Tabs value={tabFilter} onValueChange={setTabFilter}>
          <TabsList>
            <TabsTrigger value="all" badge={containers.length}>
              All
            </TabsTrigger>
            <TabsTrigger value="running" badge={runningCount}>
              Running
            </TabsTrigger>
            <TabsTrigger value="stopped" badge={stoppedCount}>
              Stopped
            </TabsTrigger>
          </TabsList>
        </Tabs>

        <div className="w-full sm:w-72">
          <Input
            placeholder="Search container by name, image, or ID..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            icon={<Search className="h-4 w-4 text-slate-500" />}
          />
        </div>
      </div>

      {/* Table */}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Container Name</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Image</TableHead>
            <TableHead>Ports</TableHead>
            <TableHead>Created</TableHead>
            <TableHead className="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {filteredContainers.length === 0 ? (
            <TableEmpty
              colSpan={6}
              icon={<Box className="h-8 w-8" />}
              message={
                containers.length === 0
                  ? "No Docker containers found on this host."
                  : "No containers match your search or filter."
              }
            />
          ) : (
            filteredContainers.map((container) => {
              const name =
                container.names?.[0]?.replace(/^\//, "") || container.id.slice(0, 12);
              const isRunning =
                container.state === "running" ||
                container.status?.toLowerCase().includes("up");
              const isBusy = actionLoadingId === container.id;

              return (
                <TableRow key={container.id}>
                  {/* Name and ID */}
                  <TableCell>
                    <div>
                      <p className="font-semibold text-white text-xs">{name}</p>
                      <p className="font-mono text-[11px] text-slate-500">
                        {container.id.slice(0, 12)}
                      </p>
                    </div>
                  </TableCell>

                  {/* Status Badge */}
                  <TableCell>
                    <StatusBadge status={container.status || container.state} />
                  </TableCell>

                  {/* Image */}
                  <TableCell>
                    <span className="font-mono text-xs text-sky-400 bg-sky-500/10 px-2 py-0.5 rounded border border-sky-500/20 max-w-[200px] truncate block">
                      {container.image}
                    </span>
                  </TableCell>

                  {/* Ports */}
                  <TableCell className="text-xs font-mono text-slate-300">
                    {container.ports && container.ports.length > 0 ? (
                      <div className="flex flex-wrap gap-1">
                        {container.ports.map((p, idx) => (
                          <span
                            key={idx}
                            className="bg-slate-800/80 px-1.5 py-0.5 rounded text-[11px] text-slate-300"
                          >
                            {p.public_port ? `${p.public_port}:` : ""}
                            {p.private_port}/{p.type}
                          </span>
                        ))}
                      </div>
                    ) : (
                      <span className="text-slate-500">-</span>
                    )}
                  </TableCell>

                  {/* Created */}
                  <TableCell className="text-xs text-slate-400">
                    {formatDate(container.created)}
                  </TableCell>

                  {/* Actions */}
                  <TableCell className="text-right">
                    <div className="flex items-center justify-end space-x-1.5">
                      {/* Log viewer link */}
                      <Link
                        href={`/logs?source=docker&container=${container.id}`}
                        title="View container logs"
                      >
                        <Button variant="ghost" size="icon" className="h-8 w-8 text-sky-400">
                          <FileText className="h-4 w-4" />
                        </Button>
                      </Link>

                      {/* Start / Stop Toggle */}
                      {isRunning ? (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-amber-400 hover:text-amber-300"
                          title="Stop container"
                          disabled={isBusy}
                          onClick={() => handleStop(container.id)}
                        >
                          <Square className="h-3.5 w-3.5" />
                        </Button>
                      ) : (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-8 w-8 text-emerald-400 hover:text-emerald-300"
                          title="Start container"
                          disabled={isBusy}
                          onClick={() => handleStart(container.id)}
                        >
                          <Play className="h-3.5 w-3.5" />
                        </Button>
                      )}

                      {/* Restart */}
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 text-slate-400 hover:text-white"
                        title="Restart container"
                        disabled={isBusy}
                        onClick={() => handleRestart(container.id)}
                      >
                        <RotateCw className="h-3.5 w-3.5" />
                      </Button>

                      {/* Delete */}
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 text-rose-400 hover:text-rose-300"
                        title="Delete container"
                        disabled={isBusy}
                        onClick={() => setDeleteTarget(container)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              );
            })
          )}
        </TableBody>
      </Table>

      {/* Delete Confirmation Modal */}
      <ConfirmActionModal
        isOpen={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        title="Remove Container"
        description={`Are you sure you want to forcibly remove container "${
          deleteTarget?.names?.[0]?.replace(/^\//, "") || deleteTarget?.id.slice(0, 12)
        }"? Any unsaved data will be permanently lost.`}
        confirmLabel="Remove Container"
        confirmVariant="destructive"
        onConfirm={handleDelete}
      />
    </div>
  );
}
