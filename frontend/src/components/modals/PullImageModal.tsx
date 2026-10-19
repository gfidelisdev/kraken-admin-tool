"use client";

import React, { useState, useEffect, useRef, useMemo } from "react";
import {
  Download,
  AlertCircle,
  CheckCircle2,
  Search,
  Star,
  Layers,
  Terminal as TerminalIcon,
  ChevronDown,
  ChevronUp,
  Loader2,
  X,
  RotateCw,
} from "lucide-react";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { api, pullImageStream } from "@/lib/api";
import { ImageSearchResult, DockerPullMessage } from "@/types";

interface PullImageModalProps {
  isOpen: boolean;
  onClose: () => void;
  onPull?: (imageName: string) => Promise<void>;
  onSuccess?: () => void;
}

interface LayerProgress {
  id: string;
  status: string;
  current: number;
  total: number;
  percent: number;
}

function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`;
}

function formatStarCount(stars: number): string {
  if (stars >= 1000) {
    return `${(stars / 1000).toFixed(1)}k`;
  }
  return stars.toString();
}

export function PullImageModal({
  isOpen,
  onClose,
  onPull,
  onSuccess,
}: PullImageModalProps) {
  const [imageName, setImageName] = useState("");
  const [mode, setMode] = useState<"input" | "pulling" | "success" | "error">("input");
  const [error, setError] = useState<string | null>(null);

  // Search autocomplete state
  const [searchResults, setSearchResults] = useState<ImageSearchResult[]>([]);
  const [isSearching, setIsSearching] = useState(false);
  const [showSuggestions, setShowSuggestions] = useState(false);
  const searchContainerRef = useRef<HTMLDivElement>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

  // Pull progress state
  const [globalStatus, setGlobalStatus] = useState("Initializing download...");
  const [layers, setLayers] = useState<Record<string, LayerProgress>>({});
  const [rawLogs, setRawLogs] = useState<string[]>([]);
  const [showLogs, setShowLogs] = useState(false);

  // Reset state on open/close
  useEffect(() => {
    if (isOpen) {
      setMode("input");
      setError(null);
      setLayers({});
      setRawLogs([]);
      setGlobalStatus("Initializing download...");
      setShowSuggestions(false);
    } else {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
        abortControllerRef.current = null;
      }
    }
  }, [isOpen]);

  // Click outside suggestions dropdown to close it
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (
        searchContainerRef.current &&
        !searchContainerRef.current.contains(event.target as Node)
      ) {
        setShowSuggestions(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  // Debounced search when user types image name
  useEffect(() => {
    if (mode !== "input") return;

    const queryTerm = imageName.trim().split(":")[0];
    if (queryTerm.length < 2) {
      setSearchResults([]);
      setShowSuggestions(false);
      return;
    }

    const timer = setTimeout(async () => {
      setIsSearching(true);
      try {
        const res = await api.get<ImageSearchResult[]>(
          `/api/docker/images/search?term=${encodeURIComponent(queryTerm)}`
        );
        const results = Array.isArray(res) ? res : [];
        setSearchResults(results);
        if (results.length > 0) {
          setShowSuggestions(true);
        }
      } catch (err) {
        // Silently catch search errors
        console.warn("Failed to fetch image suggestions:", err);
      } finally {
        setIsSearching(false);
      }
    }, 280);

    return () => clearTimeout(timer);
  }, [imageName, mode]);

  // Handle message from stream
  const handleStreamMessage = (msg: DockerPullMessage) => {
    if (msg.status) {
      setGlobalStatus(msg.status);
    }

    // Append raw log message
    if (msg.status || msg.progress || msg.error) {
      const line = [msg.id, msg.status, msg.progress, msg.error]
        .filter(Boolean)
        .join(" - ");
      if (line) {
        setRawLogs((prev) => [...prev.slice(-60), line]);
      }
    }

    // Update layer info
    if (msg.id) {
      setLayers((prev) => {
        const existing = prev[msg.id!] || {
          id: msg.id!,
          status: msg.status || "",
          current: 0,
          total: 0,
          percent: 0,
        };

        const current = msg.progressDetail?.current ?? existing.current;
        const total = msg.progressDetail?.total ?? existing.total;
        let percent = existing.percent;

        if (total > 0 && current > 0) {
          percent = Math.min(100, Math.round((current / total) * 100));
        } else if (
          msg.status?.toLowerCase().includes("complete") ||
          msg.status?.toLowerCase().includes("already exists")
        ) {
          percent = 100;
        }

        return {
          ...prev,
          [msg.id!]: {
            id: msg.id!,
            status: msg.status || existing.status,
            current,
            total,
            percent,
          },
        };
      });
    }
  };

  // Calculate overall percentage
  const overallPercent = useMemo(() => {
    const layerList = Object.values(layers);
    if (layerList.length === 0) {
      return mode === "success" ? 100 : 0;
    }

    let totalBytes = 0;
    let currentBytes = 0;
    let totalLayersCount = layerList.length;
    let completedLayersCount = 0;

    for (const l of layerList) {
      if (l.total > 0) {
        totalBytes += l.total;
        currentBytes += l.current;
      }
      if (
        l.percent === 100 ||
        l.status.toLowerCase().includes("complete") ||
        l.status.toLowerCase().includes("already exists")
      ) {
        completedLayersCount++;
      }
    }

    if (totalBytes > 0) {
      const bytePercent = Math.round((currentBytes / totalBytes) * 100);
      return Math.min(100, bytePercent);
    }

    // Fallback: layer count percentage
    return Math.min(100, Math.round((completedLayersCount / totalLayersCount) * 100));
  }, [layers, mode]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const targetImage = imageName.trim();
    if (!targetImage) return;

    setShowSuggestions(false);
    setMode("pulling");
    setError(null);
    setLayers({});
    setRawLogs([]);
    setGlobalStatus(`Pulling image: ${targetImage}...`);

    const abortController = new AbortController();
    abortControllerRef.current = abortController;

    try {
      await pullImageStream(targetImage, handleStreamMessage, abortController.signal);
      setMode("success");
      setGlobalStatus("Image downloaded and registered successfully!");

      // Trigger callers' refresh hooks
      if (onSuccess) {
        onSuccess();
      } else if (onPull) {
        // Trigger for backward compatibility without duplicating post
        onPull(targetImage).catch(() => {});
      }
    } catch (err: any) {
      if (err.name === "AbortError") {
        setMode("input");
        setError("Download cancelled by user.");
      } else {
        setMode("error");
        setError(err?.message || "Failed to pull image from registry");
      }
    } finally {
      abortControllerRef.current = null;
    }
  };

  const handleSelectSuggestion = (name: string) => {
    setImageName(name.includes(":") ? name : `${name}:latest`);
    setShowSuggestions(false);
  };

  const handleCancelPull = () => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
    }
    setMode("input");
    setError("Download cancelled.");
  };

  const handleFinish = () => {
    setImageName("");
    onClose();
  };

  const layerList = Object.values(layers);
  const completedCount = layerList.filter(
    (l) =>
      l.percent === 100 ||
      l.status.toLowerCase().includes("complete") ||
      l.status.toLowerCase().includes("already exists")
  ).length;

  return (
    <Modal
      isOpen={isOpen}
      onClose={mode === "pulling" ? handleCancelPull : onClose}
      title={
        mode === "pulling"
          ? "Pulling Docker Image"
          : mode === "success"
          ? "Image Download Complete"
          : "Pull Docker Image"
      }
      description={
        mode === "pulling"
          ? `Streaming download progress for ${imageName}...`
          : mode === "success"
          ? "The image is now available in your local Docker registry."
          : "Search and download an image from Docker Hub or a remote container registry."
      }
      maxWidth="lg"
    >
      {mode === "input" ? (
        <form onSubmit={handleSubmit} className="space-y-4">
          <div ref={searchContainerRef} className="relative">
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-slate-300">
                Image Name & Tag
              </label>
              {isSearching && (
                <div className="flex items-center space-x-1 text-[11px] text-sky-400">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  <span>Searching Docker Hub...</span>
                </div>
              )}
            </div>

            <div className="mt-1 relative">
              <Input
                placeholder="e.g. nginx:alpine, redis, or postgres:16-alpine"
                value={imageName}
                onChange={(e) => setImageName(e.target.value)}
                onFocus={() => {
                  if (searchResults.length > 0) setShowSuggestions(true);
                }}
                autoFocus
              />
              {imageName && (
                <button
                  type="button"
                  onClick={() => {
                    setImageName("");
                    setSearchResults([]);
                    setShowSuggestions(false);
                  }}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              )}
            </div>

            {/* Autocomplete Dropdown */}
            {showSuggestions && searchResults.length > 0 && (
              <div className="absolute left-0 right-0 z-50 mt-1.5 max-h-64 overflow-y-auto rounded-xl border border-slate-700/80 bg-slate-900/95 p-1.5 shadow-2xl backdrop-blur-md animate-in fade-in-50 zoom-in-95">
                <div className="px-2 py-1 text-[10px] font-semibold uppercase tracking-wider text-slate-400 border-b border-slate-800">
                  Docker Hub Suggestions
                </div>
                <div className="divide-y divide-slate-800/40">
                  {searchResults.map((item) => (
                    <button
                      key={item.name}
                      type="button"
                      onClick={() => handleSelectSuggestion(item.name)}
                      className="w-full rounded-lg px-2.5 py-2 text-left hover:bg-slate-800/80 transition-colors flex items-start justify-between gap-3 group"
                    >
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center space-x-2">
                          <span className="font-mono text-xs font-semibold text-white group-hover:text-sky-300 transition-colors">
                            {item.name}
                          </span>
                          {item.is_official && (
                            <span className="inline-flex items-center space-x-1 rounded-full bg-emerald-500/10 border border-emerald-500/30 px-1.5 py-0.2 text-[10px] font-medium text-emerald-400">
                              <CheckCircle2 className="h-2.5 w-2.5" />
                              <span>Official</span>
                            </span>
                          )}
                        </div>
                        {item.description && (
                          <p className="mt-0.5 line-clamp-1 text-[11px] text-slate-400 group-hover:text-slate-300">
                            {item.description}
                          </p>
                        )}
                      </div>

                      {item.star_count > 0 && (
                        <div className="flex items-center space-x-1 shrink-0 text-amber-400/90 text-xs font-medium">
                          <Star className="h-3 w-3 fill-amber-400/90" />
                          <span>{formatStarCount(item.star_count)}</span>
                        </div>
                      )}
                    </button>
                  ))}
                </div>
              </div>
            )}
          </div>

          {/* Quick select templates */}
          <div className="space-y-1.5">
            <span className="text-[11px] text-slate-400">Popular templates:</span>
            <div className="flex flex-wrap gap-1.5">
              {[
                "nginx:alpine",
                "redis:alpine",
                "postgres:16-alpine",
                "node:20-alpine",
                "golang:1.24-alpine",
                "python:3.11-slim",
                "ubuntu:22.04",
              ].map((img) => (
                <button
                  key={img}
                  type="button"
                  onClick={() => handleSelectSuggestion(img)}
                  className="rounded-md border border-slate-800 bg-slate-950/60 px-2 py-0.5 text-xs text-slate-400 hover:border-slate-700 hover:text-white transition-colors"
                >
                  {img}
                </button>
              ))}
            </div>
          </div>

          {error && (
            <div className="flex items-center space-x-2 rounded-lg border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
              <AlertCircle className="h-4 w-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          <div className="flex items-center justify-end space-x-2 pt-3 border-t border-slate-800">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant="docker"
              disabled={!imageName.trim()}
            >
              <Download className="mr-1.5 h-4 w-4" />
              Pull Image
            </Button>
          </div>
        </form>
      ) : (
        /* Pulling, Success & Error Progress View */
        <div className="space-y-4">
          {/* Header Card */}
          <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-4 space-y-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2 min-w-0">
                <span className="font-mono text-sm font-semibold text-white truncate">
                  {imageName}
                </span>
                <Badge variant="outline" className="text-[10px] text-sky-400 border-sky-500/30">
                  Docker Hub
                </Badge>
              </div>
              <div className="flex items-center space-x-2 shrink-0">
                {mode === "pulling" && (
                  <span className="font-mono text-sm font-bold text-sky-400">
                    {overallPercent}%
                  </span>
                )}
                {mode === "success" && (
                  <span className="inline-flex items-center text-xs font-semibold text-emerald-400 space-x-1">
                    <CheckCircle2 className="h-4 w-4" />
                    <span>Complete</span>
                  </span>
                )}
                {mode === "error" && (
                  <span className="inline-flex items-center text-xs font-semibold text-rose-400 space-x-1">
                    <AlertCircle className="h-4 w-4" />
                    <span>Failed</span>
                  </span>
                )}
              </div>
            </div>

            {/* Global Progress Bar */}
            <div className="relative h-2 w-full overflow-hidden rounded-full bg-slate-800">
              <div
                className={`h-full transition-all duration-300 ${
                  mode === "success"
                    ? "bg-emerald-500"
                    : mode === "error"
                    ? "bg-rose-500"
                    : "bg-gradient-to-r from-sky-500 to-indigo-500"
                }`}
                style={{ width: `${Math.max(mode === "pulling" ? 4 : 0, overallPercent)}%` }}
              />
            </div>

            {/* Status description */}
            <div className="flex items-center justify-between text-xs text-slate-400">
              <span className="truncate pr-2">{globalStatus}</span>
              {layerList.length > 0 && (
                <span className="shrink-0 font-mono text-[11px] text-slate-400">
                  {completedCount}/{layerList.length} layers
                </span>
              )}
            </div>
          </div>

          {/* Layers List */}
          {layerList.length > 0 && (
            <div className="space-y-2">
              <div className="flex items-center justify-between px-1">
                <span className="flex items-center space-x-1.5 text-xs font-semibold text-slate-300">
                  <Layers className="h-3.5 w-3.5 text-slate-400" />
                  <span>Download Layers</span>
                </span>
                <span className="text-[11px] text-slate-400">
                  {completedCount} of {layerList.length} ready
                </span>
              </div>

              <div className="max-h-48 overflow-y-auto space-y-1.5 pr-1">
                {layerList.map((layer) => (
                  <div
                    key={layer.id}
                    className="flex flex-col rounded-lg border border-slate-800/70 bg-slate-900/60 p-2 text-xs space-y-1.5"
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center space-x-2">
                        <span className="font-mono text-[11px] text-slate-400">
                          {layer.id.slice(0, 12)}
                        </span>
                        <span
                          className={`text-[10px] font-medium px-1.5 py-0.2 rounded ${
                            layer.percent === 100
                              ? "bg-emerald-500/10 text-emerald-400"
                              : layer.status.toLowerCase().includes("extract")
                              ? "bg-amber-500/10 text-amber-400"
                              : "bg-sky-500/10 text-sky-400"
                          }`}
                        >
                          {layer.status}
                        </span>
                      </div>

                      <div className="font-mono text-[11px] text-slate-400">
                        {layer.total > 0 ? (
                          <span>
                            {formatBytes(layer.current)} / {formatBytes(layer.total)}
                          </span>
                        ) : (
                          <span>{layer.percent}%</span>
                        )}
                      </div>
                    </div>

                    {/* Layer progress bar */}
                    <div className="relative h-1 w-full overflow-hidden rounded-full bg-slate-800">
                      <div
                        className={`h-full transition-all duration-200 ${
                          layer.percent === 100 ? "bg-emerald-500" : "bg-sky-500"
                        }`}
                        style={{ width: `${layer.percent}%` }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Expandable Terminal Log Stream */}
          {rawLogs.length > 0 && (
            <div className="space-y-1.5 pt-1">
              <button
                type="button"
                onClick={() => setShowLogs(!showLogs)}
                className="flex items-center space-x-1.5 text-[11px] text-slate-400 hover:text-slate-200 transition-colors"
              >
                <TerminalIcon className="h-3 w-3" />
                <span>{showLogs ? "Hide detailed daemon logs" : "Show detailed daemon logs"}</span>
                {showLogs ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
              </button>

              {showLogs && (
                <div className="max-h-36 overflow-y-auto rounded-lg border border-slate-800 bg-black/80 p-2 font-mono text-[11px] text-slate-300 space-y-0.5">
                  {rawLogs.map((log, idx) => (
                    <div key={idx} className="whitespace-pre-wrap break-all leading-relaxed">
                      {log}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Error Message */}
          {error && (
            <div className="flex items-center space-x-2 rounded-lg border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
              <AlertCircle className="h-4 w-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {/* Action Buttons */}
          <div className="flex items-center justify-end space-x-2 pt-3 border-t border-slate-800">
            {mode === "pulling" && (
              <Button type="button" variant="outline" onClick={handleCancelPull}>
                Cancel Download
              </Button>
            )}

            {mode === "error" && (
              <>
                <Button type="button" variant="outline" onClick={onClose}>
                  Close
                </Button>
                <Button
                  type="button"
                  variant="docker"
                  onClick={() => setMode("input")}
                >
                  <RotateCw className="mr-1.5 h-3.5 w-3.5" />
                  Try Again
                </Button>
              </>
            )}

            {mode === "success" && (
              <Button type="button" variant="docker" onClick={handleFinish}>
                <CheckCircle2 className="mr-1.5 h-4 w-4" />
                Done
              </Button>
            )}
          </div>
        </div>
      )}
    </Modal>
  );
}
