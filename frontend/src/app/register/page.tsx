"use client";

import React, { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  Anchor,
  Lock,
  User as UserIcon,
  Mail,
  AlertCircle,
  ArrowRight,
  Shield,
  CheckCircle2,
} from "lucide-react";
import { useAuth } from "@/context/AuthContext";
import { Input } from "@/components/ui/Input";
import { Select } from "@/components/ui/Select";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { api } from "@/lib/api";

export default function RegisterPage() {
  const router = useRouter();
  const { user, register } = useAuth();
  const isAdmin = user?.role === "admin";

  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [role, setRole] = useState("operator");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccess(null);

    if (!username || !email || !password) {
      setError("All fields are required");
      return;
    }

    if (password !== confirmPassword) {
      setError("Passwords do not match");
      return;
    }

    if (password.length < 8) {
      setError("Password must be at least 8 characters long");
      return;
    }

    setIsSubmitting(true);

    try {
      if (isAdmin) {
        // Admin creating a user: do not overwrite current admin token
        await api.post("/api/auth/register", {
          username,
          email,
          password,
          role,
        });
        setSuccess(`User "${username}" created successfully with role "${role}".`);
        setUsername("");
        setEmail("");
        setPassword("");
        setConfirmPassword("");
      } else {
        // Public / initial registration
        await register(username, email, password);
        router.push("/");
      }
    } catch (err: any) {
      const msg = err?.message || "Registration failed. Please check your data.";
      if (err?.status === 403 || msg.includes("restricted") || msg.includes("disabled")) {
        setError(
          "Public self-registration is disabled. Please log in with your administrator account (e.g. admin) to create new users, or set ALLOW_REGISTRATION=true in the backend configuration."
        );
      } else {
        setError(msg);
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="flex min-h-screen flex-col items-center justify-center p-4 relative overflow-hidden bg-[#090d16]">
      {/* Background glowing spheres */}
      <div className="absolute top-1/4 -right-32 h-96 w-96 rounded-full bg-sky-500/10 blur-3xl pointer-events-none" />
      <div className="absolute bottom-1/4 -left-32 h-96 w-96 rounded-full bg-indigo-500/10 blur-3xl pointer-events-none" />

      <div className="w-full max-w-md space-y-6 z-10">
        {/* Brand header */}
        <div className="text-center space-y-2">
          <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-tr from-sky-500 to-indigo-600 shadow-xl shadow-sky-500/25">
            <Anchor className="h-8 w-8 text-white" />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-white">
            {isAdmin ? "Add New System User" : "Create Account"}
          </h1>
          <p className="text-xs text-slate-400">
            {isAdmin
              ? "Provision access and permissions for team members"
              : "Register for access to Kraken Admin Tool"}
          </p>

          {isAdmin && (
            <div className="inline-flex items-center space-x-1.5 mt-2 rounded-full border border-sky-500/30 bg-sky-500/10 px-3 py-1 text-xs text-sky-400">
              <Shield className="h-3.5 w-3.5" />
              <span>Admin Session Active ({user.username})</span>
            </div>
          )}
        </div>

        {/* Card */}
        <div className="rounded-2xl border border-slate-800 bg-slate-900/80 p-8 shadow-2xl backdrop-blur-md space-y-5">
          {error && (
            <div className="flex items-start space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
              <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
              <span>{error}</span>
            </div>
          )}

          {success && (
            <div className="flex items-start space-x-2 rounded-xl border border-emerald-500/20 bg-emerald-500/10 p-3 text-xs text-emerald-400">
              <CheckCircle2 className="h-4 w-4 shrink-0 mt-0.5" />
              <div className="flex-1">
                <p>{success}</p>
                <div className="mt-2 flex space-x-2">
                  <Link
                    href="/"
                    className="inline-flex items-center text-xs font-semibold text-emerald-300 underline hover:text-emerald-200"
                  >
                    Return to Dashboard &rarr;
                  </Link>
                </div>
              </div>
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="text-xs font-medium text-slate-300">Username</label>
              <div className="mt-1">
                <Input
                  placeholder="e.g. dev_operator"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  icon={<UserIcon className="h-4 w-4" />}
                  autoFocus
                />
              </div>
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300">Email Address</label>
              <div className="mt-1">
                <Input
                  type="email"
                  placeholder="e.g. operator@kraken.local"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  icon={<Mail className="h-4 w-4" />}
                />
              </div>
            </div>

            {isAdmin && (
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
            )}

            <div>
              <label className="text-xs font-medium text-slate-300">Password</label>
              <div className="mt-1">
                <Input
                  type="password"
                  placeholder="Minimum 8 characters"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  icon={<Lock className="h-4 w-4" />}
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
                />
              </div>
            </div>

            <Button
              type="submit"
              className="w-full h-10 mt-2 font-semibold"
              isLoading={isSubmitting}
            >
              {isAdmin ? "Create User Account" : "Register & Sign In"}
              <ArrowRight className="ml-2 h-4 w-4" />
            </Button>
          </form>

          <div className="text-center pt-2 border-t border-slate-800/80">
            {isAdmin ? (
              <p className="text-xs text-slate-400">
                Finished creating users?{" "}
                <Link href="/" className="text-sky-400 hover:underline font-medium">
                  Back to Dashboard
                </Link>
              </p>
            ) : (
              <p className="text-xs text-slate-400">
                Already have an account?{" "}
                <Link href="/login" className="text-sky-400 hover:underline font-medium">
                  Sign in here
                </Link>
              </p>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
