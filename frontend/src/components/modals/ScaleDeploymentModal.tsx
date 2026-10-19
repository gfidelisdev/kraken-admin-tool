"use client";

import React, { useState, useEffect } from "react";
import { Layers, Minus, Plus, AlertCircle } from "lucide-react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { K8sDeployment } from "@/types";

interface ScaleDeploymentModalProps {
  isOpen: boolean;
  onClose: () => void;
  deployment: K8sDeployment | null;
  onScale: (namespace: string, name: string, replicas: number) => Promise<void>;
}

export function ScaleDeploymentModal({
  isOpen,
  onClose,
  deployment,
  onScale,
}: ScaleDeploymentModalProps) {
  const [replicas, setReplicas] = useState(1);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (deployment) {
      setReplicas(deployment.desired_replicas || deployment.ready_replicas || 1);
    }
  }, [deployment]);

  if (!deployment) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsLoading(true);
    setError(null);

    try {
      await onScale(deployment.namespace, deployment.name, replicas);
      onClose();
    } catch (err: any) {
      setError(err?.message || "Failed to scale deployment");
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={`Scale Deployment: ${deployment.name}`}
      description={`Adjust the desired number of pod replicas for ${deployment.namespace}/${deployment.name}.`}
    >
      <form onSubmit={handleSubmit} className="space-y-5">
        <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-4 space-y-2">
          <div className="flex justify-between text-xs text-slate-400">
            <span>Namespace:</span>
            <span className="font-mono text-slate-200">{deployment.namespace}</span>
          </div>
          <div className="flex justify-between text-xs text-slate-400">
            <span>Current Available / Desired:</span>
            <span className="font-mono text-sky-400 font-semibold">
              {deployment.available_replicas} / {deployment.desired_replicas}
            </span>
          </div>
        </div>

        {/* Stepper Controls */}
        <div className="flex flex-col items-center justify-center space-y-3 py-2">
          <label className="text-xs font-medium text-slate-300">Target Replicas Count</label>
          <div className="flex items-center space-x-4">
            <Button
              type="button"
              variant="outline"
              size="icon"
              className="h-10 w-10 rounded-xl"
              onClick={() => setReplicas((r) => Math.max(0, r - 1))}
              disabled={replicas <= 0 || isLoading}
            >
              <Minus className="h-4 w-4" />
            </Button>

            <span className="w-16 text-center text-3xl font-bold font-mono text-white">
              {replicas}
            </span>

            <Button
              type="button"
              variant="outline"
              size="icon"
              className="h-10 w-10 rounded-xl"
              onClick={() => setReplicas((r) => r + 1)}
              disabled={isLoading}
            >
              <Plus className="h-4 w-4" />
            </Button>
          </div>
        </div>

        {error && (
          <div className="flex items-center space-x-2 rounded-lg border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
            <AlertCircle className="h-4 w-4 shrink-0" />
            <span>{error}</span>
          </div>
        )}

        <div className="flex items-center justify-end space-x-2 pt-3 border-t border-slate-800">
          <Button type="button" variant="outline" onClick={onClose} disabled={isLoading}>
            Cancel
          </Button>
          <Button type="submit" variant="k8s" isLoading={isLoading}>
            <Layers className="mr-1.5 h-4 w-4" />
            Apply Scale
          </Button>
        </div>
      </form>
    </Modal>
  );
}
