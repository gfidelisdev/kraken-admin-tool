"use client";

import React, { useState, useEffect, useRef, useCallback } from "react";
import {
  FileCode,
  Hammer,
  AlertCircle,
  CheckCircle2,
  RefreshCw,
  Terminal as TerminalIcon,
  Copy,
  Check,
  Download,
  Layers,
  ChevronDown,
  ChevronUp,
  Link2,
  Unlink,
  FolderSearch,
  Sparkles,
} from "lucide-react";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import {
  getImageDockerfile,
  saveImageDockerfile,
  buildImageStream,
  listDiscoveredFiles,
  bindSource,
  unbindSource,
} from "@/lib/api";
import {
  DockerImage,
  DockerBuildMessage,
  ImageDockerfileResponse,
  SourceLink,
} from "@/types";

interface DockerfileModalProps {
  isOpen: boolean;
  onClose: () => void;
  image: DockerImage | null;
  serverId?: number;
  onSuccess?: () => void;
}

export function DockerfileModal({
  isOpen,
  onClose,
  image,
  serverId,
  onSuccess,
}: DockerfileModalProps) {
  const [activeTab, setActiveTab] = useState<"full" | "override">("full");
  const [fullDockerfile, setFullDockerfile] = useState("");
  const [extendDockerfile, setExtendDockerfile] = useState("");
  const [targetTag, setTargetTag] = useState("");
  const [noCache, setNoCache] = useState(false);
  const [pullParent, setPullParent] = useState(false);
  const [source, setSource] = useState<string>("");
  const [filePath, setFilePath] = useState<string>("");
  const [isReadOnly, setIsReadOnly] = useState<boolean>(false);
  const [matchScore, setMatchScore] = useState<number>(0);

  // Source discovery and manual binding state
  const [discoveredFiles, setDiscoveredFiles] = useState<SourceLink[]>([]);
  const [selectedDiscoveredPath, setSelectedDiscoveredPath] = useState<string>("");
  const [showSourcePicker, setShowSourcePicker] = useState<boolean>(false);
  const [isBindingSource, setIsBindingSource] = useState<boolean>(false);

  const [isLoading, setIsLoading] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isBuilding, setIsBuilding] = useState(false);
  const [buildSuccess, setBuildSuccess] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // Build stream state
  const [buildLogs, setBuildLogs] = useState<string[]>([]);
  const terminalEndRef = useRef<HTMLDivElement>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (terminalEndRef.current && isBuilding) {
      terminalEndRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [buildLogs, isBuilding]);

  const loadData = useCallback(async () => {
    if (!image) return;
    setIsLoading(true);
    setError(null);

    const primaryTag = image.repo_tags && image.repo_tags.length > 0 && image.repo_tags[0] !== "<none>:<none>"
      ? image.repo_tags[0]
      : `image-${image.id.replace(/^sha256:/, "").slice(0, 12)}:latest`;
    setTargetTag(primaryTag);

    const targetServerId = serverId || 1;

    try {
      const [resp, discovered] = await Promise.all([
        getImageDockerfile(image.id, serverId),
        listDiscoveredFiles(targetServerId, "image").catch(() => [] as SourceLink[]),
      ]);

      setFullDockerfile(resp.dockerfile || "");
      setExtendDockerfile(resp.extend_dockerfile || "");
      setSource(resp.source || "reconstructed");
      setFilePath(resp.file_path || "");
      setIsReadOnly(Boolean(resp.is_read_only));
      setMatchScore(resp.match_score || 0);

      const files = Array.isArray(discovered) ? discovered : [];
      setDiscoveredFiles(files);
      if (resp.file_path) {
        setSelectedDiscoveredPath(resp.file_path);
      } else if (files.length > 0) {
        setSelectedDiscoveredPath(files[0].file_path);
      }
      setActiveTab("full");
    } catch (err: any) {
      setError(err?.message || "Failed to load Dockerfile for this image");
      setFullDockerfile(`# Dockerfile for image ${primaryTag}\nFROM ${primaryTag}\n\n# Add your customization instructions here\n`);
      setExtendDockerfile(`FROM ${primaryTag}\n\n# Override environment or commands\n`);
    } finally {
      setIsLoading(false);
    }
  }, [image, serverId]);

  useEffect(() => {
    if (!isOpen) {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
        abortControllerRef.current = null;
      }
      setIsBuilding(false);
      setBuildSuccess(false);
      setBuildLogs([]);
      setError(null);
      setShowSourcePicker(false);
      return;
    }

    setBuildLogs([]);
    setBuildSuccess(false);
    setError(null);

    if (image) {
      loadData();
    } else {
      // New Image mode
      setTargetTag("my-custom-image:latest");
      setFullDockerfile(`FROM alpine:latest\n\n# Install packages and configure\nRUN apk add --no-cache curl bash\n\nWORKDIR /app\n\nCMD ["sh"]\n`);
      setExtendDockerfile("");
      setSource("custom");
      setFilePath("");
      setIsReadOnly(false);
      setMatchScore(0);
      setDiscoveredFiles([]);
      setActiveTab("full");
      setIsLoading(false);
    }
  }, [isOpen, image, serverId, loadData]);

  const handleUnbind = async () => {
    if (!image || !filePath) return;
    setIsBindingSource(true);
    setError(null);
    try {
      await unbindSource(serverId || 1, { file_path: filePath });
      await loadData();
    } catch (err: any) {
      setError(err?.message || "Failed to unbind source file");
    } finally {
      setIsBindingSource(false);
    }
  };

  const handleBind = async (path: string) => {
    if (!image || !path) return;
    setIsBindingSource(true);
    setError(null);
    try {
      await bindSource(serverId || 1, {
        file_path: path,
        kind: "image",
        target_ref: image.id,
      });
      await loadData();
      setShowSourcePicker(false);
    } catch (err: any) {
      setError(err?.message || "Failed to bind source file to this image");
    } finally {
      setIsBindingSource(false);
    }
  };

  const activeDockerfile = activeTab === "full" ? fullDockerfile : extendDockerfile;
  const setActiveDockerfile = (val: string) => {
    if (activeTab === "full") {
      setFullDockerfile(val);
    } else {
      setExtendDockerfile(val);
    }
  };

  const handleCopy = () => {
    if (!activeDockerfile) return;
    navigator.clipboard.writeText(activeDockerfile);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleDownload = () => {
    if (!activeDockerfile) return;
    const blob = new Blob([activeDockerfile], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "Dockerfile";
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleSaveOnly = async () => {
    if (!targetTag.trim() || !activeDockerfile.trim()) {
      setError("Target tag and Dockerfile content are required");
      return;
    }
    setIsSaving(true);
    setError(null);
    try {
      await saveImageDockerfile(
        {
          tag: targetTag.trim(),
          image_id: image?.id,
          dockerfile: activeDockerfile,
        },
        serverId
      );
      setSource("database");
    } catch (err: any) {
      setError(err?.message || "Failed to save Dockerfile");
    } finally {
      setIsSaving(false);
    }
  };

  const handleStartBuild = async () => {
    const cleanTag = targetTag.trim();
    if (!cleanTag) {
      setError("A valid image tag (e.g. repo:tag) is required");
      return;
    }
    const cleanDf = activeDockerfile.trim();
    if (!cleanDf) {
      setError("Dockerfile content cannot be empty");
      return;
    }

    setIsBuilding(true);
    setBuildSuccess(false);
    setError(null);
    setBuildLogs([`[Kraken] Initiating build for tag: ${cleanTag}...`]);

    const abortController = new AbortController();
    abortControllerRef.current = abortController;

    try {
      await buildImageStream(
        {
          tag: cleanTag,
          dockerfile: cleanDf,
          no_cache: noCache,
          pull_parent: pullParent,
        },
        (msg: DockerBuildMessage) => {
          if (msg.stream) {
            setBuildLogs((prev) => [...prev, msg.stream!.replace(/\r/g, "")]);
          } else if (msg.status) {
            setBuildLogs((prev) => [...prev, `Status: ${msg.status}`]);
          }
        },
        abortController.signal,
        serverId
      );

      setBuildSuccess(true);
      setBuildLogs((prev) => [...prev, `\n[Kraken] Image '${cleanTag}' successfully built and registered!`]);
      if (onSuccess) {
        onSuccess();
      }
    } catch (err: any) {
      if (err.name === "AbortError") {
        setBuildLogs((prev) => [...prev, "\n[Kraken] Build cancelled by user."]);
      } else {
        setError(err?.message || "Docker build failed");
        setBuildLogs((prev) => [...prev, `\n[Kraken] Error: ${err?.message || "Build failed"}`]);
      }
    } finally {
      setIsBuilding(false);
      abortControllerRef.current = null;
    }
  };

  const handleCancelBuild = () => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
    }
    setIsBuilding(false);
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={isBuilding ? handleCancelBuild : onClose}
      title={image ? `Dockerfile & Rebuild: ${targetTag}` : "Build New Docker Image"}
      description={
        image
          ? "Inspect, customize parameters in the Dockerfile and rebuild this container image on the fly."
          : "Write a Dockerfile and compile it into a new local container image."
      }
      maxWidth="xl"
    >
      <div className="space-y-4">
        {/* Source Badge & Tabs */}
        {image && (
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2.5 pb-2 border-b border-slate-800">
            <div className="flex items-center gap-2">
              <span className="text-xs text-slate-400">Mode:</span>
              <div className="flex items-center rounded-lg bg-slate-950 p-0.5 border border-slate-800">
                <button
                  type="button"
                  onClick={() => setActiveTab("full")}
                  className={`px-3 py-1 rounded-md text-xs font-medium transition ${
                    activeTab === "full"
                      ? "bg-sky-500 text-white shadow"
                      : "text-slate-400 hover:text-white"
                  }`}
                >
                  Full / Reconstructed
                </button>
                <button
                  type="button"
                  onClick={() => setActiveTab("override")}
                  className={`px-3 py-1 rounded-md text-xs font-medium transition ${
                    activeTab === "override"
                      ? "bg-sky-500 text-white shadow"
                      : "text-slate-400 hover:text-white"
                  }`}
                >
                  Quick Override (FROM Base)
                </button>
              </div>
            </div>

            <div className="flex items-center gap-2 flex-wrap">
              {source === "source_file" && (
                <span className="text-[10px] font-medium px-2 py-0.5 rounded-full bg-indigo-500/10 text-indigo-400 border border-indigo-500/20" title={`Host file: ${filePath} (Confidence: ${Math.round(matchScore * 100)}%)`}>
                  Discovered Host Source (Read-Only)
                </span>
              )}
              {source === "database" && (
                <span className="text-[10px] font-medium px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                  Customized DB Override
                </span>
              )}
              {source === "host_file" && (
                <span className="text-[10px] font-medium px-2 py-0.5 rounded-full bg-sky-500/10 text-sky-400 border border-sky-500/20">
                  Loaded from Host File
                </span>
              )}
              {source === "reconstructed" && (
                <span className="text-[10px] font-medium px-2 py-0.5 rounded-full bg-amber-500/10 text-amber-400 border border-amber-500/20">
                  Reconstructed from Layers
                </span>
              )}
            </div>
          </div>
        )}

        {/* Tag & Options Bar */}
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 items-end bg-slate-950/60 p-3 rounded-xl border border-slate-800">
          <div>
            <label className="block text-xs font-medium text-slate-300 mb-1">
              Target Image Tag
            </label>
            <Input
              value={targetTag}
              onChange={(e) => setTargetTag(e.target.value)}
              placeholder="e.g. my-app:latest"
              disabled={isBuilding}
              className="font-mono text-xs bg-slate-900 border-slate-750 text-white"
            />
          </div>

          <div className="flex items-center gap-4 pb-1">
            <label className="flex items-center gap-2 text-xs text-slate-300 cursor-pointer">
              <input
                type="checkbox"
                checked={noCache}
                onChange={(e) => setNoCache(e.target.checked)}
                disabled={isBuilding}
                className="rounded border-slate-750 bg-slate-900 text-sky-500 focus:ring-sky-500"
              />
              <span>No Cache (<code className="text-[11px] text-slate-400">--no-cache</code>)</span>
            </label>

            <label className="flex items-center gap-2 text-xs text-slate-300 cursor-pointer">
              <input
                type="checkbox"
                checked={pullParent}
                onChange={(e) => setPullParent(e.target.checked)}
                disabled={isBuilding}
                className="rounded border-slate-750 bg-slate-900 text-sky-500 focus:ring-sky-500"
              />
              <span>Pull Base (<code className="text-[11px] text-slate-400">--pull</code>)</span>
            </label>
          </div>
        </div>

        {/* Error Notification */}
        {error && (
          <div className="p-3 rounded-xl bg-rose-500/10 border border-rose-500/20 text-rose-300 text-xs flex items-center justify-between">
            <div className="flex items-center gap-2">
              <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
              <span>{error}</span>
            </div>
            <button onClick={() => setError(null)} className="text-rose-400 hover:text-white text-xs">
              ✕
            </button>
          </div>
        )}

        {/* Source File Location Callout & Binding Controls */}
        {source === "source_file" ? (
          <div className="p-3 rounded-xl bg-indigo-500/10 border border-indigo-500/20 text-indigo-300 text-xs flex flex-col sm:flex-row sm:items-center justify-between gap-2.5">
            <div className="flex items-center gap-2 truncate">
              <span className="font-semibold shrink-0 text-white flex items-center gap-1.5">
                <Link2 className="h-3.5 w-3.5 text-indigo-400" />
                Host Source:
              </span>
              <code className="text-[11px] text-indigo-200 truncate font-mono bg-indigo-950/60 px-2 py-0.5 rounded border border-indigo-500/20">
                {filePath}
              </code>
              {matchScore > 0 && (
                <span className="text-[10px] px-1.5 py-0.5 rounded bg-indigo-500/20 text-indigo-300 font-medium">
                  {Math.round(matchScore * 100)}% match
                </span>
              )}
            </div>

            <div className="flex items-center gap-2 shrink-0">
              <span className="text-[10px] uppercase font-bold tracking-wider px-2 py-0.5 rounded bg-indigo-500/20 text-indigo-200">
                Read-Only
              </span>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={handleUnbind}
                disabled={isBindingSource}
                title="Unlink this host Dockerfile and return to layer reconstruction"
                className="h-6 px-2 text-[11px] border-indigo-500/30 text-indigo-300 hover:text-white hover:bg-indigo-500/20"
              >
                <Unlink className="h-3 w-3 mr-1" />
                Unlink
              </Button>
            </div>
          </div>
        ) : (
          image && (
            <div className="p-2.5 rounded-xl bg-slate-900/80 border border-slate-800 text-xs">
              <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2 text-slate-400">
                  <FolderSearch className="h-3.5 w-3.5 text-sky-400" />
                  <span>
                    {discoveredFiles.length > 0
                      ? `${discoveredFiles.length} host Dockerfile${discoveredFiles.length === 1 ? "" : "s"} discovered on host`
                      : "No host Dockerfiles discovered yet"}
                  </span>
                </div>
                {discoveredFiles.length > 0 && (
                  <button
                    type="button"
                    onClick={() => setShowSourcePicker(!showSourcePicker)}
                    className="text-[11px] font-medium text-sky-400 hover:text-sky-300 flex items-center gap-1 transition"
                  >
                    <span>{showSourcePicker ? "Hide Picker" : "Link Discovered Host Dockerfile"}</span>
                    {showSourcePicker ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
                  </button>
                )}
              </div>

              {showSourcePicker && discoveredFiles.length > 0 && (
                <div className="mt-2.5 pt-2.5 border-t border-slate-800 flex flex-col sm:flex-row items-stretch sm:items-center gap-2">
                  <select
                    value={selectedDiscoveredPath}
                    onChange={(e) => setSelectedDiscoveredPath(e.target.value)}
                    className="flex-1 text-xs rounded-lg border border-slate-750 bg-slate-950 px-2.5 py-1.5 text-slate-200 focus:outline-none focus:border-sky-500 font-mono"
                  >
                    <option value="">-- Select a discovered host Dockerfile --</option>
                    {discoveredFiles.map((f) => (
                      <option key={f.id} value={f.file_path}>
                        {f.file_path} {f.match_score > 0 ? `(${Math.round(f.match_score * 100)}% match)` : ""}
                      </option>
                    ))}
                  </select>
                  <Button
                    type="button"
                    size="sm"
                    onClick={() => handleBind(selectedDiscoveredPath)}
                    disabled={isBindingSource || !selectedDiscoveredPath}
                    className="h-8 px-3 text-xs bg-indigo-600 hover:bg-indigo-500 text-white shrink-0"
                  >
                    {isBindingSource ? (
                      <>
                        <RefreshCw className="h-3 w-3 mr-1 animate-spin" />
                        Linking...
                      </>
                    ) : (
                      <>
                        <Link2 className="h-3 w-3 mr-1" />
                        Link to Image
                      </>
                    )}
                  </Button>
                </div>
              )}
            </div>
          )
        )}

        {/* Code Editor */}
        <div className="space-y-1.5">
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold uppercase tracking-wider text-slate-400 flex items-center gap-1.5">
              <FileCode className="h-4 w-4 text-sky-400" />
              <span>Dockerfile Content</span>
            </span>

            <div className="flex items-center gap-1.5">
              <button
                type="button"
                onClick={handleCopy}
                disabled={isLoading || !activeDockerfile}
                className="p-1 rounded text-slate-400 hover:text-white transition flex items-center gap-1 text-xs"
                title="Copy Dockerfile"
              >
                {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                <span>{copied ? "Copied" : "Copy"}</span>
              </button>
              <button
                type="button"
                onClick={handleDownload}
                disabled={isLoading || !activeDockerfile}
                className="p-1 rounded text-slate-400 hover:text-white transition flex items-center gap-1 text-xs"
                title="Download Dockerfile"
              >
                <Download className="h-3.5 w-3.5" />
                <span>Download</span>
              </button>
            </div>
          </div>

          <div className="relative">
            {isLoading ? (
              <div className="h-64 flex flex-col items-center justify-center rounded-xl border border-slate-800 bg-slate-950 text-slate-400">
                <RefreshCw className="h-6 w-6 animate-spin text-sky-400 mb-2" />
                <span className="text-xs">Inspecting image layers and Dockerfile...</span>
              </div>
            ) : (
              <textarea
                value={activeDockerfile}
                onChange={(e) => setActiveDockerfile(e.target.value)}
                disabled={isBuilding}
                rows={11}
                spellCheck={false}
                onKeyDown={(e) => {
                  if (e.key === "Tab") {
                    e.preventDefault();
                    const start = e.currentTarget.selectionStart;
                    const end = e.currentTarget.selectionEnd;
                    const val = activeDockerfile;
                    setActiveDockerfile(val.substring(0, start) + "  " + val.substring(end));
                    setTimeout(() => {
                      e.currentTarget.selectionStart = e.currentTarget.selectionEnd = start + 2;
                    }, 0);
                  }
                }}
                className="w-full font-mono text-xs rounded-xl border border-slate-800 bg-slate-950 p-3.5 text-slate-200 focus:outline-none focus:border-sky-500 leading-relaxed resize-y"
                placeholder="# Enter Dockerfile instructions here..."
              />
            )}
            {source === "source_file" && (
              <p className="text-[11px] text-slate-500 italic mt-1">
                Discovered host file is read-only. Edits saved here will be safely stored in the database override table without modifying the host filesystem.
              </p>
            )}
          </div>
        </div>

        {/* Live Build Stream Console */}
        {buildLogs.length > 0 && (
          <div className="space-y-1.5">
            <div className="flex items-center justify-between text-xs font-semibold text-slate-400">
              <span className="flex items-center gap-1.5">
                <TerminalIcon className="h-3.5 w-3.5 text-emerald-400" />
                <span>Build Console Output</span>
              </span>
              {isBuilding && (
                <span className="flex items-center gap-1 text-sky-400 text-[11px]">
                  <RefreshCw className="h-3 w-3 animate-spin" />
                  Compiling image...
                </span>
              )}
              {buildSuccess && (
                <span className="flex items-center gap-1 text-emerald-400 text-[11px]">
                  <CheckCircle2 className="h-3.5 w-3.5" />
                  Success
                </span>
              )}
            </div>

            <div className="h-44 overflow-y-auto rounded-xl border border-slate-800 bg-black/90 p-3 font-mono text-[11px] text-slate-300 space-y-0.5 leading-relaxed">
              {buildLogs.map((log, idx) => (
                <div key={idx} className="whitespace-pre-wrap break-all">
                  {log}
                </div>
              ))}
              <div ref={terminalEndRef} />
            </div>
          </div>
        )}

        {/* Action Buttons */}
        <div className="flex items-center justify-between pt-3 border-t border-slate-800">
          <div>
            {image && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={handleSaveOnly}
                disabled={isLoading || isSaving || isBuilding || !activeDockerfile.trim()}
                className="text-xs border-slate-700 bg-slate-900/60 hover:bg-slate-800 text-slate-300"
              >
                {isSaving ? "Saving..." : "Save Dockerfile Only"}
              </Button>
            )}
          </div>

          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={isBuilding ? handleCancelBuild : onClose}
              className="text-xs border-slate-700 bg-slate-900 hover:bg-slate-800 text-slate-300"
            >
              {isBuilding ? "Cancel Build" : buildSuccess ? "Close" : "Cancel"}
            </Button>

            <Button
              type="button"
              size="sm"
              onClick={handleStartBuild}
              disabled={isLoading || isBuilding || !activeDockerfile.trim() || !targetTag.trim()}
              className="text-xs bg-sky-500 hover:bg-sky-400 text-white shadow-lg shadow-sky-500/20"
            >
              {isBuilding ? (
                <>
                  <RefreshCw className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                  Building Image...
                </>
              ) : (
                <>
                  <Hammer className="h-3.5 w-3.5 mr-1.5" />
                  {image ? "Rebuild Image" : "Build Image"}
                </>
              )}
            </Button>
          </div>
        </div>
      </div>
    </Modal>
  );
}
