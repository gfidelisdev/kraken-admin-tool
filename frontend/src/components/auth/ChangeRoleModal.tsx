"use client";

import React, { useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Select } from "@/components/ui/Select";
import { Button } from "@/components/ui/Button";
import { Shield, AlertCircle } from "lucide-react";
import { api } from "@/lib/api";
import { User } from "@/types";

interface ChangeRoleModalProps {
  isOpen: boolean;
  user: User | null;
  onClose: () => void;
  onRoleUpdated: () => void;
}

export function ChangeRoleModal({
  isOpen,
  user,
  onClose,
  onRoleUpdated,
}: ChangeRoleModalProps) {
  const [role, setRole] = useState(user?.role || "operator");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Sync role when user changes
  React.useEffect(() => {
    if (user) {
      setRole(user.role || "operator");
    }
  }, [user]);

  if (!user) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setIsSubmitting(true);

    try {
      await api.put(`/api/auth/users/${user.id}/role`, { role });
      onRoleUpdated();
      onClose();
    } catch (err: any) {
      setError(err?.message || "Failed to update role");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Modify User Role"
      description={`Update access privileges for ${user.username}`}
      maxWidth="md"
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        {error && (
          <div className="flex items-center space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
            <AlertCircle className="h-4 w-4 shrink-0" />
            <span>{error}</span>
          </div>
        )}

        <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3 flex items-center justify-between">
          <div>
            <p className="text-xs font-semibold text-white">{user.username}</p>
            <p className="text-[11px] text-slate-400">{user.email}</p>
          </div>
          <span className="text-[10px] font-mono text-slate-400">Current: {user.role}</span>
        </div>

        <div>
          <Select
            label="Assigned System Role"
            value={role}
            onChange={(e) => setRole(e.target.value)}
            options={[
              {
                label: "Operator (Manage containers & workloads)",
                value: "operator",
              },
              {
                label: "Viewer (Read-only status & metrics)",
                value: "viewer",
              },
              {
                label: "Admin (Full infrastructure & user management)",
                value: "admin",
              },
            ]}
          />
        </div>

        <div className="flex justify-end space-x-2 pt-3 border-t border-slate-800">
          <Button type="button" variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" size="sm" isLoading={isSubmitting}>
            <Shield className="mr-1.5 h-4 w-4" />
            Update Role
          </Button>
        </div>
      </form>
    </Modal>
  );
}
