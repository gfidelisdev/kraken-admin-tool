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
  ArrowLeft,
  Loader2,
  Container,
  Server,
  KeyRound,
  Sparkles,
  Database,
  Globe,
  Eye,
  EyeOff,
  Copy,
  Check,
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

const STEPS = [
  { id: 1, title: "Admin", icon: UserIcon },
  { id: 2, title: "Database", icon: Database },
  { id: 3, title: "Secrets", icon: ShieldCheck },
  { id: 4, title: "Entra ID", icon: KeyRound },
  { id: 5, title: "Domain / FQDN", icon: Globe },
] as const;

type SecretFieldKey = "db_password" | "jwt_secret" | "encryption_key" | "admin_recovery_key";

export default function SetupPage() {
  const router = useRouter();
  const { isAuthenticated, isLoading: authLoading, needsSetup, setupStatus, completeSetup } = useAuth();

  const [step, setStep] = useState<number>(1);

  // Step 1: Admin
  const [adminUsername, setAdminUsername] = useState("admin");
  const [adminEmail, setAdminEmail] = useState("admin@kraken.local");
  const [adminPassword, setAdminPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [showAdminPassword, setShowAdminPassword] = useState(false);
  const [allowRegistration, setAllowRegistration] = useState(false);

  // Step 2: Database
  const [dbUsername, setDbUsername] = useState("kraken");
  const [dbPassword, setDbPassword] = useState("");

  // Step 3: System Secrets
  const [jwtSecret, setJwtSecret] = useState("");
  const [encryptionKey, setEncryptionKey] = useState("");
  const [adminRecoveryKey, setAdminRecoveryKey] = useState("");

  // Visibility & Copy state for secrets
  const [visibleFields, setVisibleFields] = useState<Record<SecretFieldKey, boolean>>({
    db_password: false,
    jwt_secret: false,
    encryption_key: false,
    admin_recovery_key: false,
  });
  const [copiedField, setCopiedField] = useState<SecretFieldKey | null>(null);

  // Step 4: Microsoft Entra ID
  const [microsoftTenantID, setMicrosoftTenantID] = useState("");
  const [microsoftClientID, setMicrosoftClientID] = useState("");

  // Step 5: FQDN / Ingress
  const [fqdnInput, setFqdnInput] = useState("");
  const [enableIngress, setEnableIngress] = useState(false);
  const [ingressClassName, setIngressClassName] = useState("");

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (setupStatus) {
      setAllowRegistration(Boolean(setupStatus.allow_registration));
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

  const toggleVisibility = (field: SecretFieldKey) => {
    setVisibleFields((prev) => ({ ...prev, [field]: !prev[field] }));
  };

  const revealField = (field: SecretFieldKey) => {
    setVisibleFields((prev) => ({ ...prev, [field]: true }));
  };

  const handleCopy = async (field: SecretFieldKey, value: string) => {
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      setCopiedField(field);
      setTimeout(() => setCopiedField((cur) => (cur === field ? null : cur)), 2000);
    } catch {
      // Clipboard API unavailable in non-secure contexts
    }
  };

  const validateStep = (targetStep: number): boolean => {
    setError(null);
    if (targetStep === 1) {
      if (!adminUsername.trim() || !adminEmail.trim() || !adminPassword) {
        setError("Administrator username, email, and password are required.");
        return false;
      }
      if (adminPassword.length < 8) {
        setError("Administrator password must be at least 8 characters long.");
        return false;
      }
      if (adminPassword !== confirmPassword) {
        setError("Passwords do not match.");
        return false;
      }
    } else if (targetStep === 2) {
      const trimmedUser = dbUsername.trim();
      if (!trimmedUser || !/^[a-zA-Z_][a-zA-Z0-9_]{0,62}$/.test(trimmedUser)) {
        setError("Database username must start with a letter or underscore and contain only letters, numbers, or underscores.");
        return false;
      }
      if (dbPassword.trim() && dbPassword.trim().length < 8) {
        setError("Database password must be at least 8 characters long.");
        return false;
      }
    } else if (targetStep === 3) {
      if (jwtSecret.trim() && jwtSecret.trim().length < 32) {
        setError("JWT_SECRET must be at least 32 characters long.");
        return false;
      }
      if (encryptionKey.trim() && encryptionKey.trim().length < 32) {
        setError("ENCRYPTION_KEY must be at least 32 characters long.");
        return false;
      }
      if (adminRecoveryKey.trim() && adminRecoveryKey.trim().length < 32) {
        setError("ADMIN_RECOVERY_KEY must be at least 32 characters long.");
        return false;
      }
    }
    return true;
  };

  const handleNext = () => {
    if (!validateStep(step)) return;
    setStep((s) => Math.min(STEPS.length, s + 1));
  };

  const handleBack = () => {
    setError(null);
    setStep((s) => Math.max(1, s - 1));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!validateStep(1) || !validateStep(2) || !validateStep(3)) return;

    setIsSubmitting(true);
    setError(null);
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
        db_username: dbUsername.trim() || undefined,
        db_password: dbPassword.trim() || undefined,
        jwt_secret: jwtSecret.trim() || undefined,
        encryption_key: encryptionKey.trim() || undefined,
        admin_recovery_key: adminRecoveryKey.trim() || undefined,
        microsoft_tenant_id: microsoftTenantID.trim() || undefined,
        microsoft_client_id: microsoftClientID.trim() || undefined,
        domains: parsedDomains.length > 0 ? parsedDomains : undefined,
        ingress_enabled: parsedDomains.length > 0 ? enableIngress : undefined,
        ingress_class_name: ingressClassName.trim() || undefined,
      });
      router.replace("/");
    } catch (err: any) {
      setError(err?.message || "Failed to complete initial setup.");
    } finally {
      setIsSubmitting(false);
    }
  };

  const renderSecretControl = (
    field: SecretFieldKey,
    label: string,
    value: string,
    onChange: (val: string) => void,
    placeholder: string,
    byteLen: number,
    helperText?: string
  ) => {
    const isVisible = visibleFields[field];
    const isCopied = copiedField === field;
    return (
      <div className="space-y-1.5">
        <div className="flex items-center justify-between">
          <label className="text-xs font-medium text-slate-300">{label}</label>
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => {
                onChange(generateStrongSecret(byteLen));
                revealField(field);
              }}
              className="text-[11px] font-medium text-sky-400 hover:text-sky-300 transition-colors"
            >
              Auto-generate
            </button>
            <button
              type="button"
              onClick={() => toggleVisibility(field)}
              className="flex items-center gap-1 text-[11px] font-medium text-slate-400 hover:text-slate-200 transition-colors"
              title={isVisible ? "Hide value" : "Show value"}
            >
              {isVisible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              <span>{isVisible ? "Hide" : "Show"}</span>
            </button>
            <button
              type="button"
              onClick={() => handleCopy(field, value)}
              disabled={!value}
              className="flex items-center gap-1 text-[11px] font-medium text-slate-400 hover:text-slate-200 disabled:opacity-40 transition-colors"
              title="Copy to clipboard"
            >
              {isCopied ? (
                <>
                  <Check className="h-3.5 w-3.5 text-emerald-400" />
                  <span className="text-emerald-400">Copied</span>
                </>
              ) : (
                <>
                  <Copy className="h-3.5 w-3.5" />
                  <span>Copy</span>
                </>
              )}
            </button>
          </div>
        </div>
        <Input
          type={isVisible ? "text" : "password"}
          placeholder={placeholder}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className="font-mono text-xs"
          autoComplete="new-password"
        />
        {helperText && <p className="text-[11px] text-slate-400">{helperText}</p>}
      </div>
    );
  };

  return (
    <div className="relative flex min-h-screen flex-col items-center justify-center bg-[#090d16] p-4 py-10 overflow-hidden">
      <div className="absolute -top-40 -left-40 h-96 w-96 rounded-full bg-sky-500/10 blur-3xl pointer-events-none" />
      <div className="absolute -bottom-40 -right-40 h-96 w-96 rounded-full bg-emerald-500/10 blur-3xl pointer-events-none" />

      <div className="relative z-10 w-full max-w-xl">
        {/* Header */}
        <div className="mb-6 flex flex-col items-center text-center">
          <div className="relative mb-3 flex h-16 w-16 items-center justify-center rounded-2xl bg-gradient-to-br from-sky-500/20 to-emerald-500/20 border border-sky-500/30 shadow-2xl shadow-sky-500/10">
            <Image
              src="/kraken-logo.png"
              alt="Kraken Admin Tool Logo"
              width={44}
              height={44}
              className="object-contain drop-shadow-[0_0_12px_rgba(56,189,248,0.4)]"
            />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-white">Kraken First-Time Setup</h1>
          <p className="mt-1 text-sm text-slate-400">
            Step {step} of {STEPS.length}: Configure your cluster securely before first use.
          </p>
          <div className="mt-2.5 inline-flex items-center space-x-2 rounded-full border border-slate-800 bg-slate-900/80 px-3 py-1 text-xs text-slate-300">
            {isKubernetes ? (
              <>
                <Server className="h-3.5 w-3.5 text-sky-400" />
                <span>Kubernetes Mode Detected (Pods + Ingress)</span>
              </>
            ) : (
              <>
                <Container className="h-3.5 w-3.5 text-emerald-400" />
                <span>Docker Compose Mode Detected</span>
              </>
            )}
          </div>
        </div>

        {/* Stepper */}
        <div className="mb-6 grid grid-cols-5 gap-2">
          {STEPS.map((s) => {
            const Icon = s.icon;
            const isActive = step === s.id;
            const isCompleted = step > s.id;
            return (
              <button
                key={s.id}
                type="button"
                onClick={() => {
                  if (s.id < step) {
                    setError(null);
                    setStep(s.id);
                  }
                }}
                className={`flex flex-col items-center gap-1 rounded-xl border px-2 py-2 text-[11px] font-medium transition-colors ${
                  isActive
                    ? "border-sky-500/60 bg-sky-500/15 text-sky-300"
                    : isCompleted
                    ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-300 cursor-pointer hover:bg-emerald-500/15"
                    : "border-slate-800 bg-slate-900/50 text-slate-500 cursor-default"
                }`}
              >
                <Icon className="h-3.5 w-3.5" />
                <span className="truncate max-w-full">{s.title}</span>
              </button>
            );
          })}
        </div>

        {/* Wizard Card */}
        <div className="rounded-2xl border border-slate-800/80 bg-slate-900/80 backdrop-blur-xl p-6 sm:p-8 shadow-2xl shadow-black/50">
          <form onSubmit={handleSubmit} className="space-y-5">
            {error && (
              <div className="flex items-start space-x-2.5 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3.5 text-xs text-rose-400">
                <AlertCircle className="h-4 w-4 shrink-0 mt-0.5" />
                <span>{error}</span>
              </div>
            )}

            {/* Step 1: Administrator Password */}
            {step === 1 && (
              <div className="space-y-4">
                <div className="flex items-center space-x-2 text-xs font-semibold uppercase tracking-wider text-sky-400">
                  <ShieldCheck className="h-4 w-4" />
                  <span>Step 1 — Administrator Credentials</span>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                  <div>
                    <label className="block text-xs font-medium text-slate-300 mb-1.5">Admin Username</label>
                    <Input
                      placeholder="admin"
                      value={adminUsername}
                      onChange={(e) => setAdminUsername(e.target.value)}
                      icon={<UserIcon className="h-4 w-4" />}
                      required
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-slate-300 mb-1.5">Admin Email</label>
                    <Input
                      type="email"
                      placeholder="admin@kraken.local"
                      value={adminEmail}
                      onChange={(e) => setAdminEmail(e.target.value)}
                      icon={<Mail className="h-4 w-4" />}
                      required
                    />
                  </div>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                  <div className="space-y-1.5">
                    <div className="flex items-center justify-between">
                      <label className="text-xs font-medium text-slate-300">Admin Password</label>
                      <button
                        type="button"
                        onClick={() => setShowAdminPassword((v) => !v)}
                        className="flex items-center gap-1 text-[11px] text-slate-400 hover:text-slate-200"
                      >
                        {showAdminPassword ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                        <span>{showAdminPassword ? "Hide" : "Show"}</span>
                      </button>
                    </div>
                    <Input
                      type={showAdminPassword ? "text" : "password"}
                      placeholder="At least 8 characters"
                      value={adminPassword}
                      onChange={(e) => setAdminPassword(e.target.value)}
                      icon={<Lock className="h-4 w-4" />}
                      autoComplete="new-password"
                      required
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-slate-300 mb-1.5">Confirm Password</label>
                    <Input
                      type={showAdminPassword ? "text" : "password"}
                      placeholder="Repeat password"
                      value={confirmPassword}
                      onChange={(e) => setConfirmPassword(e.target.value)}
                      icon={<Lock className="h-4 w-4" />}
                      autoComplete="new-password"
                      required
                    />
                  </div>
                </div>

                <label className="flex items-start space-x-3 rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 cursor-pointer hover:border-slate-700 transition-colors">
                  <input
                    type="checkbox"
                    checked={allowRegistration}
                    onChange={(e) => setAllowRegistration(e.target.checked)}
                    className="mt-0.5 h-4 w-4 rounded border-slate-700 bg-slate-900 text-sky-500 focus:ring-sky-500"
                  />
                  <div>
                    <span className="block text-sm font-medium text-slate-200">Allow self-registration</span>
                    <span className="block text-xs text-slate-400">
                      Users who register remain pending until approved by an administrator.
                    </span>
                  </div>
                </label>
              </div>
            )}

            {/* Step 2: Database Credentials */}
            {step === 2 && (
              <div className="space-y-4">
                <div className="flex items-center space-x-2 text-xs font-semibold uppercase tracking-wider text-sky-400">
                  <Database className="h-4 w-4" />
                  <span>Step 2 — Database Credentials</span>
                </div>
                <p className="text-xs text-slate-400">
                  Configure the PostgreSQL role and password used by Kraken. You can reveal and copy the password to
                  store it safely in your password manager.
                </p>

                <div>
                  <label className="block text-xs font-medium text-slate-300 mb-1.5">Database Username (db_username)</label>
                  <Input
                    placeholder="kraken"
                    value={dbUsername}
                    onChange={(e) => setDbUsername(e.target.value)}
                    icon={<UserIcon className="h-4 w-4" />}
                    required
                  />
                </div>

                {renderSecretControl(
                  "db_password",
                  "Database Password (db_password)",
                  dbPassword,
                  setDbPassword,
                  "Click Auto-generate or enter a strong password (min 8 chars)",
                  24,
                  "If the role does not yet exist in PostgreSQL, Kraken creates it automatically and updates Secret/kraken-secrets."
                )}
              </div>
            )}

            {/* Step 3: System Secrets */}
            {step === 3 && (
              <div className="space-y-4">
                <div className="flex items-center justify-between">
                  <div className="flex items-center space-x-2 text-xs font-semibold uppercase tracking-wider text-sky-400">
                    <ShieldCheck className="h-4 w-4" />
                    <span>Step 3 — System Secrets</span>
                  </div>
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    onClick={() => {
                      setJwtSecret(generateStrongSecret(48));
                      setEncryptionKey(generateStrongSecret(48));
                      setAdminRecoveryKey(generateStrongSecret(36));
                      setVisibleFields((prev) => ({
                        ...prev,
                        jwt_secret: true,
                        encryption_key: true,
                        admin_recovery_key: true,
                      }));
                    }}
                  >
                    Auto-Generate All
                  </Button>
                </div>
                <p className="text-xs text-slate-400">
                  Provide or auto-generate cryptographic secrets (minimum 32 characters). Leave blank to let Kraken
                  auto-generate and persist them securely.
                </p>

                {renderSecretControl(
                  "jwt_secret",
                  "JWT Signing Secret (JWT_SECRET)",
                  jwtSecret,
                  setJwtSecret,
                  "32+ characters (auto-generated if empty)",
                  48
                )}

                {renderSecretControl(
                  "encryption_key",
                  "Vault / Credential Encryption Key (ENCRYPTION_KEY)",
                  encryptionKey,
                  setEncryptionKey,
                  "32+ characters (auto-generated if empty)",
                  48
                )}

                {renderSecretControl(
                  "admin_recovery_key",
                  "Admin Recovery Key (ADMIN_RECOVERY_KEY)",
                  adminRecoveryKey,
                  setAdminRecoveryKey,
                  "32+ characters (optional emergency recovery key)",
                  36
                )}
              </div>
            )}

            {/* Step 4: Microsoft Entra ID */}
            {step === 4 && (
              <div className="space-y-4">
                <div className="flex items-center space-x-2 text-xs font-semibold uppercase tracking-wider text-sky-400">
                  <KeyRound className="h-4 w-4" />
                  <span>Step 4 — Microsoft Entra ID (Optional)</span>
                </div>
                <p className="text-xs text-slate-400">
                  Configure Microsoft Entra ID SSO parameters now, or leave blank to skip and configure later in Settings.
                </p>

                <div>
                  <label className="block text-xs font-medium text-slate-300 mb-1.5">Microsoft Tenant ID (Optional)</label>
                  <Input
                    placeholder="common or your Azure AD Tenant ID"
                    value={microsoftTenantID}
                    onChange={(e) => setMicrosoftTenantID(e.target.value)}
                  />
                </div>
                <div>
                  <label className="block text-xs font-medium text-slate-300 mb-1.5">Microsoft Client ID (Optional)</label>
                  <Input
                    placeholder="Azure App Registration Client ID"
                    value={microsoftClientID}
                    onChange={(e) => setMicrosoftClientID(e.target.value)}
                  />
                </div>
              </div>
            )}

            {/* Step 5: Domain & FQDN */}
            {step === 5 && (
              <div className="space-y-4">
                <div className="flex items-center space-x-2 text-xs font-semibold uppercase tracking-wider text-sky-400">
                  <Globe className="h-4 w-4" />
                  <span>Step 5 — Domain / FQDN & Ingress (Optional)</span>
                </div>
                <p className="text-xs text-slate-400">
                  You can start on IP:Port now and configure or add multiple DNS names anytime later in{" "}
                  <span className="text-slate-200 font-medium">Settings</span>.
                </p>

                <div>
                  <label className="block text-xs font-medium text-slate-300 mb-1.5">
                    FQDN / DNS Names (Optional, comma or space separated)
                  </label>
                  <Input
                    placeholder="kat.mycompany.com, kraken.mycompany.com"
                    value={fqdnInput}
                    onChange={(e) => setFqdnInput(e.target.value)}
                  />
                </div>

                {isKubernetes && (
                  <div className="space-y-3">
                    <label className="flex items-start space-x-3 rounded-xl border border-slate-800 bg-slate-950/60 p-3 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={enableIngress}
                        onChange={(e) => setEnableIngress(e.target.checked)}
                        className="mt-0.5 h-4 w-4 rounded border-slate-700 bg-slate-900 text-sky-500"
                      />
                      <div>
                        <span className="block text-xs font-medium text-slate-200">
                          Enable Kubernetes Ingress on port 443
                        </span>
                        <span className="block text-[11px] text-slate-400">
                          Generates a multi-SAN TLS certificate covering all listed FQDNs and updates Ingress automatically.
                        </span>
                      </div>
                    </label>
                    <div>
                      <label className="block text-xs font-medium text-slate-300 mb-1.5">
                        Ingress Class Name (Optional)
                      </label>
                      <Input
                        placeholder="nginx or traefik"
                        value={ingressClassName}
                        onChange={(e) => setIngressClassName(e.target.value)}
                      />
                    </div>
                  </div>
                )}
              </div>
            )}

            {/* Navigation Footer */}
            <div className="flex items-center justify-between gap-3 pt-2 border-t border-slate-800/80">
              {step > 1 ? (
                <Button
                  type="button"
                  variant="secondary"
                  size="lg"
                  onClick={handleBack}
                  disabled={isSubmitting}
                >
                  <ArrowLeft className="h-4 w-4 mr-1.5" />
                  Back
                </Button>
              ) : (
                <div />
              )}

              {step < STEPS.length ? (
                <Button
                  type="button"
                  variant="default"
                  size="lg"
                  className="shadow-sky-500/20"
                  onClick={handleNext}
                >
                  Next
                  <ArrowRight className="h-4 w-4 ml-1.5" />
                </Button>
              ) : (
                <Button
                  type="submit"
                  variant="default"
                  size="lg"
                  className="shadow-sky-500/20"
                  isLoading={isSubmitting}
                >
                  {!isSubmitting && <Sparkles className="h-4 w-4 mr-1.5" />}
                  Complete Setup & Launch
                </Button>
              )}
            </div>
          </form>
        </div>
      </div>
    </div>
  );
}
