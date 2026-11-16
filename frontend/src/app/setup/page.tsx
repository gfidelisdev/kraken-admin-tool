"use client";

import React, { useState, useEffect } from "react";
import Image from "next/image";
import { useRouter } from "next/navigation";
import {
  ShieldCheck,
  User as UserIcon,
  Mail,
  Lock,
  AlertCircle,
  ArrowRight,
  ChevronDown,
  ChevronUp,
  Loader2,
  Container,
  Server,
  KeyRound,
  Sparkles,
} from "lucide-react";
import { useAuth } from "@/context/AuthContext";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";

function generateStrongSecret(byteLen = 48): string {
  const bytes = new Uint8Array(byteLen);
  if (typeof window !== "undefined" && window.crypto?.getRandomValues) {
    window.crypto.getRandomValues(bytes);
  } else {
    for (let i = 0; i < byteLen; i++) bytes[i] = Math.floor(Math.random() * 256);
  }
  let binary = "";
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export default function SetupPage() {
  const router = useRouter();
  const { isAuthenticated, isLoading: authLoading, needsSetup, setupStatus, completeSetup } = useAuth();

  const [adminUsername, setAdminUsername] = useState("admin");
  const [adminEmail, setAdminEmail] = useState("admin@kraken.local");
  const [adminPassword, setAdminPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [allowRegistration, setAllowRegistration] = useState(false);
  const [showMSConfig, setShowMSConfig] = useState(false);
  const [microsoftTenantID, setMicrosoftTenantID] = useState("");
  const [microsoftClientID, setMicrosoftClientID] = useState("");
  const [showDomainConfig, setShowDomainConfig] = useState(false);
  const [fqdnInput, setFqdnInput] = useState("");
  const [enableIngress, setEnableIngress] = useState(false);
  const [ingressClassName, setIngressClassName] = useState("");
  const [showSecretsConfig, setShowSecretsConfig] = useState(false);
  const [jwtSecret, setJwtSecret] = useState("");
  const [encryptionKey, setEncryptionKey] = useState("");
  const [dbPassword, setDbPassword] = useState("");
  const [adminRecoveryKey, setAdminRecoveryKey] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (setupStatus) {
      setAllowRegistration(Boolean(setupStatus.allow_registration));
      if (setupStatus.secrets_preconfigured === false) {
        setShowSecretsConfig(true);
      }
    }
  }, [setupStatus]);

  useEffect(() => {
    if (!authLoading && !needsSetup) {
      router.replace(isAuthenticated ? "/" : "/login");
    }
  }, [authLoading, needsSetup, isAuthenticated, router]);

  if (authLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-[#090d16]">
        <div className="flex flex-col items-center space-y-4">
          <Loader2 className="h-10 w-10 animate-spin text-sky-400" />
          <p className="text-xs font-mono text-slate-400">Checking system initialization...</p>
        </div>
      </div>
    );
  }

  if (!needsSetup) {
    return null;
  }

  const isKubernetes = setupStatus?.runtime_mode === "kubernetes";

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!adminUsername.trim() || !adminEmail.trim() || !adminPassword) {
      setError("Administrator username, email, and password are required.");
      return;
    }

    if (adminPassword.length < 8) {
      setError("Administrator password must be at least 8 characters long.");
      return;
    }

    if (adminPassword !== confirmPassword) {
      setError("Passwords do not match.");
      return;
    }

    setIsSubmitting(true);
    try {
      const parsedDomains = fqdnInput
        .split(/[\s,;]+/)
        .map((d) => d.trim())
        .filter(Boolean);

      await completeSetup({
        admin_username: adminUsername.trim(),
        admin_email: adminEmail.trim(),
        admin_password: adminPassword,
        allow_registration: allowRegistration,
        microsoft_tenant_id: microsoftTenantID.trim() || undefined,
        microsoft_client_id: microsoftClientID.trim() || undefined,
        domains: parsedDomains.length > 0 ? parsedDomains : undefined,
        ingress_enabled: parsedDomains.length > 0 ? enableIngress : undefined,
        ingress_class_name: ingressClassName.trim() || undefined,
        jwt_secret: jwtSecret.trim() || undefined,
        encryption_key: encryptionKey.trim() || undefined,
        db_password: dbPassword.trim() || undefined,
        admin_recovery_key: adminRecoveryKey.trim() || undefined,
      });
      router.replace("/");
    } catch (err: any) {
      setError(err?.message || "Failed to complete initial configuration.");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="flex min-h-screen flex-col items-center justify-center p-4 relative overflow-hidden bg-[#090d16]">
      {/* Background glowing spheres */}
      <div className="absolute top-1/4 -left-32 h-96 w-96 rounded-full bg-sky-500/10 blur-3xl pointer-events-none" />
      <div className="absolute bottom-1/4 -right-32 h-96 w-96 rounded-full bg-indigo-500/10 blur-3xl pointer-events-none" />

      <div className="w-full max-w-lg space-y-6 z-10">
        {/* Brand header */}
        <div className="text-center space-y-2">
          <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-tr from-sky-500 to-indigo-600 shadow-xl shadow-sky-500/25">
            <Image src="/kraken.png" alt="" width={50} height={50} />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-white">
            Kraken Initial Setup
          </h1>
          <p className="text-xs text-slate-400">
            Configure your primary administrator credentials and essential security settings before first use.
          </p>

          <div className="pt-1 flex justify-center">
            <div
              className={`inline-flex items-center space-x-1.5 rounded-full border px-3 py-1 text-xs font-medium ${
                isKubernetes
                  ? "border-indigo-500/30 bg-indigo-500/10 text-indigo-300"
                  : "border-sky-500/30 bg-sky-500/10 text-sky-300"
              }`}
            >
              {isKubernetes ? (
                <Server className="h-3.5 w-3.5" />
              ) : (
                <Container className="h-3.5 w-3.5" />
              )}
              <span>
                Runtime Environment: {isKubernetes ? "Kubernetes Cluster" : "Docker Compose"}
              </span>
            </div>
          </div>
        </div>

        {/* Setup Card */}
        <div className="rounded-2xl border border-slate-800 bg-slate-900/80 p-8 shadow-2xl backdrop-blur-md space-y-5">
          <div className="flex items-start space-x-2.5 rounded-xl border border-sky-500/20 bg-sky-500/5 p-3.5 text-xs text-sky-300">
            <ShieldCheck className="h-4 w-4 text-sky-400 shrink-0 mt-0.5" />
            <div className="space-y-1">
              <p className="font-semibold text-sky-200">First-Boot Security Configuration</p>
              <p className="text-[11px] text-slate-300 leading-relaxed">
                This screen is shown only once before any administrator exists. Cryptographic JWT and encryption keys are provisioned automatically.
              </p>
            </div>
          </div>

          {error && (
            <div className="flex items-start space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
              <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
              <span>{error}</span>
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div>
                <label className="text-xs font-medium text-slate-300">Admin Username</label>
                <div className="mt-1">
                  <Input
                    placeholder="admin"
                    value={adminUsername}
                    onChange={(e) => setAdminUsername(e.target.value)}
                    icon={<UserIcon className="h-4 w-4" />}
                    autoFocus
                  />
                </div>
              </div>

              <div>
                <label className="text-xs font-medium text-slate-300">Admin Email</label>
                <div className="mt-1">
                  <Input
                    type="email"
                    placeholder="admin@kraken.local"
                    value={adminEmail}
                    onChange={(e) => setAdminEmail(e.target.value)}
                    icon={<Mail className="h-4 w-4" />}
                  />
                </div>
              </div>
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300">Admin Password</label>
              <div className="mt-1">
                <Input
                  type="password"
                  placeholder="Minimum 8 characters"
                  value={adminPassword}
                  onChange={(e) => setAdminPassword(e.target.value)}
                  icon={<Lock className="h-4 w-4" />}
                />
              </div>
            </div>

            <div>
              <label className="text-xs font-medium text-slate-300">Confirm Admin Password</label>
              <div className="mt-1">
                <Input
                  type="password"
                  placeholder="Repeat administrator password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  icon={<Lock className="h-4 w-4" />}
                />
              </div>
            </div>

            {/* Essential Security Settings */}
            <div className="rounded-xl border border-slate-800 bg-slate-950/50 p-3.5 space-y-3">
              <label className="flex items-start space-x-2.5 cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={allowRegistration}
                  onChange={(e) => setAllowRegistration(e.target.checked)}
                  className="mt-0.5 h-4 w-4 rounded border-slate-700 bg-slate-900 text-sky-500 focus:ring-sky-500"
                />
                <div className="space-y-0.5">
                  <span className="text-xs font-medium text-slate-200 block">
                    Allow self-registration requests
                  </span>
                  <span className="text-[11px] text-slate-400 block">
                    New users remain pending until explicitly approved and assigned a role by an administrator.
                  </span>
                </div>
              </label>

              <div className="border-t border-slate-800/80 pt-2.5">
                <button
                  type="button"
                  onClick={() => setShowSecretsConfig((prev) => !prev)}
                  className="flex w-full items-center justify-between text-xs font-medium text-slate-300 hover:text-white"
                >
                  <span className="flex items-center gap-1.5">
                    <KeyRound className="h-3.5 w-3.5 text-amber-400" />
                    <span>
                      System Secrets &amp; Keys{" "}
                      {setupStatus?.secrets_preconfigured === false
                        ? "(Recommended — Not Set in Env / K8s Secret)"
                        : "(Optional Override)"}
                    </span>
                  </span>
                  {showSecretsConfig ? (
                    <ChevronUp className="h-4 w-4 text-slate-400" />
                  ) : (
                    <ChevronDown className="h-4 w-4 text-slate-400" />
                  )}
                </button>

                {showSecretsConfig && (
                  <div className="mt-3 space-y-3">
                    <div className="flex items-center justify-between gap-2 rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-2 text-[11px] text-amber-200">
                      <span>
                        {setupStatus?.secrets_preconfigured === false
                          ? "No external .env or Kubernetes Secret detected. Provide or auto-generate secrets below."
                          : "Secrets are already initialized; leave blank to keep current values or enter new ones."}
                      </span>
                      <button
                        type="button"
                        onClick={() => {
                          if (!jwtSecret) setJwtSecret(generateStrongSecret(48));
                          if (!encryptionKey) setEncryptionKey(generateStrongSecret(48));
                          if (!dbPassword) setDbPassword(generateStrongSecret(24));
                          if (!adminRecoveryKey) setAdminRecoveryKey(generateStrongSecret(48));
                        }}
                        className="shrink-0 inline-flex items-center gap-1 rounded-md bg-sky-500/20 px-2 py-1 text-[10px] font-semibold text-sky-300 hover:bg-sky-500/30"
                      >
                        <Sparkles className="h-3 w-3" />
                        Generate All
                      </button>
                    </div>

                    <div>
                      <div className="flex items-center justify-between">
                        <label className="text-[11px] font-medium text-slate-400">JWT_SECRET (32+ chars)</label>
                        <button
                          type="button"
                          onClick={() => setJwtSecret(generateStrongSecret(48))}
                          className="text-[10px] text-sky-400 hover:underline"
                        >
                          Generate
                        </button>
                      </div>
                      <div className="mt-1">
                        <Input
                          type="password"
                          placeholder="Leave blank to keep auto-generated secret"
                          value={jwtSecret}
                          onChange={(e) => setJwtSecret(e.target.value)}
                        />
                      </div>
                    </div>

                    <div>
                      <div className="flex items-center justify-between">
                        <label className="text-[11px] font-medium text-slate-400">ENCRYPTION_KEY (32+ chars)</label>
                        <button
                          type="button"
                          onClick={() => setEncryptionKey(generateStrongSecret(48))}
                          className="text-[10px] text-sky-400 hover:underline"
                        >
                          Generate
                        </button>
                      </div>
                      <div className="mt-1">
                        <Input
                          type="password"
                          placeholder="Leave blank to keep auto-generated key"
                          value={encryptionKey}
                          onChange={(e) => setEncryptionKey(e.target.value)}
                        />
                      </div>
                    </div>

                    <div>
                      <div className="flex items-center justify-between">
                        <label className="text-[11px] font-medium text-slate-400">DB_PASSWORD / POSTGRES_PASSWORD (8+ chars)</label>
                        <button
                          type="button"
                          onClick={() => setDbPassword(generateStrongSecret(24))}
                          className="text-[10px] text-sky-400 hover:underline"
                        >
                          Generate
                        </button>
                      </div>
                      <div className="mt-1">
                        <Input
                          type="password"
                          placeholder="Set PostgreSQL password (syncs live DB & K8s Secret)"
                          value={dbPassword}
                          onChange={(e) => setDbPassword(e.target.value)}
                        />
                      </div>
                    </div>

                    <div>
                      <div className="flex items-center justify-between">
                        <label className="text-[11px] font-medium text-slate-400">ADMIN_RECOVERY_KEY (Optional, 32+ chars)</label>
                        <button
                          type="button"
                          onClick={() => setAdminRecoveryKey(generateStrongSecret(48))}
                          className="text-[10px] text-sky-400 hover:underline"
                        >
                          Generate
                        </button>
                      </div>
                      <div className="mt-1">
                        <Input
                          type="password"
                          placeholder="Optional emergency HTTP recovery key"
                          value={adminRecoveryKey}
                          onChange={(e) => setAdminRecoveryKey(e.target.value)}
                        />
                      </div>
                    </div>
                  </div>
                )}
              </div>

              <div className="border-t border-slate-800/80 pt-2.5">
                <button
                  type="button"
                  onClick={() => setShowMSConfig((prev) => !prev)}
                  className="flex w-full items-center justify-between text-xs font-medium text-slate-300 hover:text-white"
                >
                  <span>Optional: Microsoft Entra ID (Azure AD) SSO</span>
                  {showMSConfig ? (
                    <ChevronUp className="h-4 w-4 text-slate-400" />
                  ) : (
                    <ChevronDown className="h-4 w-4 text-slate-400" />
                  )}
                </button>

                {showMSConfig && (
                  <div className="mt-3 space-y-3">
                    <div>
                      <label className="text-[11px] font-medium text-slate-400">
                        Tenant ID (defaults to &quot;{setupStatus?.microsoft_tenant_id || "common"}&quot;)
                      </label>
                      <div className="mt-1">
                        <Input
                          placeholder={setupStatus?.microsoft_tenant_id || "common"}
                          value={microsoftTenantID}
                          onChange={(e) => setMicrosoftTenantID(e.target.value)}
                        />
                      </div>
                    </div>
                    <div>
                      <label className="text-[11px] font-medium text-slate-400">
                        Client ID (defaults to Azure CLI public client)
                      </label>
                      <div className="mt-1">
                        <Input
                          placeholder={setupStatus?.microsoft_client_id || "04b07795-8ddb-461a-bbee-02f9e1bf7b46"}
                          value={microsoftClientID}
                          onChange={(e) => setMicrosoftClientID(e.target.value)}
                        />
                      </div>
                    </div>
                  </div>
                )}
              </div>

              <div className="border-t border-slate-800/80 pt-2.5">
                <button
                  type="button"
                  onClick={() => setShowDomainConfig((prev) => !prev)}
                  className="flex w-full items-center justify-between text-xs font-medium text-slate-300 hover:text-white"
                >
                  <span>Optional: Application FQDN &amp; Kubernetes Ingress (Port 443)</span>
                  {showDomainConfig ? (
                    <ChevronUp className="h-4 w-4 text-slate-400" />
                  ) : (
                    <ChevronDown className="h-4 w-4 text-slate-400" />
                  )}
                </button>

                {showDomainConfig && (
                  <div className="mt-3 space-y-3">
                    <div>
                      <label className="text-[11px] font-medium text-slate-400">
                        DNS Names / FQDNs (comma-separated, leave blank to start on ip:port)
                      </label>
                      <div className="mt-1">
                        <Input
                          placeholder="kat.mycompany.com, kraken.mycompany.com"
                          value={fqdnInput}
                          onChange={(e) => {
                            const val = e.target.value;
                            setFqdnInput(val);
                            if (val.trim() && !enableIngress && isKubernetes) {
                              setEnableIngress(true);
                            }
                          }}
                        />
                      </div>
                    </div>
                    <label className="flex items-center space-x-2 cursor-pointer select-none">
                      <input
                        type="checkbox"
                        checked={enableIngress}
                        onChange={(e) => setEnableIngress(e.target.checked)}
                        className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-sky-500 focus:ring-sky-500"
                      />
                      <span className="text-[11px] text-slate-300">
                        Enable Kubernetes Ingress on port 443 with auto-generated multi-domain TLS certificate
                      </span>
                    </label>
                    {enableIngress && (
                      <div>
                        <label className="text-[11px] font-medium text-slate-400">
                          Ingress Class Name (optional, e.g. nginx or traefik)
                        </label>
                        <div className="mt-1">
                          <Input
                            placeholder="nginx"
                            value={ingressClassName}
                            onChange={(e) => setIngressClassName(e.target.value)}
                          />
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            </div>

            <Button
              type="submit"
              className="w-full h-10 mt-2 font-semibold"
              isLoading={isSubmitting}
            >
              Complete Setup &amp; Launch Dashboard
              <ArrowRight className="ml-2 h-4 w-4" />
            </Button>
          </form>
        </div>
      </div>
    </div>
  );
}
