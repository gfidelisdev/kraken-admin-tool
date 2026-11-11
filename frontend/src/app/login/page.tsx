"use client";

import React, { useState, useEffect, useRef } from "react";
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
  Copy,
  Check,
  ExternalLink,
} from "lucide-react";
import { useAuth } from "@/context/AuthContext";
import { api } from "@/lib/api";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { RecoverPasswordModal } from "@/components/auth/RecoverPasswordModal";
import { MicrosoftDeviceCodeResponse, MicrosoftPollResponse } from "@/types";
import Image from "next/image";

export default function LoginPage() {
  const router = useRouter();
  const { login, completeMicrosoftLogin, isAuthenticated, isLoading: authLoading } = useAuth();

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isPendingApproval, setIsPendingApproval] = useState(false);
  const [showRecoverModal, setShowRecoverModal] = useState(false);

  // Microsoft Device Code Login State
  const [isMSLoginActive, setIsMSLoginActive] = useState(false);
  const [msDeviceData, setMsDeviceData] = useState<MicrosoftDeviceCodeResponse | null>(null);
  const [isMSLoading, setIsMSLoading] = useState(false);
  const [hasCopiedCode, setHasCopiedCode] = useState(false);
  const [msPollStatus, setMsPollStatus] = useState<string>("");
  const msPollingIntervalRef = useRef<NodeJS.Timeout | null>(null);

  const stopMSPolling = () => {
    if (msPollingIntervalRef.current) {
      clearInterval(msPollingIntervalRef.current);
      msPollingIntervalRef.current = null;
    }
  };

  useEffect(() => {
    return () => stopMSPolling();
  }, []);

  useEffect(() => {
    if (isAuthenticated && !authLoading) {
      router.push("/");
    }
  }, [isAuthenticated, authLoading, router]);

  const handleStartMicrosoftLogin = async () => {
    setError(null);
    setIsPendingApproval(false);
    setIsMSLoading(true);
    stopMSPolling();

    try {
      const res = await api.post<MicrosoftDeviceCodeResponse>("/api/auth/microsoft/device-code", {});
      setMsDeviceData(res);
      setIsMSLoginActive(true);
      setMsPollStatus("Waiting for your authorization at Microsoft...");

      let currentIntervalMs = (res.interval || 5) * 1000;

      const schedulePoll = (intervalMs: number) => {
        stopMSPolling();
        msPollingIntervalRef.current = setInterval(async () => {
          try {
            const pollRes = await api.post<MicrosoftPollResponse>("/api/auth/microsoft/poll", {
              device_code: res.device_code,
            });

            if (pollRes.status === "slow_down") {
              const backoff = (pollRes.interval || 5) * 1000;
              currentIntervalMs += backoff;
              setMsPollStatus("Adjusting polling cadence (slow_down)...");
              schedulePoll(currentIntervalMs);
              return;
            }

            if (pollRes.status === "authenticated" && pollRes.token && pollRes.user) {
              stopMSPolling();
              completeMicrosoftLogin(pollRes.token, pollRes.user);
              router.push("/");
            }
          } catch (pollErr: any) {
            const status = pollErr?.status;
            const msg = pollErr?.message || "";
            if (
              status === 403 &&
              (msg.toLowerCase().includes("pending") || msg.toLowerCase().includes("approval"))
            ) {
              stopMSPolling();
              setIsPendingApproval(true);
              setIsMSLoginActive(false);
              setError(msg);
            } else if (status === 400 || status >= 500) {
              stopMSPolling();
              setIsMSLoginActive(false);
              setError(msg || "Microsoft sign-in was interrupted. Please try again.");
            }
          }
        }, intervalMs);
      };

      schedulePoll(currentIntervalMs);
    } catch (err: any) {
      setError(err?.message || "Failed to initiate Microsoft login.");
    } finally {
      setIsMSLoading(false);
    }
  };

  const handleCancelMSLogin = () => {
    stopMSPolling();
    setIsMSLoginActive(false);
    setMsDeviceData(null);
    setMsPollStatus("");
  };

  const handleCopyCodeAndOpen = () => {
    if (!msDeviceData) return;
    navigator.clipboard.writeText(msDeviceData.user_code);
    setHasCopiedCode(true);
    setTimeout(() => setHasCopiedCode(false), 3000);
    window.open(msDeviceData.verification_uri || "https://microsoft.com/devicelogin", "_blank", "noopener,noreferrer");
  };

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

          {/* Microsoft Device Code View vs Standard Login Form */}
          {isMSLoginActive && msDeviceData ? (
            <div className="space-y-4">
              <div className="rounded-xl border border-sky-500/30 bg-sky-500/5 p-4 text-center space-y-3">
                <div className="flex items-center justify-center space-x-2 text-xs font-semibold text-sky-400">
                  <Image src="/kraken.png" alt="" width={16} height={16} className="inline-block" />
                  <span>Microsoft Sign-In Code</span>
                </div>
                <div className="py-2.5 px-4 rounded-lg bg-slate-950/80 border border-slate-700/80 font-mono text-xl tracking-wider font-bold text-white select-all">
                  {msDeviceData.user_code}
                </div>
                <p className="text-[11px] text-slate-300">
                  {msDeviceData.message || "To sign in, open https://microsoft.com/devicelogin and enter the code above."}
                </p>
                <Button
                  type="button"
                  onClick={handleCopyCodeAndOpen}
                  className="w-full h-10 font-medium inline-flex items-center justify-center space-x-2 bg-gradient-to-r from-sky-500 to-indigo-600 hover:from-sky-400 hover:to-indigo-500 text-white"
                >
                  {hasCopiedCode ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                  <span>{hasCopiedCode ? "Code Copied! Open Login" : "Copy Code & Open Microsoft Login"}</span>
                  <ExternalLink className="h-3.5 w-3.5 ml-1" />
                </Button>
              </div>

              {msPollStatus && (
                <div className="flex items-center justify-center space-x-2 text-[11px] text-slate-400">
                  <div className="h-1.5 w-1.5 rounded-full bg-sky-400 animate-pulse" />
                  <span>{msPollStatus}</span>
                </div>
              )}

              <Button
                type="button"
                variant="outline"
                onClick={handleCancelMSLogin}
                className="w-full h-9 text-xs"
              >
                Cancel / Use Local Account
              </Button>
            </div>
          ) : (
            <>
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

              <div className="relative my-4">
                <div className="absolute inset-0 flex items-center">
                  <div className="w-full border-t border-slate-800" />
                </div>
                <div className="relative flex justify-center text-[10px] uppercase">
                  <span className="bg-slate-900 px-2 text-slate-500">Or continue with</span>
                </div>
              </div>

              {/* Microsoft Device Code Login Button */}
              <button
                type="button"
                onClick={handleStartMicrosoftLogin}
                disabled={isMSLoading || isSubmitting}
                className="w-full h-10 flex items-center justify-center space-x-2.5 rounded-xl border border-slate-700/80 bg-slate-800/60 hover:bg-slate-800 hover:border-slate-600 transition-all text-xs font-medium text-slate-200 disabled:opacity-50"
              >
                {/* Microsoft 4-box logo SVG */}
                <svg className="h-4 w-4" viewBox="0 0 21 21" fill="none">
                  <rect x="1" y="1" width="9" height="9" fill="#F25022" />
                  <rect x="11" y="1" width="9" height="9" fill="#7FBA00" />
                  <rect x="1" y="11" width="9" height="9" fill="#00A4EF" />
                  <rect x="11" y="11" width="9" height="9" fill="#FFB900" />
                </svg>
                <span>{isMSLoading ? "Initiating Device Code..." : "Sign in with Microsoft"}</span>
              </button>
            </>
          )}

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
