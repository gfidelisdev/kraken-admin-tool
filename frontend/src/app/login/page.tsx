"use client";

import React, { useState, useEffect } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Anchor, Lock, User, AlertCircle, ArrowRight, Sparkles } from "lucide-react";
import { useAuth } from "@/context/AuthContext";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";

export default function LoginPage() {
  const router = useRouter();
  const { login, isAuthenticated, isLoading: authLoading } = useAuth();

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

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

    try {
      await login(username, password);
      router.push("/");
    } catch (err: any) {
      setError(err?.message || "Invalid credentials. Please check your username and password.");
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleUseDefaultCredentials = () => {
    setUsername("admin");
    setPassword("admin123");
    setError(null);
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
            <Anchor className="h-8 w-8 text-white" />
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
                <span>Development Default: <strong>admin</strong> / <strong>admin123</strong></span>
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

          {error && (
            <div className="flex items-center space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
              <AlertCircle className="h-4 w-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

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
              <label className="text-xs font-medium text-slate-300">Password</label>
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

          <div className="text-center pt-2 border-t border-slate-800/80">
            <p className="text-xs text-slate-400">
              Don&apos;t have an account?{" "}
              <Link href="/register" className="text-sky-400 hover:underline font-medium">
                Create admin account
              </Link>
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
