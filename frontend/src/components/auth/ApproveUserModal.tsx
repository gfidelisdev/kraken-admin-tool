"use client";

import React, { useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Select } from "@/components/ui/Select";
import { Button } from "@/components/ui/Button";
import { ShieldCheck, UserCheck, AlertCircle } from "lucide-react";
import { api } from "@/lib/api";
import { User } from "@/types";

interface ApproveUserModalProps {
  isOpen: boolean;
  user: User | null;
  onClose: () => void;
  onUserApproved: () => void;
}

export function ApproveUserModal({
  isOpen,
  user,
  onClose,
  onUserApproved,
}: ApproveUserModalProps) {
  const [role, setRole] = useState("operator");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (!user) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setIsSubmitting(true);

    try {
      await api.post(`/api/auth/users/${user.id}/approve`, { role });
      onUserApproved();
      onClose();
    } catch (err: any) {
      setError(err?.message || "Failed to approve user");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Approve User Account"
      description={`Activate account and assign access role for ${user.username}`}
      maxWidth="md"
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        {error && (
          <div className="flex items-center space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
            <AlertCircle className="h-4 w-4 shrink-0" />
            <span>{error}</span>
          </div>
        )}

        {/* User Summary Pill */}
        <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3 space-y-2">
          <div className="flex items-center space-x-3">
            <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-sky-500/20 text-sky-400 font-bold text-sm border border-sky-500/30">
              {user.username.charAt(0).toUpperCase()}
            </div>
            <div>
              <p className="text-xs font-semibold text-white">{user.username}</p>
              <p className="text-[11px] text-slate-400">{user.email}</p>
            </div>
          </div>
          <div className="text-[10px] text-slate-500 font-mono">
            Requested: {user.created_at ? new Date(user.created_at).toLocaleString() : "Recently"}
          </div>
        </div>

        <div>
          <Select
            label="Attribute Role & Access Level"
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
          <p className="mt-1.5 text-[11px] text-slate-400">
            The user will immediately be granted credentials to sign in with this role.
          </p>
        </div>

        <div className="flex justify-end space-x-2 pt-3 border-t border-slate-800">
          <Button type="button" variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            size="sm"
            isLoading={isSubmitting}
            className="bg-emerald-600 hover:bg-emerald-500 text-white"
          >
            <UserCheck className="mr-1.5 h-4 w-4" />
            Approve & Attribute Role
          </Button>
        </div>
      </form>
    </Modal>
  );
}
