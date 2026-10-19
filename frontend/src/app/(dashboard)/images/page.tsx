"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import {
  Disc,
  Download,
  Trash2,
  Search,
  RefreshCw,
  AlertCircle,
  Tag,
  HardDrive,
} from "lucide-react";
import { api } from "@/lib/api";
import { DockerImage } from "@/types";
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
import { PullImageModal } from "@/components/modals/PullImageModal";
import { ConfirmActionModal } from "@/components/modals/ConfirmActionModal";
import { useServer } from "@/context/ServerContext";
import { formatBytes, formatDate } from "@/lib/utils";

export default function ImagesPage() {
  const { activeServer } = useServer();
  const [images, setImages] = useState<DockerImage[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [search, setSearch] = useState("");
  const [error, setError] = useState<string | null>(null);

  const [pullModalOpen, setPullModalOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<DockerImage | null>(null);

  const fetchImages = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const data = await api.get<DockerImage[]>("/api/docker/images");
      setImages(Array.isArray(data) ? data : []);
    } catch (err: any) {
      setError(err?.message || "Failed to load Docker images");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchImages();
  }, [fetchImages, activeServer?.id]);

  const handlePullImage = async (imageName: string) => {
    await api.post("/api/docker/images/pull", { image: imageName });
    await fetchImages();
  };

  const handleDeleteImage = async () => {
    if (!deleteTarget) return;
    try {
      await api.delete(`/api/docker/images/${deleteTarget.id}?force=true`);
      setDeleteTarget(null);
      await fetchImages();
    } catch (err: any) {
      setError(err?.message || "Failed to delete image");
    }
  };

  const filteredImages = useMemo(() => {
    if (!search.trim()) return images;
    const query = search.toLowerCase();
    return images.filter((img) => {
      const tagMatch = img.repo_tags?.some((t) => t.toLowerCase().includes(query));
      const idMatch = img.id.toLowerCase().includes(query);
      return tagMatch || idMatch;
    });
  }, [images, search]);

  const totalBytes = useMemo(() => {
    return images.reduce((acc, curr) => acc + (curr.size || 0), 0);
  }, [images]);

  return (
    <div className="space-y-6 animate-in fade-in duration-300">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <div className="flex items-center space-x-2">
            <Disc className="h-6 w-6 text-sky-400" />
            <h1 className="text-2xl font-bold tracking-tight text-white">Docker Images</h1>
          </div>
          <p className="text-xs text-slate-400 mt-1">
            Total {images.length} images stored locally ({formatBytes(totalBytes)} disk usage).
          </p>
        </div>

        <div className="flex items-center space-x-2">
          <Button
            variant="outline"
            size="sm"
            onClick={fetchImages}
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

      {error && (
        <div className="flex items-center space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3.5 text-xs text-rose-400">
          <AlertCircle className="h-4 w-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* Search Input */}
      <div className="w-full sm:w-80">
        <Input
          placeholder="Filter by repository tag or ID..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          icon={<Search className="h-4 w-4 text-slate-500" />}
        />
      </div>

      {/* Images Table */}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Repository & Tag</TableHead>
            <TableHead>Image ID</TableHead>
            <TableHead>Virtual Size</TableHead>
            <TableHead>Created</TableHead>
            <TableHead className="text-right">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {filteredImages.length === 0 ? (
            <TableEmpty
              colSpan={5}
              icon={<Disc className="h-8 w-8" />}
              message={
                images.length === 0
                  ? "No images found in local Docker cache."
                  : "No images match your search query."
              }
            />
          ) : (
            filteredImages.map((image) => {
              const tags = image.repo_tags && image.repo_tags.length > 0
                ? image.repo_tags
                : ["<none>:<none>"];
              const shortId = image.id.replace(/^sha256:/, "").slice(0, 12);

              return (
                <TableRow key={image.id}>
                  {/* Repo Tags */}
                  <TableCell>
                    <div className="flex flex-wrap gap-1.5 items-center">
                      <Tag className="h-3.5 w-3.5 text-sky-400 shrink-0" />
                      {tags.map((t, idx) => (
                        <span
                          key={idx}
                          className="font-mono text-xs text-white bg-slate-800 px-2 py-0.5 rounded border border-slate-700/60"
                        >
                          {t}
                        </span>
                      ))}
                    </div>
                  </TableCell>

                  {/* ID */}
                  <TableCell className="font-mono text-xs text-slate-400">
                    {shortId}
                  </TableCell>

                  {/* Size */}
                  <TableCell className="text-xs font-mono text-slate-300">
                    <span className="flex items-center gap-1.5">
                      <HardDrive className="h-3.5 w-3.5 text-slate-500" />
                      {formatBytes(image.size)}
                    </span>
                  </TableCell>

                  {/* Created */}
                  <TableCell className="text-xs text-slate-400">
                    {formatDate(image.created)}
                  </TableCell>

                  {/* Actions */}
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 text-rose-400 hover:text-rose-300"
                      title="Delete image"
                      onClick={() => setDeleteTarget(image)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </TableCell>
                </TableRow>
              );
            })
          )}
        </TableBody>
      </Table>

      {/* Pull Image Modal */}
      <PullImageModal
        isOpen={pullModalOpen}
        onClose={() => setPullModalOpen(false)}
        onSuccess={fetchImages}
      />

      {/* Delete Confirmation Modal */}
      <ConfirmActionModal
        isOpen={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        title="Delete Image"
        description={`Are you sure you want to remove image "${
          deleteTarget?.repo_tags?.[0] || deleteTarget?.id.slice(0, 12)
        }"? Any running container using this image may prevent removal unless forced.`}
        confirmLabel="Remove Image"
        confirmVariant="destructive"
        onConfirm={handleDeleteImage}
      />
    </div>
  );
}
