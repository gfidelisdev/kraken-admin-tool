"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import {
  ShieldCheck,
  Search,
  RefreshCw,
  AlertCircle,
  Filter,
  User,
  Info,
} from "lucide-react";
import { api } from "@/lib/api";
import { AuditLog } from "@/types";
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
import { Modal } from "@/components/ui/Modal";
import { formatDate } from "@/lib/utils";

export default function AuditPage() {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [typeFilter, setTypeFilter] = useState("all");
  const [error, setError] = useState<string | null>(null);

  // Detail Modal
  const [selectedLog, setSelectedLog] = useState<AuditLog | null>(null);

  const fetchAuditLogs = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const data = await api.get<any>("/api/audit-logs");
      const list = Array.isArray(data) ? data : data?.data || data?.logs || [];
      setLogs(list);
    } catch (err: any) {
      setError(err?.message || "Failed to load audit logs");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAuditLogs();
  }, [fetchAuditLogs]);

  const filteredLogs = useMemo(() => {
    return logs.filter((log) => {
      // Status filter
      if (statusFilter !== "all" && log.status?.toUpperCase() !== statusFilter) {
        return false;
      }

      // Type filter
      if (typeFilter !== "all") {
        if (!log.resource_type?.toLowerCase().includes(typeFilter.toLowerCase())) {
          return false;
        }
      }

      // Search query
      if (search.trim()) {
        const q = search.toLowerCase();
        const userMatch = log.username?.toLowerCase().includes(q);
        const actionMatch = log.action?.toLowerCase().includes(q);
        const resMatch = log.resource_id?.toLowerCase().includes(q);
        const detailsMatch = log.details?.toLowerCase().includes(q);
        return userMatch || actionMatch || resMatch || detailsMatch;
      }

      return true;
    });
  }, [logs, statusFilter, typeFilter, search]);

  return (
    <div className="space-y-6 animate-in fade-in duration-300">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <div className="flex items-center space-x-2">
            <ShieldCheck className="h-6 w-6 text-sky-400" />
            <h1 className="text-2xl font-bold tracking-tight text-white">
              Audit Operations Trail
            </h1>
          </div>
          <p className="text-xs text-slate-400 mt-1">
            Immutable tracking of all administrative actions across Docker, Kubernetes, and Authentication.
          </p>
        </div>

        <Button
          variant="outline"
          size="sm"
          onClick={fetchAuditLogs}
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

      {/* Filter Toolbar */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <Input
          placeholder="Search by user, action, resource..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          icon={<Search className="h-4 w-4 text-slate-500" />}
        />

        <Select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          options={[
            { label: "All Statuses", value: "all" },
            { label: "Success only", value: "SUCCESS" },
            { label: "Failed only", value: "FAILED" },
          ]}
        />

        <Select
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          options={[
            { label: "All Resource Types", value: "all" },
            { label: "Docker Resources", value: "docker" },
            { label: "Kubernetes Resources", value: "k8s" },
            { label: "Auth / System", value: "auth" },
          ]}
        />
      </div>

      {/* Audit Table */}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>User</TableHead>
            <TableHead>Action</TableHead>
            <TableHead>Resource Type</TableHead>
            <TableHead>Target ID</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Timestamp</TableHead>
            <TableHead className="text-right">Details</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {filteredLogs.length === 0 ? (
            <TableEmpty
              colSpan={7}
              icon={<ShieldCheck className="h-8 w-8" />}
              message={
                logs.length === 0
                  ? "No audit records logged yet."
                  : "No audit logs match current filters."
              }
            />
          ) : (
            filteredLogs.map((log) => (
              <TableRow key={log.id}>
                {/* User */}
                <TableCell>
                  <div className="flex items-center space-x-2">
                    <div className="flex h-6 w-6 items-center justify-center rounded-full bg-slate-800 text-[11px] text-slate-300 font-semibold">
                      {log.username ? log.username.charAt(0).toUpperCase() : "U"}
                    </div>
                    <span className="font-semibold text-white text-xs">{log.username}</span>
                  </div>
                </TableCell>

                {/* Action */}
                <TableCell>
                  <Badge variant="outline" className="font-mono text-[11px]">
                    {log.action}
                  </Badge>
                </TableCell>

                {/* Resource Type */}
                <TableCell className="font-mono text-xs text-slate-400">
                  {log.resource_type}
                </TableCell>

                {/* Target ID */}
                <TableCell className="font-mono text-xs text-slate-300 max-w-[160px] truncate">
                  {log.resource_id}
                </TableCell>

                {/* Status */}
                <TableCell>
                  <StatusBadge status={log.status} />
                </TableCell>

                {/* Timestamp */}
                <TableCell className="text-xs text-slate-400">
                  {formatDate(log.created_at)}
                </TableCell>

                {/* Details Button */}
                <TableCell className="text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 text-xs text-slate-400 hover:text-white"
                    onClick={() => setSelectedLog(log)}
                  >
                    <Info className="h-3.5 w-3.5 mr-1" />
                    Inspect
                  </Button>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>

      {/* Detail Modal */}
      {selectedLog && (
        <Modal
          isOpen={!!selectedLog}
          onClose={() => setSelectedLog(null)}
          title={`Audit Event #${selectedLog.id}: ${selectedLog.action}`}
          description={`Logged at ${formatDate(selectedLog.created_at)} by ${selectedLog.username}`}
        >
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-3 rounded-xl border border-slate-800 bg-slate-950/60 p-4 text-xs">
              <div>
                <span className="text-slate-500">Resource Type:</span>
                <p className="font-mono text-slate-200 mt-0.5">{selectedLog.resource_type}</p>
              </div>
              <div>
                <span className="text-slate-500">Status:</span>
                <div className="mt-0.5">
                  <StatusBadge status={selectedLog.status} />
                </div>
              </div>
              <div className="col-span-2">
                <span className="text-slate-500">Resource ID:</span>
                <p className="font-mono text-slate-200 mt-0.5 break-all">
                  {selectedLog.resource_id}
                </p>
              </div>
            </div>

            <div>
              <span className="text-xs font-medium text-slate-400">Event Payload / Details:</span>
              <pre className="mt-1.5 max-h-56 overflow-auto rounded-xl border border-slate-800 bg-slate-950 p-3 font-mono text-xs text-slate-300 whitespace-pre-wrap break-all">
                {selectedLog.details || "No additional metadata recorded."}
              </pre>
            </div>

            <div className="flex justify-end pt-2 border-t border-slate-800">
              <Button variant="outline" size="sm" onClick={() => setSelectedLog(null)}>
                Close
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
