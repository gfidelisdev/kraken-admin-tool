"use client";

import React, { useState, useEffect } from "react";
import {
  ShieldCheck,
  ArrowUpCircle,
  RefreshCw,
  AlertTriangle,
  CheckCircle2,
  AlertCircle,
  RotateCcw,
  Box,
} from "lucide-react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { checkImageUpdates, updateImage } from "@/lib/api";
import { DockerImage, ImageUpdateInfo, UpdateImageResult } from "@/types";

interface ImageUpdateModalProps {
  isOpen: boolean;
  onClose: () => void;
  image: DockerImage | null;
  serverId?: number;
  onSuccess?: () => void;
}

export function ImageUpdateModal({
  isOpen,
  onClose,
  image,
  serverId = 1,
  onSuccess,
}: ImageUpdateModalProps) {
  const [updateInfo, setUpdateInfo] = useState<ImageUpdateInfo | null>(null);
  const [selectedMode, setSelectedMode] = useState<"retain" | "full">("retain");
  const [recreateContainers, setRecreateContainers] = useState(true);
  const [removeOldImage, setRemoveOldImage] = useState(false);

  const [isLoading, setIsLoading] = useState(false);
  const [isUpdating, setIsUpdating] = useState(false);
  const [result, setResult] = useState<UpdateImageResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!isOpen || !image) {
      setUpdateInfo(null);
      setResult(null);
      setError(null);
      setIsUpdating(false);
      return;
    }

    setIsLoading(true);
    setError(null);
    setResult(null);

    const primaryTag = image.repo_tags && image.repo_tags.length > 0 && image.repo_tags[0] !== "<none>:<none>"
      ? image.repo_tags[0]
      : image.id;

    checkImageUpdates(serverId, primaryTag)
      .then((data: any) => {
        const info: ImageUpdateInfo = data?.data ? data.data[0] : data;
        setUpdateInfo(info);
        if (info.retain_has_update) {
          setSelectedMode("retain");
        } else if (info.full_has_update) {
          setSelectedMode("full");
        }
      })
      .catch((err: any) => {
        setError(err?.message || "Failed to query Docker Hub for updates");
      })
      .finally(() => {
        setIsLoading(false);
      });
  }, [isOpen, image, serverId]);

  const handleApplyUpdate = async () => {
    if (!image || !updateInfo) return;
    const targetTag = selectedMode === "retain" ? updateInfo.retain_tag : updateInfo.full_tag;
    if (!targetTag) {
      setError("No target tag available for selected mode");
      return;
    }

    setIsUpdating(true);
    setError(null);
    setResult(null);

    try {
      const res = await updateImage({
        server_id: serverId,
        image_id: image.id,
        target_tag: `${updateInfo.repository}:${targetTag}`,
        mode: selectedMode,
        recreate_containers: recreateContainers,
        remove_old_image: removeOldImage,
      });

      setResult(res);
      if (res.rolled_back) {
        setError(res.error_message || "Container recreation failed; safe rollback executed.");
      } else if (onSuccess) {
        onSuccess();
      }
    } catch (err: any) {
      setError(err?.message || "Failed to update image");
    } finally {
      setIsUpdating(false);
    }
  };

  const primaryTag = image?.repo_tags && image.repo_tags.length > 0 ? image.repo_tags[0] : image?.id.slice(0, 12);

  return (
    <Modal
      isOpen={isOpen}
      onClose={isUpdating ? () => {} : onClose}
      title={`Update Image: ${primaryTag}`}
      description="Compare and update this image directly from Docker Hub to remediate security vulnerabilities."
      maxWidth="lg"
    >
      <div className="space-y-4">
        {error && (
          <div className="p-3 rounded-xl bg-rose-500/10 border border-rose-500/20 text-rose-300 text-xs flex items-start gap-2">
            <AlertCircle className="h-4 w-4 shrink-0 text-rose-400 mt-0.5" />
            <div className="flex-1">
              <span>{error}</span>
            </div>
          </div>
        )}

        {result && !result.rolled_back && (
          <div className="p-3 rounded-xl bg-emerald-500/10 border border-emerald-500/20 text-emerald-300 text-xs flex items-start gap-2">
            <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400 mt-0.5" />
            <div className="flex-1 space-y-1">
              <div className="font-semibold">Image updated successfully to {result.target_tag}!</div>
              {result.recreated_containers && result.recreated_containers.length > 0 && (
                <div className="text-[11px] text-emerald-400">
                  Recreated and verified containers: {result.recreated_containers.join(", ")}
                </div>
              )}
            </div>
          </div>
        )}

        {isLoading ? (
          <div className="h-44 flex flex-col items-center justify-center rounded-xl border border-slate-800 bg-slate-950 text-slate-400 space-y-2">
            <RefreshCw className="h-6 w-6 animate-spin text-sky-400" />
            <span className="text-xs">Fetching tags and security advisories from Docker Hub...</span>
          </div>
        ) : updateInfo ? (
          <div className="space-y-4">
            {/* Version Selection Cards */}
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              {/* Option A: Retain Compatibility */}
              <div
                onClick={() => updateInfo.retain_has_update && setSelectedMode("retain")}
                className={`p-3.5 rounded-xl border transition cursor-pointer flex flex-col justify-between ${
                  selectedMode === "retain"
                    ? "bg-sky-500/10 border-sky-500/50 ring-1 ring-sky-500/50"
                    : updateInfo.retain_has_update
                    ? "bg-slate-950/60 border-slate-800 hover:border-slate-700"
                    : "bg-slate-950/30 border-slate-900 opacity-50 cursor-not-allowed"
                }`}
              >
                <div>
                  <div className="flex items-center justify-between gap-1 mb-1.5">
                    <span className="text-xs font-semibold text-slate-200 flex items-center gap-1.5">
                      <ShieldCheck className="w-4 h-4 text-emerald-400" />
                      Retain Compatibility
                    </span>
                    {updateInfo.retain_has_update && (
                      <span className="text-[10px] font-bold px-1.5 py-0.5 rounded bg-emerald-500/20 text-emerald-300">
                        Patch / CVE Fix
                      </span>
                    )}
                  </div>
                  <p className="text-[11px] text-slate-400">
                    Keep the current major/minor line and apply security patches with zero breaking changes.
                  </p>
                </div>

                <div className="mt-3 pt-2 border-t border-slate-800/80 flex items-center justify-between text-xs font-mono">
                  <span className="text-slate-400 text-[11px]">Target Tag:</span>
                  <span className={`font-semibold ${updateInfo.retain_has_update ? "text-emerald-400" : "text-slate-500"}`}>
                    {updateInfo.retain_tag || "Up to date"}
                  </span>
                </div>
              </div>

              {/* Option B: Full Upgrade */}
              <div
                onClick={() => updateInfo.full_has_update && setSelectedMode("full")}
                className={`p-3.5 rounded-xl border transition cursor-pointer flex flex-col justify-between ${
                  selectedMode === "full"
                    ? "bg-purple-500/10 border-purple-500/50 ring-1 ring-purple-500/50"
                    : updateInfo.full_has_update
                    ? "bg-slate-950/60 border-slate-800 hover:border-slate-700"
                    : "bg-slate-950/30 border-slate-900 opacity-50 cursor-not-allowed"
                }`}
              >
                <div>
                  <div className="flex items-center justify-between gap-1 mb-1.5">
                    <span className="text-xs font-semibold text-slate-200 flex items-center gap-1.5">
                      <ArrowUpCircle className="w-4 h-4 text-purple-400" />
                      Full Upgrade
                    </span>
                    {updateInfo.full_has_update && (
                      <span className="text-[10px] font-bold px-1.5 py-0.5 rounded bg-purple-500/20 text-purple-300">
                        Latest Release
                      </span>
                    )}
                  </div>
                  <p className="text-[11px] text-slate-400">
                    Upgrade to the newest release on Docker Hub matching this distribution variant.
                  </p>
                </div>

                <div className="mt-3 pt-2 border-t border-slate-800/80 flex items-center justify-between text-xs font-mono">
                  <span className="text-slate-400 text-[11px]">Target Tag:</span>
                  <span className={`font-semibold ${updateInfo.full_has_update ? "text-purple-400" : "text-slate-500"}`}>
                    {updateInfo.full_tag || "Up to date"}
                  </span>
                </div>
              </div>
            </div>

            {/* Options */}
            <div className="p-3 bg-slate-950/60 rounded-xl border border-slate-800 space-y-2.5">
              <label className="flex items-center gap-2.5 text-xs text-slate-300 cursor-pointer">
                <input
                  type="checkbox"
                  checked={recreateContainers}
                  onChange={(e) => setRecreateContainers(e.target.checked)}
                  disabled={isUpdating}
                  className="rounded border-slate-750 bg-slate-900 text-sky-500 focus:ring-sky-500"
                />
                <div className="flex flex-col">
                  <span className="font-medium text-slate-200">Automatically recreate running containers</span>
                  <span className="text-[11px] text-slate-400">
                    Preserves ports, networks, and environment variables. Rolls back automatically if the new container fails.
                  </span>
                </div>
              </label>

              <label className="flex items-center gap-2.5 text-xs text-slate-300 cursor-pointer">
                <input
                  type="checkbox"
                  checked={removeOldImage}
                  onChange={(e) => setRemoveOldImage(e.target.checked)}
                  disabled={isUpdating}
                  className="rounded border-slate-750 bg-slate-900 text-sky-500 focus:ring-sky-500"
                />
                <div className="flex flex-col">
                  <span className="font-medium text-slate-200">Clean up old image after recreation</span>
                  <span className="text-[11px] text-slate-400">
                    Frees host storage by removing superseded image layers once the workload is healthy.
                  </span>
                </div>
              </label>
            </div>
          </div>
        ) : (
          <div className="text-center py-6 text-xs text-slate-400">
            No image update information available.
          </div>
        )}

        {/* Modal Actions */}
        <div className="flex items-center justify-end gap-2.5 pt-3 border-t border-slate-800">
          <Button variant="ghost" onClick={onClose} disabled={isUpdating}>
            Close
          </Button>
          <Button
            onClick={handleApplyUpdate}
            disabled={isUpdating || isLoading || (!updateInfo?.retain_has_update && !updateInfo?.full_has_update)}
            className="flex items-center gap-1.5"
          >
            {isUpdating ? (
              <>
                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                <span>Pulling & Recreating...</span>
              </>
            ) : (
              <>
                <ArrowUpCircle className="w-3.5 h-3.5" />
                <span>
                  Update to {selectedMode === "retain" ? updateInfo?.retain_tag : updateInfo?.full_tag}
                </span>
              </>
            )}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
