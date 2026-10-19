"use client";

import React, { useState } from "react";
import { AlertTriangle } from "lucide-react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";

interface ConfirmActionModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  description: string;
  confirmLabel?: string;
  confirmVariant?: "destructive" | "default" | "docker" | "k8s" | "warning";
  onConfirm: () => Promise<void>;
}

export function ConfirmActionModal({
  isOpen,
  onClose,
  title,
  description,
  confirmLabel = "Confirm Action",
  confirmVariant = "destructive",
  onConfirm,
}: ConfirmActionModalProps) {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleConfirm = async () => {
    setIsLoading(true);
    setError(null);
    try {
      await onConfirm();
      onClose();
    } catch (err: any) {
      setError(err?.message || "Action failed");
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={title}>
      <div className="space-y-4">
        <div className="flex items-start space-x-3 rounded-xl border border-amber-500/20 bg-amber-500/10 p-3.5 text-amber-300">
          <AlertTriangle className="h-5 w-5 shrink-0 mt-0.5 text-amber-400" />
          <p className="text-xs leading-relaxed">{description}</p>
        </div>

        {error && (
          <div className="rounded-lg border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
            {error}
          </div>
        )}

        <div className="flex items-center justify-end space-x-2 pt-3 border-t border-slate-800">
          <Button type="button" variant="outline" onClick={onClose} disabled={isLoading}>
            Cancel
          </Button>
          <Button
            type="button"
            variant={confirmVariant}
            isLoading={isLoading}
            onClick={handleConfirm}
          >
            {confirmLabel}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
