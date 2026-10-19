"use client";

import React, { useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { Select } from "@/components/ui/Select";
import { Button } from "@/components/ui/Button";
import { User, Mail, Lock, AlertCircle, CheckCircle2 } from "lucide-react";
import { api } from "@/lib/api";

interface AddUserModalProps {
  isOpen: boolean;
  onClose: () => void;
  onUserAdded?: () => void;
}

export function AddUserModal({ isOpen, onClose, onUserAdded }: AddUserModalProps) {
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("operator");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const resetForm = () => {
    setUsername("");
    setEmail("");
    setPassword("");
    setRole("operator");
    setError(null);
    setSuccess(null);
  };

  const handleClose = () => {
    resetForm();
    onClose();
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccess(null);

    if (!username || !email || !password) {
      setError("Username, email, and password are required");
      return;
    }

    if (password.length < 8) {
      setError("Password must be at least 8 characters long");
      return;
    }

    setIsSubmitting(true);
    try {
      await api.post("/api/auth/register", {
        username,
        email,
        password,
        role,
      });

      setSuccess(`User "${username}" created successfully with role "${role}"!`);
      if (onUserAdded) {
        onUserAdded();
      }
      setTimeout(() => {
        handleClose();
      }, 1500);
    } catch (err: any) {
      setError(err?.message || "Failed to create user");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      title="Create New User"
      description="Register a new user account with specified system permissions."
      maxWidth="md"
    >
      <form onSubmit={handleSubmit} className="space-y-4">
        {error && (
          <div className="flex items-center space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
            <AlertCircle className="h-4 w-4 shrink-0" />
            <span>{error}</span>
          </div>
        )}

        {success && (
          <div className="flex items-center space-x-2 rounded-xl border border-emerald-500/20 bg-emerald-500/10 p-3 text-xs text-emerald-400">
            <CheckCircle2 className="h-4 w-4 shrink-0" />
            <span>{success}</span>
          </div>
        )}

        <div>
          <label className="text-xs font-medium text-slate-300">Username</label>
          <div className="mt-1">
            <Input
              placeholder="e.g. jdoe_ops"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              icon={<User className="h-4 w-4" />}
              autoFocus
              required
            />
          </div>
        </div>

        <div>
          <label className="text-xs font-medium text-slate-300">Email Address</label>
          <div className="mt-1">
            <Input
              type="email"
              placeholder="e.g. jdoe@kraken.local"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              icon={<Mail className="h-4 w-4" />}
              required
            />
          </div>
        </div>

        <div>
          <label className="text-xs font-medium text-slate-300">Temporary Password</label>
          <div className="mt-1">
            <Input
              type="password"
              placeholder="Minimum 8 characters"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              icon={<Lock className="h-4 w-4" />}
              required
            />
          </div>
        </div>

        <div>
          <Select
            label="Role & Privileges"
            value={role}
            onChange={(e) => setRole(e.target.value)}
            options={[
              { label: "Operator (Manage containers & workloads)", value: "operator" },
              { label: "Viewer (Read-only status & metrics)", value: "viewer" },
              { label: "Admin (Full infrastructure & user management)", value: "admin" },
            ]}
          />
        </div>

        <div className="flex justify-end space-x-2 pt-2 border-t border-slate-800">
          <Button type="button" variant="outline" size="sm" onClick={handleClose}>
            Cancel
          </Button>
          <Button type="submit" size="sm" isLoading={isSubmitting}>
            Create User
          </Button>
        </div>
      </form>
    </Modal>
  );
}
