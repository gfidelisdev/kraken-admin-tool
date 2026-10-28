"use client";

import React, { useState } from "react";
import { Modal } from "@/components/ui/Modal";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import {
  Mail,
  AlertCircle,
  CheckCircle2,
  ShieldAlert,
  Smartphone,
  Lock,
  UserCheck,
  Send,
  KeyRound,
} from "lucide-react";
import { api, recoverPasswordOTP } from "@/lib/api";

interface RecoverPasswordModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function RecoverPasswordModal({ isOpen, onClose }: RecoverPasswordModalProps) {
  const [mode, setMode] = useState<"otp" | "admin">("otp");

  // Common / OTP fields
  const [usernameOrEmail, setUsernameOrEmail] = useState("");
  const [otpCode, setOtpCode] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");

  // Admin request fields
  const [note, setNote] = useState("");

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const resetForm = () => {
    setUsernameOrEmail("");
    setOtpCode("");
    setNewPassword("");
    setConfirmPassword("");
    setNote("");
    setError(null);
    setSuccess(null);
    setMode("otp");
  };

  const handleClose = () => {
    resetForm();
    onClose();
  };

  const handleOTPSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccess(null);

    const identifier = usernameOrEmail.trim();
    if (!identifier) {
      setError("Username or email is required.");
      return;
    }
    const cleanCode = otpCode.replace(/\D/g, "");
    if (cleanCode.length !== 6) {
      setError("Please enter the 6-digit code from your authenticator app.");
      return;
    }
    if (newPassword.length < 8) {
      setError("New password must be at least 8 characters long.");
      return;
    }
    if (newPassword !== confirmPassword) {
      setError("New password and confirmation do not match.");
      return;
    }

    setIsSubmitting(true);
    try {
      const res = await recoverPasswordOTP({
        username_or_email: identifier,
        code: cleanCode,
        new_password: newPassword,
      });

      setSuccess(
        res?.message ||
          "Password updated successfully! You can now log in using your new password."
      );
    } catch (err: any) {
      setError(
        err?.message ||
          "Failed to recover password. Please check your username and 6-digit code."
      );
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleAdminSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccess(null);

    const identifier = usernameOrEmail.trim();
    if (!identifier) {
      setError("Username or email is required.");
      return;
    }

    setIsSubmitting(true);
    try {
      const res = await api.post<{ message: string }>("/api/auth/recover-password", {
        username_or_email: identifier,
        note: note.trim(),
      });

      setSuccess(
        res?.message ||
          "Password recovery request submitted. An administrator will review your request and reset your credentials."
      );
    } catch (err: any) {
      setError(err?.message || "Failed to submit password recovery request.");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      title="Recover Account Password"
      description="Reset your password using an Authenticator app (TOTP) or submit an admin request."
      maxWidth="md"
    >
      <div className="space-y-4">
        {/* Mode Selector Tabs */}
        {!success && (
          <div className="grid grid-cols-2 gap-2 p-1 bg-slate-950/70 border border-slate-800 rounded-xl text-xs">
            <button
              type="button"
              onClick={() => {
                setError(null);
                setMode("otp");
              }}
              className={`flex items-center justify-center space-x-2 py-2 px-3 rounded-lg font-medium transition-colors ${
                mode === "otp"
                  ? "bg-sky-500/20 text-sky-300 border border-sky-500/30"
                  : "text-slate-400 hover:text-slate-200"
              }`}
            >
              <Smartphone className="h-4 w-4" />
              <span>Authenticator (Instant)</span>
            </button>
            <button
              type="button"
              onClick={() => {
                setError(null);
                setMode("admin");
              }}
              className={`flex items-center justify-center space-x-2 py-2 px-3 rounded-lg font-medium transition-colors ${
                mode === "admin"
                  ? "bg-sky-500/20 text-sky-300 border border-sky-500/30"
                  : "text-slate-400 hover:text-slate-200"
              }`}
            >
              <UserCheck className="h-4 w-4" />
              <span>Via Administrator</span>
            </button>
          </div>
        )}

        {error && (
          <div className="flex items-start space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
            <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
            <span>{error}</span>
          </div>
        )}

        {success ? (
          <div className="space-y-4">
            <div className="flex items-start space-x-2.5 rounded-xl border border-emerald-500/20 bg-emerald-500/10 p-4 text-xs text-emerald-300">
              <CheckCircle2 className="h-5 w-5 shrink-0 text-emerald-400 mt-0.5" />
              <div className="space-y-1">
                <p className="font-semibold text-emerald-200">
                  {mode === "otp" ? "Password Reset Successfully" : "Request Submitted"}
                </p>
                <p>{success}</p>
                {mode === "admin" && (
                  <p className="text-[11px] text-emerald-400/80 mt-2">
                    Please contact your system administrator to receive your temporary password.
                  </p>
                )}
              </div>
            </div>
            <div className="flex justify-end pt-2">
              <Button type="button" size="sm" onClick={handleClose}>
                Return to Login
              </Button>
            </div>
          </div>
        ) : mode === "otp" ? (
          <form onSubmit={handleOTPSubmit} className="space-y-3.5">
            <div className="rounded-xl border border-sky-500/20 bg-sky-500/5 p-3 text-xs text-sky-300 flex items-start space-x-2.5">
              <Smartphone className="h-4 w-4 shrink-0 text-sky-400 mt-0.5" />
              <span>
                If you have enrolled Google, Proton, Microsoft, or Authy Authenticator on your account,
                you can reset your password instantly with a 6-digit verification code.
              </span>
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300">Username or Email</label>
              <div className="mt-1">
                <Input
                  placeholder="e.g. operator_john or john@kraken.local"
                  value={usernameOrEmail}
                  onChange={(e) => setUsernameOrEmail(e.target.value)}
                  icon={<Mail className="h-4 w-4" />}
                  autoFocus
                  required
                />
              </div>
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300">6-Digit Authenticator Code</label>
              <div className="mt-1">
                <Input
                  type="text"
                  inputMode="numeric"
                  placeholder="000000"
                  maxLength={6}
                  value={otpCode}
                  onChange={(e) => setOtpCode(e.target.value.replace(/\D/g, ""))}
                  icon={<KeyRound className="h-4 w-4" />}
                  className="font-mono text-center tracking-widest text-base"
                  required
                />
              </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <div>
                <label className="text-xs font-medium text-slate-300">New Password</label>
                <div className="mt-1">
                  <Input
                    type="password"
                    placeholder="Min. 8 characters"
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    icon={<Lock className="h-4 w-4" />}
                    required
                  />
                </div>
              </div>
              <div>
                <label className="text-xs font-medium text-slate-300">Confirm Password</label>
                <div className="mt-1">
                  <Input
                    type="password"
                    placeholder="Repeat password"
                    value={confirmPassword}
                    onChange={(e) => setConfirmPassword(e.target.value)}
                    icon={<Lock className="h-4 w-4" />}
                    required
                  />
                </div>
              </div>
            </div>

            <div className="flex justify-between items-center pt-2 border-t border-slate-800">
              <span className="text-[11px] text-slate-500">
                No authenticator set up? Switch to{" "}
                <button
                  type="button"
                  onClick={() => setMode("admin")}
                  className="text-sky-400 hover:underline"
                >
                  Via Administrator
                </button>
              </span>
              <div className="flex space-x-2">
                <Button type="button" variant="outline" size="sm" onClick={handleClose}>
                  Cancel
                </Button>
                <Button type="submit" size="sm" isLoading={isSubmitting}>
                  Reset Password
                </Button>
              </div>
            </div>
          </form>
        ) : (
          <form onSubmit={handleAdminSubmit} className="space-y-4">
            <div className="rounded-xl border border-sky-500/20 bg-sky-500/5 p-3 text-xs text-sky-300 flex items-start space-x-2.5">
              <ShieldAlert className="h-4 w-4 shrink-0 text-sky-400 mt-0.5" />
              <span>
                Kraken enforces strict administrator-governed access. Submitting this form notifies
                administrators, who can reset your password or issue a temporary credential.
              </span>
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300">Username or Email</label>
              <div className="mt-1">
                <Input
                  placeholder="e.g. operator_john or john@kraken.local"
                  value={usernameOrEmail}
                  onChange={(e) => setUsernameOrEmail(e.target.value)}
                  icon={<Mail className="h-4 w-4" />}
                  autoFocus
                  required
                />
              </div>
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300">
                Reason / Note for Administrator <span className="text-slate-500">(Optional)</span>
              </label>
              <div className="mt-1">
                <textarea
                  rows={2}
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  placeholder="e.g. Lost device, forgotten password after vacation"
                  className="w-full rounded-xl border border-slate-800 bg-slate-950 px-3 py-2 text-xs text-slate-200 placeholder-slate-500 focus:border-sky-500 focus:outline-none focus:ring-1 focus:ring-sky-500 transition-colors"
                />
              </div>
            </div>

            <div className="flex justify-end space-x-2 pt-2 border-t border-slate-800">
              <Button type="button" variant="outline" size="sm" onClick={handleClose}>
                Cancel
              </Button>
              <Button type="submit" size="sm" isLoading={isSubmitting}>
                <Send className="mr-1.5 h-3.5 w-3.5" />
                Submit Request
              </Button>
            </div>
          </form>
        )}
      </div>
    </Modal>
  );
}
