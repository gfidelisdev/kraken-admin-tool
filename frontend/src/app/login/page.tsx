"use client";

import React, { useState, useEffect } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  Anchor,
  Lock,
  User,
  AlertCircle,
  ArrowRight,
  Sparkles,
  KeyRound,
  Clock,
  ShieldAlert,
} from "lucide-react";
import { useAuth } from "@/context/AuthContext";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { RecoverPasswordModal } from "@/components/auth/RecoverPasswordModal";
import Image from "next/image";

export default function LoginPage() {
  const router = useRouter();
  const { login, isAuthenticated, isLoading: authLoading } = useAuth();

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isPendingApproval, setIsPendingApproval] = useState(false);
  const [showRecoverModal, setShowRecoverModal] = useState(false);

  useEffect(() => {
    if (isAuthenticated && !authLoading) {
      router.push("/");
    }
  }, [isAuthenticated, authLoading, router]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!username || !password) {
      setError("Please fill in both fields");
      return;
    }

    setIsSubmitting(true);
    setError(null);
    setIsPendingApproval(false);

    try {
      await login(username, password);
      router.push("/");
    } catch (err: any) {
      const msg = err?.message || "Invalid credentials. Please check your username and password.";
      const status = err?.status;
      if (
        status === 403 &&
        (msg.toLowerCase().includes("pending") || msg.toLowerCase().includes("approval"))
      ) {
        setIsPendingApproval(true);
        setError(msg);
      } else {
        setError(msg);
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleUseDefaultCredentials = () => {
    setUsername("admin");
    setPassword("admin123");
    setError(null);
    setIsPendingApproval(false);
  };

  return (
    <div className="flex min-h-screen flex-col items-center justify-center p-4 relative overflow-hidden bg-[#090d16]">
      {/* Background glowing spheres */}
      <div className="absolute top-1/4 -left-32 h-96 w-96 rounded-full bg-sky-500/10 blur-3xl pointer-events-none" />
      <div className="absolute bottom-1/4 -right-32 h-96 w-96 rounded-full bg-indigo-500/10 blur-3xl pointer-events-none" />

      <div className="w-full max-w-md space-y-6 z-10">
        {/* Brand header */}
        <div className="text-center space-y-2">
          <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-tr from-sky-500 to-indigo-600 shadow-xl shadow-sky-500/25">
            {/*<Anchor className="h-8 w-8 text-white" />*/}
            <Image src="/kraken.png" alt="" width={50} height={50} />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Kraken Admin Tool</h1>
          <p className="text-xs text-slate-400">
            Sign in to manage your Docker containers and Kubernetes clusters
          </p>
        </div>

        {/* Card */}
        <div className="rounded-2xl border border-slate-800 bg-slate-900/80 p-8 shadow-2xl backdrop-blur-md space-y-5">
          {/* Quick Demo Credential Pill (Development Only) */}
          {process.env.NODE_ENV !== "production" && (
            <div className="flex items-center justify-between rounded-xl border border-sky-500/20 bg-sky-500/5 p-3 text-xs text-sky-300">
              <div className="flex items-center space-x-2">
                <Sparkles className="h-4 w-4 text-sky-400 shrink-0" />
                <span>
                  Development Default: <strong>admin</strong> / <strong>admin123</strong>
                </span>
              </div>
              <button
                type="button"
                onClick={handleUseDefaultCredentials}
                className="text-[11px] underline underline-offset-2 hover:text-white font-medium"
              >
                Fill In
              </button>
            </div>
          )}

          {/* Pending Approval Notice */}
          {isPendingApproval ? (
            <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 p-3.5 text-xs text-amber-300 space-y-1.5">
              <div className="flex items-center space-x-2 font-semibold text-amber-200">
                <Clock className="h-4 w-4 shrink-0 text-amber-400" />
                <span>Account Pending Administrator Approval</span>
              </div>
              <p className="text-slate-300 text-[11px] leading-relaxed">
                Your account was created successfully, but requires an administrator to approve it
                and assign your role (Operator or Viewer) before you can sign in.
              </p>
              <p className="text-[10px] text-amber-400/80 font-mono">
                Please contact your Kraken administrator to activate your account.
              </p>
            </div>
          ) : error ? (
            <div className="flex items-center space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
              <AlertCircle className="h-4 w-4 shrink-0" />
              <span>{error}</span>
            </div>
          ) : null}

          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="text-xs font-medium text-slate-300">Username</label>
              <div className="mt-1">
                <Input
                  placeholder="Enter username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  icon={<User className="h-4 w-4" />}
                  autoFocus
                />
              </div>
            </div>

            <div>
              <div className="flex items-center justify-between">
                <label className="text-xs font-medium text-slate-300">Password</label>
                <button
                  type="button"
                  onClick={() => setShowRecoverModal(true)}
                  className="text-[11px] text-sky-400 hover:text-sky-300 hover:underline inline-flex items-center space-x-1"
                >
                  <KeyRound className="h-3 w-3" />
                  <span>Forgot password?</span>
                </button>
              </div>
              <div className="mt-1">
                <Input
                  type="password"
                  placeholder="Enter password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  icon={<Lock className="h-4 w-4" />}
                />
              </div>
            </div>

            <Button
              type="submit"
              className="w-full h-10 mt-2 font-semibold"
              isLoading={isSubmitting}
            >
              Sign In
              <ArrowRight className="ml-2 h-4 w-4" />
            </Button>
          </form>

          <div className="flex items-center justify-between pt-2 border-t border-slate-800/80 text-xs text-slate-400">
            <span>Don&apos;t have an account?</span>
            <Link href="/register" className="text-sky-400 hover:underline font-medium">
              Create account &rarr;
            </Link>
          </div>
        </div>
      </div>

      <RecoverPasswordModal
        isOpen={showRecoverModal}
        onClose={() => setShowRecoverModal(false)}
      />
    </div>
  );
}
