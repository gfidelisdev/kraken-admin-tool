"use client";

import React, { useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { Lock, Sparkles, Copy, Check, AlertCircle, KeyRound, CheckCircle2 } from "lucide-react";
import { api } from "@/lib/api";
import { User } from "@/types";

interface ResetPasswordModalProps {
  isOpen: boolean;
  user: User | { id: number; username: string; email?: string } | null;
  onClose: () => void;
  onPasswordReset?: () => void;
}

export function ResetPasswordModal({
  isOpen,
  user,
  onClose,
  onPasswordReset,
}: ResetPasswordModalProps) {
  const [newPassword, setNewPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [generatedPassword, setGeneratedPassword] = useState<string | null>(null);
  const [hasCopied, setHasCopied] = useState(false);

  if (!user) return null;

  const handleGenerateRandom = () => {
    const chars = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$%&*+";
    let pass = "";
    for (let i = 0; i < 14; i++) {
      pass += chars.charAt(Math.floor(Math.random() * chars.length));
    }
    setNewPassword(pass);
  };

  const handleCopy = () => {
    if (generatedPassword) {
      navigator.clipboard.writeText(generatedPassword);
      setHasCopied(true);
      setTimeout(() => setHasCopied(false), 2000);
    }
  };

  const handleClose = () => {
    setNewPassword("");
    setError(null);
    setGeneratedPassword(null);
    setHasCopied(false);
    onClose();
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (newPassword && newPassword.length < 8) {
      setError("Password must be at least 8 characters long");
      return;
    }

    setIsSubmitting(true);
    try {
      const res = await api.post<{
        message: string;
        new_password?: string;
        username?: string;
      }>(`/api/auth/users/${user.id}/reset-password`, {
        new_password: newPassword,
      });

      const finalPass = res.new_password || newPassword;
      setGeneratedPassword(finalPass);
      if (onPasswordReset) {
        onPasswordReset();
      }
    } catch (err: any) {
      setError(err?.message || "Failed to reset password");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      title="Reset User Password"
      description={`Set or generate a new password for ${user.username}`}
      maxWidth="md"
    >
      {generatedPassword ? (
        <div className="space-y-4">
          <div className="flex items-start space-x-2.5 rounded-xl border border-emerald-500/20 bg-emerald-500/10 p-4 text-xs text-emerald-300">
            <CheckCircle2 className="h-5 w-5 shrink-0 text-emerald-400 mt-0.5" />
            <div className="space-y-1">
              <p className="font-semibold text-emerald-200">Password Updated Successfully</p>
              <p className="text-slate-300">
                User <strong>{user.username}</strong> can now log in using the credential below.
              </p>
            </div>
          </div>

          <div className="space-y-1.5">
            <label className="text-xs font-medium text-slate-300">New Temporary Password</label>
            <div className="flex items-center space-x-2 rounded-xl border border-slate-700 bg-slate-950 px-3 py-2.5">
              <KeyRound className="h-4 w-4 text-sky-400 shrink-0" />
              <span className="flex-1 font-mono text-sm font-semibold tracking-wider text-sky-300 select-all">
                {generatedPassword}
              </span>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={handleCopy}
                className="h-7 px-2.5 text-xs text-slate-300 hover:text-white"
              >
                {hasCopied ? (
                  <>
                    <Check className="h-3.5 w-3.5 mr-1 text-emerald-400" />
                    Copied
                  </>
                ) : (
                  <>
                    <Copy className="h-3.5 w-3.5 mr-1" />
                    Copy
                  </>
                )}
              </Button>
            </div>
            <p className="text-[11px] text-amber-400/90">
              Please copy this password and share it securely with the user. It will not be shown again.
            </p>
          </div>

          <div className="flex justify-end pt-3 border-t border-slate-800">
            <Button type="button" size="sm" onClick={handleClose}>
              Done
            </Button>
          </div>
        </div>
      ) : (
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
              {user.email && <p className="text-[11px] text-slate-400">{user.email}</p>}
            </div>
            <span className="text-[10px] font-mono text-slate-500 uppercase">Target User</span>
          </div>

          <div>
            <div className="flex items-center justify-between">
              <label className="text-xs font-medium text-slate-300">New Password</label>
              <button
                type="button"
                onClick={handleGenerateRandom}
                className="text-[11px] text-sky-400 hover:text-sky-300 hover:underline inline-flex items-center space-x-1 font-medium"
              >
                <Sparkles className="h-3 w-3" />
                <span>Generate Strong Password</span>
              </button>
            </div>
            <div className="mt-1">
              <Input
                type="text"
                placeholder="Leave blank to auto-generate, or type new password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                icon={<Lock className="h-4 w-4" />}
                autoFocus
              />
            </div>
            <p className="mt-1.5 text-[11px] text-slate-400">
              Leave blank to automatically generate a secure 12-character random password.
            </p>
          </div>

          <div className="flex justify-end space-x-2 pt-3 border-t border-slate-800">
            <Button type="button" variant="outline" size="sm" onClick={handleClose}>
              Cancel
            </Button>
            <Button type="submit" size="sm" isLoading={isSubmitting}>
              Reset Password
            </Button>
          </div>
        </form>
      )}
    </Modal>
  );
}
