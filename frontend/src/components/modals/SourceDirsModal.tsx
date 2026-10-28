"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  FolderSearch,
  Plus,
  Trash2,
  RefreshCw,
  FolderTree,
  Calendar,
  AlertCircle,
  CheckCircle2,
  Clock,
  FileCode,
  Link2,
  Unlink,
  Eye,
  Check,
  Copy,
  ChevronRight,
  Layers,
  Sparkles,
} from "lucide-react";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import {
  listSourceDirs,
  createSourceDir,
  deleteSourceDir,
  updateSourceDir,
  scanSourcesStream,
  listDiscoveredFiles,
  getSourceContent,
  bindSource,
  unbindSource,
  api,
} from "@/lib/api";
import {
  SourceSearchDir,
  SourceScanProgress,
  SourceLink,
  DockerImage,
  CandidateWorkloadRecommendation,
} from "@/types";

interface SourceDirsModalProps {
  isOpen: boolean;
  onClose: () => void;
  serverId: number;
  serverName?: string;
  onScanCompleted?: () => void;
}

export function SourceDirsModal({
  isOpen,
  onClose,
  serverId,
  serverName = "Target Host",
  onScanCompleted,
}: SourceDirsModalProps) {
  const [activeTab, setActiveTab] = useState<"dirs" | "files">("dirs");
  const [dirs, setDirs] = useState<SourceSearchDir[]>([]);
  const [discoveredFiles, setDiscoveredFiles] = useState<SourceLink[]>([]);
  const [availableImages, setAvailableImages] = useState<DockerImage[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // New Directory Form
  const [newPath, setNewPath] = useState("");
  const [newRecursive, setNewRecursive] = useState(true);
  const [newMaxDepth, setNewMaxDepth] = useState(5);
  const [newScheduleEnabled, setNewScheduleEnabled] = useState(false);
  const [newScheduleCron, setNewScheduleCron] = useState("@daily");
  const [isAdding, setIsAdding] = useState(false);

  // Scan Stream State
  const [isScanning, setIsScanning] = useState(false);
  const [scanProgress, setScanProgress] = useState<SourceScanProgress | null>(null);
  const [scanSummary, setScanSummary] = useState<{ total_files_found: number; image_matches: number } | null>(null);

  // File Preview Modal/State
  const [previewFile, setPreviewFile] = useState<{ path: string; content: string } | null>(null);
  const [isPreviewLoading, setIsPreviewLoading] = useState(false);
  const [previewCopied, setPreviewCopied] = useState(false);

  // Manual Bind State
  const [bindingFile, setBindingFile] = useState<SourceLink | null>(null);
  const [selectedTargetRef, setSelectedTargetRef] = useState<string>("");
  const [isBinding, setIsBinding] = useState(false);

  const fetchDirs = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const [dirsData, filesData] = await Promise.all([
        listSourceDirs(serverId),
        listDiscoveredFiles(serverId),
      ]);
      setDirs(dirsData);
      setDiscoveredFiles(filesData);

      // Fetch images on this host for manual binding dropdown
      try {
        const imgs = await api.get<DockerImage[]>("/api/docker/images", {
          headers: { "X-Server-ID": serverId.toString() },
        });
        setAvailableImages(Array.isArray(imgs) ? imgs : []);
      } catch {
        // non-blocking
      }
    } catch (err: any) {
      setError(err?.message || "Failed to load source directories and discovered files");
    } finally {
      setIsLoading(false);
    }
  }, [serverId]);

  useEffect(() => {
    if (isOpen) {
      fetchDirs();
      setScanProgress(null);
      setScanSummary(null);
      setError(null);
      setPreviewFile(null);
      setBindingFile(null);
    }
  }, [isOpen, fetchDirs]);

  const handleAddDir = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newPath.trim()) return;

    setIsAdding(true);
    setError(null);
    try {
      await createSourceDir(serverId, {
        path: newPath.trim(),
        recursive: newRecursive,
        max_depth: Number(newMaxDepth) || 5,
        schedule_enabled: newScheduleEnabled,
        schedule_cron: newScheduleCron,
      });
      setNewPath("");
      setNewScheduleEnabled(false);
      await fetchDirs();
    } catch (err: any) {
      setError(err?.message || "Failed to add search directory");
    } finally {
      setIsAdding(false);
    }
  };

  const handleDeleteDir = async (dirId: number) => {
    try {
      await deleteSourceDir(serverId, dirId);
      await fetchDirs();
    } catch (err: any) {
      setError(err?.message || "Failed to delete directory");
    }
  };

  const handleToggleSchedule = async (dir: SourceSearchDir) => {
    try {
      await updateSourceDir(serverId, dir.id, {
        schedule_enabled: !dir.schedule_enabled,
      });
      await fetchDirs();
    } catch (err: any) {
      setError(err?.message || "Failed to update schedule");
    }
  };

  const handleStartScan = async () => {
    setIsScanning(true);
    setError(null);
    setScanSummary(null);
    setScanProgress({
      stage: "discovering",
      message: "Initiating host source file scan...",
      files_found: 0,
      matches_made: 0,
    });

    try {
      const summary = await scanSourcesStream(serverId, (p) => {
        setScanProgress(p);
      });
      setScanSummary(summary);
      await fetchDirs();
      setActiveTab("files");
      if (onScanCompleted) {
        onScanCompleted();
      }
    } catch (err: any) {
      setError(err?.message || "Scan failed");
    } finally {
      setIsScanning(false);
    }
  };

  const handleViewContent = async (file: SourceLink) => {
    setIsPreviewLoading(true);
    setError(null);
    try {
      const res = await getSourceContent(serverId, file.file_path);
      setPreviewFile({
        path: res.file_path,
        content: res.content,
      });
    } catch (err: any) {
      setError(err?.message || "Failed to read file content from host");
    } finally {
      setIsPreviewLoading(false);
    }
  };

  const handleOpenBindDialog = (file: SourceLink) => {
    setBindingFile(file);
    setSelectedTargetRef(file.target_ref || "");
  };

  const handleApplyBind = async () => {
    if (!bindingFile || !selectedTargetRef.trim()) return;
    setIsBinding(true);
    setError(null);
    try {
      await bindSource(serverId, {
        link_id: bindingFile.id,
        file_path: bindingFile.file_path,
        kind: bindingFile.kind,
        target_ref: selectedTargetRef.trim(),
      });
      setBindingFile(null);
      await fetchDirs();
      if (onScanCompleted) onScanCompleted();
    } catch (err: any) {
      setError(err?.message || "Failed to bind source file to workload");
    } finally {
      setIsBinding(false);
    }
  };

  const handleUnbind = async (linkId: number) => {
    try {
      await unbindSource(serverId, linkId);
      await fetchDirs();
      if (onScanCompleted) onScanCompleted();
    } catch (err: any) {
      setError(err?.message || "Failed to unbind source file");
    }
  };

  const parseRecommendations = (metadataStr?: string): CandidateWorkloadRecommendation[] => {
    if (!metadataStr) return [];
    try {
      const parsed = JSON.parse(metadataStr);
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={isScanning ? () => {} : onClose}
      title={`Source Management: ${serverName}`}
      description="Discover author-created Dockerfiles and Compose files on target hosts, view raw files, and bind them to container workloads."
      maxWidth="xl"
    >
      <div className="space-y-4">
        {error && (
          <div className="p-3 rounded-xl bg-rose-500/10 border border-rose-500/20 text-rose-300 text-xs flex items-center justify-between">
            <div className="flex items-center gap-2">
              <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
              <span>{error}</span>
            </div>
            <button onClick={() => setError(null)} className="text-rose-400 hover:text-white">✕</button>
          </div>
        )}

        {/* Scan Actions & Progress Banner */}
        <div className="p-3.5 bg-slate-950/70 rounded-xl border border-slate-800 space-y-3">
          <div className="flex items-center justify-between flex-wrap gap-2">
            <div>
              <div className="text-xs font-semibold text-slate-200 flex items-center gap-1.5">
                <FolderSearch className="w-4 h-4 text-sky-400" />
                <span>Host Source Discovery & Binding</span>
              </div>
              <div className="text-[11px] text-slate-400">
                Discovered files remain read-only. Automatic and manual bindings are stored in the Kraken database.
              </div>
            </div>

            <Button
              onClick={handleStartScan}
              disabled={isScanning || dirs.length === 0}
              className="flex items-center gap-1.5 text-xs py-1.5"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${isScanning ? "animate-spin" : ""}`} />
              <span>{isScanning ? "Scanning Host..." : "Scan Directories Now"}</span>
            </Button>
          </div>

          {/* Active Scan Progress */}
          {scanProgress && (
            <div className="p-2.5 rounded-lg bg-sky-500/10 border border-sky-500/20 space-y-1.5">
              <div className="flex items-center justify-between text-xs">
                <span className="font-medium text-sky-300">{scanProgress.message}</span>
                <span className="text-[11px] text-sky-400 font-mono">
                  {scanProgress.files_found} files found / {scanProgress.matches_made} matches
                </span>
              </div>
              {scanProgress.current_path && (
                <div className="text-[10px] text-slate-400 truncate font-mono">
                  {scanProgress.current_path}
                </div>
              )}
            </div>
          )}

          {scanSummary && !isScanning && (
            <div className="p-2.5 rounded-lg bg-emerald-500/10 border border-emerald-500/20 text-emerald-300 text-xs flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 text-emerald-400 shrink-0" />
              <span>
                Scan finished! Evaluated {scanSummary.total_files_found} candidate files and linked {scanSummary.image_matches} workloads.
              </span>
            </div>
          )}
        </div>

        {/* Tab Bar: Search Dirs vs Discovered Files */}
        <div className="flex items-center gap-2 border-b border-slate-800 pb-2">
          <button
            type="button"
            onClick={() => setActiveTab("dirs")}
            className={`px-3 py-1.5 rounded-lg text-xs font-medium transition flex items-center gap-1.5 ${
              activeTab === "dirs"
                ? "bg-slate-800 text-white border border-slate-700"
                : "text-slate-400 hover:text-white"
            }`}
          >
            <FolderTree className="w-3.5 h-3.5 text-sky-400" />
            <span>Search Directories ({dirs.length})</span>
          </button>
          <button
            type="button"
            onClick={() => setActiveTab("files")}
            className={`px-3 py-1.5 rounded-lg text-xs font-medium transition flex items-center gap-1.5 ${
              activeTab === "files"
                ? "bg-slate-800 text-white border border-slate-700"
                : "text-slate-400 hover:text-white"
            }`}
          >
            <FileCode className="w-3.5 h-3.5 text-indigo-400" />
            <span>Discovered Files ({discoveredFiles.length})</span>
            {discoveredFiles.filter((f) => f.status === "unbound" || f.status === "candidate").length > 0 && (
              <span className="text-[10px] px-1.5 py-0.2 rounded-full bg-amber-500/20 text-amber-300 font-bold">
                {discoveredFiles.filter((f) => f.status === "unbound" || f.status === "candidate").length}
              </span>
            )}
          </button>
        </div>

        {activeTab === "dirs" && (
          <div className="space-y-4">
            {/* Add Directory Form */}
            <form onSubmit={handleAddDir} className="p-3 bg-slate-900/60 rounded-xl border border-slate-800 space-y-3">
              <div className="text-xs font-semibold text-slate-300 flex items-center gap-1.5">
                <Plus className="w-3.5 h-3.5 text-sky-400" />
                <span>Add Target Search Directory</span>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5">
                <div className="sm:col-span-2">
                  <Input
                    value={newPath}
                    onChange={(e) => setNewPath(e.target.value)}
                    placeholder="e.g. /srv/projetos or /opt/services"
                    className="text-xs bg-slate-950 border-slate-750 font-mono"
                  />
                </div>
                <div>
                  <Input
                    type="number"
                    min={1}
                    max={15}
                    value={newMaxDepth}
                    onChange={(e) => setNewMaxDepth(Number(e.target.value))}
                    placeholder="Max Depth (e.g. 5)"
                    className="text-xs bg-slate-950 border-slate-750"
                  />
                </div>
              </div>

              <div className="flex items-center justify-between flex-wrap gap-2 pt-1">
                <div className="flex items-center gap-4 text-xs text-slate-300">
                  <label className="flex items-center gap-1.5 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={newRecursive}
                      onChange={(e) => setNewRecursive(e.target.checked)}
                      className="rounded border-slate-750 bg-slate-900 text-sky-500"
                    />
                    <span>Recursive</span>
                  </label>

                  <label className="flex items-center gap-1.5 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={newScheduleEnabled}
                      onChange={(e) => setNewScheduleEnabled(e.target.checked)}
                      className="rounded border-slate-750 bg-slate-900 text-sky-500"
                    />
                    <span>Schedule Automatic Scans</span>
                  </label>
                </div>

                <Button type="submit" disabled={isAdding || !newPath.trim()} className="text-xs py-1.5">
                  Add Directory
                </Button>
              </div>
            </form>

            {/* Search Directories List */}
            <div className="space-y-2">
              {isLoading ? (
                <div className="py-8 text-center text-xs text-slate-400 flex items-center justify-center gap-2">
                  <RefreshCw className="w-4 h-4 animate-spin text-sky-400" />
                  <span>Loading search directories...</span>
                </div>
              ) : dirs.length === 0 ? (
                <div className="p-4 rounded-xl border border-dashed border-slate-800 text-center text-xs text-slate-400">
                  No search directories configured yet. Add paths like <code className="text-sky-400">/srv/projetos</code>, <code className="text-sky-400">/opt</code>, or <code className="text-sky-400">/home/user/app</code> to discover source files.
                </div>
              ) : (
                <div className="space-y-1.5 max-h-60 overflow-y-auto pr-1">
                  {dirs.map((d) => (
                    <div
                      key={d.id}
                      className="p-2.5 rounded-lg bg-slate-950/60 border border-slate-800 flex items-center justify-between gap-3 text-xs"
                    >
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <FolderTree className="w-3.5 h-3.5 text-sky-400 shrink-0" />
                          <span className="font-mono text-slate-200 font-semibold truncate">{d.path}</span>
                          {d.recursive && (
                            <span className="text-[10px] px-1.5 py-0.5 rounded bg-slate-800 text-slate-300 font-medium">
                              depth: {d.max_depth}
                            </span>
                          )}
                        </div>

                        <div className="flex items-center gap-3 text-[11px] text-slate-400 mt-1">
                          <button
                            onClick={() => handleToggleSchedule(d)}
                            className={`flex items-center gap-1 transition ${
                              d.schedule_enabled ? "text-purple-400 hover:text-purple-300" : "text-slate-500 hover:text-slate-400"
                            }`}
                          >
                            <Calendar className="w-3 h-3" />
                            <span>{d.schedule_enabled ? `Scheduled (${d.schedule_cron})` : "Schedule Disabled"}</span>
                          </button>

                          {d.last_scanned_at && (
                            <span className="flex items-center gap-1 text-slate-500">
                              <Clock className="w-3 h-3" />
                              <span>Last scanned: {new Date(d.last_scanned_at).toLocaleString()}</span>
                            </span>
                          )}
                        </div>
                      </div>

                      <button
                        onClick={() => handleDeleteDir(d.id)}
                        className="p-1 rounded text-slate-400 hover:text-rose-400 transition"
                        title="Delete Directory"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}

        {/* Tab 2: Discovered Files & Manual Binding */}
        {activeTab === "files" && (
          <div className="space-y-3">
            {discoveredFiles.length === 0 ? (
              <div className="p-8 rounded-xl border border-dashed border-slate-800 text-center text-xs text-slate-400 space-y-2">
                <FileCode className="w-8 h-8 text-slate-600 mx-auto" />
                <p>No container source files have been discovered yet.</p>
                <p className="text-[11px] text-slate-500">
                  Click &ldquo;Scan Directories Now&rdquo; above to inspect the configured search directories.
                </p>
              </div>
            ) : (
              <div className="space-y-2 max-h-80 overflow-y-auto pr-1">
                {discoveredFiles.map((file) => {
                  const recommendations = parseRecommendations(file.metadata);
                  const isBound = file.target_ref && file.target_ref.trim() !== "";

                  return (
                    <div
                      key={file.id}
                      className="p-3 rounded-xl bg-slate-950/70 border border-slate-800 space-y-2 text-xs"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0 flex-1 space-y-1">
                          <div className="flex items-center gap-2 flex-wrap">
                            <span className="font-mono text-slate-200 font-semibold truncate" title={file.file_path}>
                              {file.file_path}
                            </span>
                            <span className="text-[10px] uppercase font-bold px-1.5 py-0.5 rounded bg-slate-800 text-slate-400 border border-slate-700">
                              {file.kind}
                            </span>
                          </div>

                          {/* Binding Status Badge */}
                          <div className="flex items-center gap-2 text-[11px]">
                            {isBound ? (
                              <div className="flex items-center gap-1.5 text-emerald-400 font-mono">
                                <Link2 className="w-3.5 h-3.5" />
                                <span>Bound to: <strong>{file.target_ref}</strong></span>
                                {file.match_score > 0 && (
                                  <span className="text-[10px] px-1 py-0.2 rounded bg-emerald-500/10 border border-emerald-500/20">
                                    {Math.round(file.match_score * 100)}% match
                                  </span>
                                )}
                              </div>
                            ) : (
                              <div className="flex items-center gap-1.5 text-amber-400">
                                <Unlink className="w-3.5 h-3.5" />
                                <span>Unbound (Available for manual binding)</span>
                              </div>
                            )}
                          </div>
                        </div>

                        {/* Action buttons */}
                        <div className="flex items-center gap-1 shrink-0">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleViewContent(file)}
                            className="h-7 px-2 text-xs text-sky-400 hover:text-sky-300"
                            title="View Raw File Content"
                          >
                            <Eye className="w-3.5 h-3.5 mr-1" />
                            View
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => handleOpenBindDialog(file)}
                            className="h-7 px-2 text-xs border-indigo-700/60 bg-indigo-950/30 hover:bg-indigo-900/40 text-indigo-300"
                            title="Bind to Image or Stack"
                          >
                            <Link2 className="w-3.5 h-3.5 mr-1" />
                            {isBound ? "Change Binding" : "Bind Workload"}
                          </Button>
                          {isBound && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleUnbind(file.id)}
                              className="h-7 px-2 text-xs text-rose-400 hover:text-rose-300"
                              title="Unbind from current workload"
                            >
                              <Unlink className="w-3.5 h-3.5" />
                            </Button>
                          )}
                        </div>
                      </div>

                      {/* Candidate Suggestions */}
                      {recommendations.length > 0 && !isBound && (
                        <div className="pt-2 border-t border-slate-900 flex items-center gap-2 flex-wrap text-[11px] text-slate-400">
                          <span className="flex items-center gap-1 text-slate-500">
                            <Sparkles className="w-3 h-3 text-amber-400" />
                            Suggested:
                          </span>
                          {recommendations.slice(0, 3).map((rec, i) => (
                            <button
                              key={i}
                              type="button"
                              onClick={async () => {
                                await bindSource(serverId, {
                                  link_id: file.id,
                                  file_path: file.file_path,
                                  kind: file.kind,
                                  target_ref: rec.target_ref,
                                });
                                await fetchDirs();
                                if (onScanCompleted) onScanCompleted();
                              }}
                              className="px-2 py-0.5 rounded bg-slate-900 hover:bg-indigo-950/60 border border-slate-800 hover:border-indigo-500/40 text-slate-300 font-mono text-[10px] flex items-center gap-1 transition"
                            >
                              <span>{rec.name}</span>
                              <span className="text-amber-400">({Math.round(rec.score * 100)}%)</span>
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}

        {/* Sub-Dialog: File Content Previewer */}
        {previewFile && (
          <div className="p-3.5 rounded-xl bg-slate-950 border border-slate-800 space-y-2">
            <div className="flex items-center justify-between text-xs">
              <span className="font-mono text-slate-300 font-semibold truncate max-w-md">
                {previewFile.path} (Read-Only)
              </span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => {
                    navigator.clipboard.writeText(previewFile.content);
                    setPreviewCopied(true);
                    setTimeout(() => setPreviewCopied(false), 2000);
                  }}
                  className="text-slate-400 hover:text-white flex items-center gap-1 text-[11px]"
                >
                  {previewCopied ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
                  <span>{previewCopied ? "Copied" : "Copy"}</span>
                </button>
                <button
                  onClick={() => setPreviewFile(null)}
                  className="text-slate-400 hover:text-white text-xs ml-2"
                >
                  ✕
                </button>
              </div>
            </div>
            <textarea
              readOnly
              value={previewFile.content}
              className="w-full h-48 font-mono text-xs rounded-lg border border-slate-800 bg-slate-900 p-3 text-slate-200 resize-none"
            />
          </div>
        )}

        {/* Sub-Dialog: Manual Binding Selector */}
        {bindingFile && (
          <div className="p-3.5 rounded-xl bg-slate-900/90 border border-indigo-500/30 space-y-3">
            <div className="flex items-center justify-between text-xs font-semibold text-slate-200">
              <span className="flex items-center gap-1.5">
                <Link2 className="w-4 h-4 text-indigo-400" />
                <span>Bind Source File: <code className="text-indigo-300 font-mono">{bindingFile.file_path}</code></span>
              </span>
              <button onClick={() => setBindingFile(null)} className="text-slate-400 hover:text-white">✕</button>
            </div>

            <div className="space-y-2">
              <label className="block text-xs text-slate-300">
                Select Candidate Container Image:
              </label>
              <select
                value={selectedTargetRef}
                onChange={(e) => setSelectedTargetRef(e.target.value)}
                className="w-full text-xs font-mono rounded-lg border border-slate-700 bg-slate-950 p-2 text-white"
              >
                <option value="">-- Choose Target Image on this Server --</option>
                {availableImages.map((img) => {
                  const tag = img.repo_tags && img.repo_tags.length > 0 ? img.repo_tags[0] : img.id.slice(0, 12);
                  return (
                    <option key={img.id} value={tag}>
                      {tag} ({img.id.replace(/^sha256:/, "").slice(0, 12)})
                    </option>
                  );
                })}
              </select>

              <div className="flex items-center justify-between pt-1">
                <span className="text-[11px] text-slate-400">
                  Or enter manual identifier:
                </span>
                <input
                  type="text"
                  value={selectedTargetRef}
                  onChange={(e) => setSelectedTargetRef(e.target.value)}
                  placeholder="e.g. redis:7.0-alpine"
                  className="text-xs font-mono rounded border border-slate-750 bg-slate-950 px-2 py-1 text-white w-60"
                />
              </div>
            </div>

            <div className="flex items-center justify-end gap-2 pt-2 border-t border-slate-800">
              <Button variant="ghost" size="sm" onClick={() => setBindingFile(null)}>
                Cancel
              </Button>
              <Button
                size="sm"
                onClick={handleApplyBind}
                disabled={isBinding || !selectedTargetRef.trim()}
                className="bg-indigo-600 hover:bg-indigo-500 text-white"
              >
                {isBinding ? "Binding..." : "Confirm & Bind Workload"}
              </Button>
            </div>
          </div>
        )}

        <div className="flex items-center justify-end pt-3 border-t border-slate-800">
          <Button variant="ghost" onClick={onClose} disabled={isScanning}>
            Close
          </Button>
        </div>
      </div>
    </Modal>
  );
}
