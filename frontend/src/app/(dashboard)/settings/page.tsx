"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Globe,
  ShieldCheck,
  Plus,
  X,
  RefreshCw,
  Download,
  CheckCircle2,
  AlertCircle,
  Lock,
  Server,
  Container,
  ExternalLink,
  Loader2,
  KeyRound,
  Sparkles,
  Eye,
  EyeOff,
  Copy,
  Check,
} from "lucide-react";
import { useAuth } from "@/context/AuthContext";
import {
  getNetworkConfig,
  updateNetworkConfig,
  regenerateNetworkCertificate,
  getSystemSecrets,
  updateSystemSecrets,
} from "@/lib/api";
import { NetworkConfigResponse, SystemSecretsResponse } from "@/types";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";

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

export default function DomainIngressSettingsPage() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";

  const [activeTab, setActiveTab] = useState<"domain" | "secrets">("domain");
  const [config, setConfig] = useState<NetworkConfigResponse | null>(null);
  const [secretsData, setSecretsData] = useState<SystemSecretsResponse | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isSavingSecrets, setIsSavingSecrets] = useState(false);
  const [isRegenerating, setIsRegenerating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  // Secret form state
  const [jwtSecretInput, setJwtSecretInput] = useState("");
  const [encryptionKeyInput, setEncryptionKeyInput] = useState("");
  const [dbPasswordInput, setDbPasswordInput] = useState("");
  const [recoveryKeyInput, setRecoveryKeyInput] = useState("");
  const [msClientIDInput, setMsClientIDInput] = useState("");
  const [msTenantIDInput, setMsTenantIDInput] = useState("");
  const [restartOnSecretsSave, setRestartOnSecretsSave] = useState(false);

  // Visibility & copy states for sensitive secret fields
  const [showJwtSecret, setShowJwtSecret] = useState(false);
  const [showEncryptionKey, setShowEncryptionKey] = useState(false);
  const [showDbPassword, setShowDbPassword] = useState(false);
  const [showRecoveryKey, setShowRecoveryKey] = useState(false);
  const [copiedField, setCopiedField] = useState<string | null>(null);

  const copyToClipboard = (text: string, fieldName: string) => {
    if (!text) return;
    navigator.clipboard.writeText(text);
    setCopiedField(fieldName);
    setTimeout(() => {
      setCopiedField((current) => (current === fieldName ? null : current));
    }, 2000);
  };

  // Editable form state
  const [ingressEnabled, setIngressEnabled] = useState(false);
  const [domains, setDomains] = useState<string[]>([]);
  const [domainInput, setDomainInput] = useState("");
  const [ingressClassName, setIngressClassName] = useState("");
  const [standardHTTPSPort, setStandardHTTPSPort] = useState(true);
  const [certIssuer, setCertIssuer] = useState("");
  const [tlsMode, setTlsMode] = useState<"auto_self_signed" | "custom">("auto_self_signed");
  const [customCertPEM, setCustomCertPEM] = useState("");
  const [customKeyPEM, setCustomKeyPEM] = useState("");
  const [restartDeployments, setRestartDeployments] = useState(true);

  const loadConfig = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const data = await getNetworkConfig();
      setConfig(data);
      setIngressEnabled(Boolean(data.ingress_enabled));
      setDomains(Array.isArray(data.domains) ? data.domains : []);
      setIngressClassName(data.ingress_class_name || "");
      setStandardHTTPSPort(data.standard_https_port !== false);
      setCertIssuer(data.cert_issuer || "");
      setTlsMode(data.tls_mode === "custom" ? "custom" : "auto_self_signed");

      if (isAdmin) {
        try {
          const sec = await getSystemSecrets();
          setSecretsData(sec);
          setMsClientIDInput(sec.microsoft_client_id || "");
          setMsTenantIDInput(sec.microsoft_tenant_id || "");
        } catch {
          // ignore non-admin or transient secrets fetch issue
        }
      }
    } catch (err: any) {
      setError(err?.message || "Failed to load domain and ingress configuration.");
    } finally {
      setIsLoading(false);
    }
  }, [isAdmin]);

  useEffect(() => {
    loadConfig();
  }, [loadConfig]);

  const addDomainsFromInput = () => {
    const rawParts = domainInput
      .split(/[\s,;]+/)
      .map((s) => s.trim().toLowerCase().replace(/^https?:\/\//, "").split("/")[0].replace(/\.$/, ""))
      .filter(Boolean);

    if (rawParts.length === 0) return;

    setDomains((prev) => {
      const next = [...prev];
      for (const d of rawParts) {
        if (!next.includes(d)) {
          next.push(d);
        }
      }
      return next;
    });
    setDomainInput("");
    setError(null);
  };

  const removeDomain = (target: string) => {
    setDomains((prev) => prev.filter((d) => d !== target));
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!isAdmin) return;

    setError(null);
    setSuccess(null);

    // Include any uncommitted text currently typed in domainInput
    let effectiveDomains = [...domains];
    if (domainInput.trim()) {
      const pending = domainInput
        .split(/[\s,;]+/)
        .map((s) => s.trim().toLowerCase().replace(/^https?:\/\//, "").split("/")[0].replace(/\.$/, ""))
        .filter(Boolean);
      for (const d of pending) {
        if (!effectiveDomains.includes(d)) {
          effectiveDomains.push(d);
        }
      }
      setDomains(effectiveDomains);
      setDomainInput("");
    }

    if (ingressEnabled && effectiveDomains.length === 0) {
      setError("Add at least one DNS name / FQDN (e.g. kat.mycompany.com) when Ingress is enabled.");
      return;
    }

    if (tlsMode === "custom" && (!customCertPEM.trim() || !customKeyPEM.trim()) && !config?.cert_pem) {
      setError("Provide both PEM Certificate and PEM Private Key when using Custom TLS mode.");
      return;
    }

    setIsSaving(true);
    try {
      const updated = await updateNetworkConfig({
        ingress_enabled: ingressEnabled,
        domains: effectiveDomains,
        ingress_class_name: ingressClassName.trim() || undefined,
        standard_https_port: standardHTTPSPort,
        tls_mode: tlsMode,
        cert_issuer: certIssuer.trim() || undefined,
        custom_cert_pem: tlsMode === "custom" && customCertPEM.trim() ? customCertPEM.trim() : undefined,
        custom_key_pem: tlsMode === "custom" && customKeyPEM.trim() ? customKeyPEM.trim() : undefined,
        restart_deployments: restartDeployments,
      });
      setConfig(updated);
      setDomains(updated.domains || []);
      setCustomCertPEM("");
      setCustomKeyPEM("");
      setSuccess(
        updated.last_apply_status === "error"
          ? `Configuration saved, but Kubernetes sync reported an error: ${updated.last_apply_message}`
          : updated.k8s_connected
          ? `Configuration applied! ${updated.last_apply_message || "Kubernetes Ingress, TLS Secret, and rolling restarts triggered."}`
          : "Configuration and multi-domain TLS certificate saved."
      );
    } catch (err: any) {
      setError(err?.message || "Failed to apply domain and ingress configuration.");
    } finally {
      setIsSaving(false);
    }
  };

  const handleRegenerateCert = async () => {
    if (!isAdmin) return;
    setError(null);
    setSuccess(null);
    setIsRegenerating(true);
    try {
      const updated = await regenerateNetworkCertificate();
      setConfig(updated);
      setTlsMode("auto_self_signed");
      setSuccess("New multi-domain SAN TLS certificate generated and applied.");
    } catch (err: any) {
      setError(err?.message || "Failed to regenerate TLS certificate.");
    } finally {
      setIsRegenerating(false);
    }
  };

  const handleDownloadCert = () => {
    if (!config?.cert_pem) return;
    const blob = new Blob([config.cert_pem], { type: "application/x-pem-file" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = domains[0] ? `${domains[0]}.crt.pem` : "kraken-cert.pem";
    a.click();
    URL.revokeObjectURL(url);
  };

  const handleSaveSecrets = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!isAdmin) return;
    setError(null);
    setSuccess(null);
    setIsSavingSecrets(true);
    try {
      const updated = await updateSystemSecrets({
        jwt_secret: jwtSecretInput.trim() || undefined,
        encryption_key: encryptionKeyInput.trim() || undefined,
        db_password: dbPasswordInput.trim() || undefined,
        admin_recovery_key: recoveryKeyInput.trim() || undefined,
        microsoft_client_id: msClientIDInput.trim() || undefined,
        microsoft_tenant_id: msTenantIDInput.trim() || undefined,
        restart_deployments: restartOnSecretsSave,
      });
      setSecretsData(updated);
      setJwtSecretInput("");
      setEncryptionKeyInput("");
      setDbPasswordInput("");
      setRecoveryKeyInput("");
      setSuccess(
        updated.k8s_connected
          ? "System secrets persisted, PostgreSQL updated (if changed), and Kubernetes Secret/kraken-secrets synchronized."
          : "System secrets persisted and applied in-process."
      );
    } catch (err: any) {
      setError(err?.message || "Failed to update system secrets.");
    } finally {
      setIsSavingSecrets(false);
    }
  };

  if (isLoading) {
    return (
      <div className="flex min-h-[50vh] items-center justify-center">
        <div className="flex flex-col items-center space-y-3">
          <Loader2 className="h-8 w-8 animate-spin text-sky-400" />
          <p className="text-xs font-mono text-slate-400">Loading Domain &amp; Ingress settings...</p>
        </div>
      </div>
    );
  }

  const isK8s = config?.runtime_mode === "kubernetes";

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white flex items-center gap-2.5">
            <Globe className="h-6 w-6 text-sky-400" />
            <span>Domain, Ingress &amp; SSL (Port 443)</span>
          </h1>
          <p className="text-sm text-slate-400 mt-1">
            Configure one or multiple DNS names (FQDNs), standard HTTPS port 443 Ingress routing, and automatic multi-SAN TLS certificates at any time.
          </p>
        </div>

        <div className="flex items-center gap-2">
          <Badge variant={isK8s ? "k8s" : "docker"} className="px-3 py-1">
            {isK8s ? <Server className="h-3.5 w-3.5 mr-1.5" /> : <Container className="h-3.5 w-3.5 mr-1.5" />}
            {isK8s ? `Kubernetes (${config?.k8s_namespace || "kraken"})` : "Docker Compose"}
          </Badge>
          {config?.k8s_connected ? (
            <Badge variant="running" className="px-3 py-1">K8s API Sync Ready</Badge>
          ) : (
            <Badge variant="outline" className="px-3 py-1">Local Mode</Badge>
          )}
        </div>
      </div>

      {!isAdmin && (
        <div className="flex items-center gap-2 rounded-xl border border-amber-500/30 bg-amber-500/10 p-4 text-xs text-amber-300">
          <Lock className="h-4 w-4 shrink-0" />
          <span>Administrator privileges are required to modify FQDN, Ingress, or TLS settings.</span>
        </div>
      )}

      {error && (
        <div className="flex items-start gap-2.5 rounded-xl border border-rose-500/30 bg-rose-500/10 p-4 text-xs text-rose-300">
          <AlertCircle className="h-4 w-4 shrink-0 mt-0.5 text-rose-400" />
          <span>{error}</span>
        </div>
      )}

      {success && (
        <div className="flex items-start gap-2.5 rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-4 text-xs text-emerald-300">
          <CheckCircle2 className="h-4 w-4 shrink-0 mt-0.5 text-emerald-400" />
          <span>{success}</span>
        </div>
      )}

      {/* Navigation Tabs */}
      <div className="flex items-center gap-2 border-b border-slate-800 pb-3">
        <button
          type="button"
          onClick={() => {
            setActiveTab("domain");
            setError(null);
            setSuccess(null);
          }}
          className={`inline-flex items-center gap-2 rounded-lg px-4 py-2 text-xs font-semibold transition-all ${
            activeTab === "domain"
              ? "bg-sky-500/15 text-sky-300 border border-sky-500/30"
              : "text-slate-400 hover:text-slate-200 hover:bg-slate-800/50"
          }`}
        >
          <Globe className="h-4 w-4" />
          <span>Domain &amp; Ingress (Port 443)</span>
        </button>

        {isAdmin && (
          <button
            type="button"
            onClick={() => {
              setActiveTab("secrets");
              setError(null);
              setSuccess(null);
            }}
            className={`inline-flex items-center gap-2 rounded-lg px-4 py-2 text-xs font-semibold transition-all ${
              activeTab === "secrets"
                ? "bg-amber-500/15 text-amber-300 border border-amber-500/30"
                : "text-slate-400 hover:text-slate-200 hover:bg-slate-800/50"
            }`}
          >
            <KeyRound className="h-4 w-4" />
            <span>Security &amp; Secrets</span>
          </button>
        )}
      </div>

      {activeTab === "secrets" && isAdmin ? (
        <form onSubmit={handleSaveSecrets} className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <div className="lg:col-span-2 space-y-6">
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <KeyRound className="h-5 w-5 text-amber-400" />
                  <span>System Cryptographic &amp; Database Secrets</span>
                </CardTitle>
                <CardDescription>
                  Configure or rotate Kraken&apos;s core secrets at any time. Changes persist to <code className="text-slate-300">.kraken_secrets.json</code>, rotate <code className="text-slate-300">ENCRYPTION_KEY_PREVIOUS</code> without downtime, update PostgreSQL passwords, and synchronize <code className="text-slate-300">Secret/kraken-secrets</code> in Kubernetes.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex justify-end">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setJwtSecretInput(generateStrongSecret(48));
                      setEncryptionKeyInput(generateStrongSecret(48));
                      setDbPasswordInput(generateStrongSecret(24));
                      setRecoveryKeyInput(generateStrongSecret(48));
                    }}
                  >
                    <Sparkles className="h-3.5 w-3.5 mr-1.5 text-sky-400" />
                    Generate All New Secrets
                  </Button>
                </div>

                <div>
                  <div className="flex items-center justify-between">
                    <label className="text-xs font-medium text-slate-300">JWT_SECRET (32+ chars)</label>
                    <div className="flex items-center space-x-2">
                      {jwtSecretInput && (
                        <>
                          <button
                            type="button"
                            onClick={() => setShowJwtSecret(!showJwtSecret)}
                            className="text-[11px] text-slate-400 hover:text-slate-200 inline-flex items-center gap-1"
                            title={showJwtSecret ? "Hide secret" : "Show secret"}
                          >
                            {showJwtSecret ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                            <span>{showJwtSecret ? "Hide" : "Show"}</span>
                          </button>
                          <button
                            type="button"
                            onClick={() => copyToClipboard(jwtSecretInput, "jwt")}
                            className="text-[11px] text-slate-400 hover:text-slate-200 inline-flex items-center gap-1"
                            title="Copy secret"
                          >
                            {copiedField === "jwt" ? (
                              <Check className="h-3 w-3 text-emerald-400" />
                            ) : (
                              <Copy className="h-3 w-3" />
                            )}
                            <span className={copiedField === "jwt" ? "text-emerald-400" : ""}>
                              {copiedField === "jwt" ? "Copied" : "Copy"}
                            </span>
                          </button>
                        </>
                      )}
                      <button
                        type="button"
                        onClick={() => {
                          setJwtSecretInput(generateStrongSecret(48));
                          setShowJwtSecret(true);
                        }}
                        className="text-[11px] text-sky-400 hover:underline"
                      >
                        Generate
                      </button>
                    </div>
                  </div>
                  <div className="mt-1">
                    <Input
                      type={showJwtSecret ? "text" : "password"}
                      placeholder="Leave blank to keep current JWT_SECRET"
                      value={jwtSecretInput}
                      onChange={(e) => setJwtSecretInput(e.target.value)}
                    />
                  </div>
                </div>

                <div>
                  <div className="flex items-center justify-between">
                    <label className="text-xs font-medium text-slate-300">
                      ENCRYPTION_KEY (32+ chars — automatically archives old key to ENCRYPTION_KEY_PREVIOUS)
                    </label>
                    <div className="flex items-center space-x-2">
                      {encryptionKeyInput && (
                        <>
                          <button
                            type="button"
                            onClick={() => setShowEncryptionKey(!showEncryptionKey)}
                            className="text-[11px] text-slate-400 hover:text-slate-200 inline-flex items-center gap-1"
                            title={showEncryptionKey ? "Hide key" : "Show key"}
                          >
                            {showEncryptionKey ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                            <span>{showEncryptionKey ? "Hide" : "Show"}</span>
                          </button>
                          <button
                            type="button"
                            onClick={() => copyToClipboard(encryptionKeyInput, "enc")}
                            className="text-[11px] text-slate-400 hover:text-slate-200 inline-flex items-center gap-1"
                            title="Copy key"
                          >
                            {copiedField === "enc" ? (
                              <Check className="h-3 w-3 text-emerald-400" />
                            ) : (
                              <Copy className="h-3 w-3" />
                            )}
                            <span className={copiedField === "enc" ? "text-emerald-400" : ""}>
                              {copiedField === "enc" ? "Copied" : "Copy"}
                            </span>
                          </button>
                        </>
                      )}
                      <button
                        type="button"
                        onClick={() => {
                          setEncryptionKeyInput(generateStrongSecret(48));
                          setShowEncryptionKey(true);
                        }}
                        className="text-[11px] text-sky-400 hover:underline"
                      >
                        Generate
                      </button>
                    </div>
                  </div>
                  <div className="mt-1">
                    <Input
                      type={showEncryptionKey ? "text" : "password"}
                      placeholder="Leave blank to keep current ENCRYPTION_KEY"
                      value={encryptionKeyInput}
                      onChange={(e) => setEncryptionKeyInput(e.target.value)}
                    />
                  </div>
                </div>

                <div>
                  <div className="flex items-center justify-between">
                    <label className="text-xs font-medium text-slate-300">DB_PASSWORD / POSTGRES_PASSWORD (8+ chars)</label>
                    <div className="flex items-center space-x-2">
                      {dbPasswordInput && (
                        <>
                          <button
                            type="button"
                            onClick={() => setShowDbPassword(!showDbPassword)}
                            className="text-[11px] text-slate-400 hover:text-slate-200 inline-flex items-center gap-1"
                            title={showDbPassword ? "Hide password" : "Show password"}
                          >
                            {showDbPassword ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                            <span>{showDbPassword ? "Hide" : "Show"}</span>
                          </button>
                          <button
                            type="button"
                            onClick={() => copyToClipboard(dbPasswordInput, "db")}
                            className="text-[11px] text-slate-400 hover:text-slate-200 inline-flex items-center gap-1"
                            title="Copy password"
                          >
                            {copiedField === "db" ? (
                              <Check className="h-3 w-3 text-emerald-400" />
                            ) : (
                              <Copy className="h-3 w-3" />
                            )}
                            <span className={copiedField === "db" ? "text-emerald-400" : ""}>
                              {copiedField === "db" ? "Copied" : "Copy"}
                            </span>
                          </button>
                        </>
                      )}
                      <button
                        type="button"
                        onClick={() => {
                          setDbPasswordInput(generateStrongSecret(24));
                          setShowDbPassword(true);
                        }}
                        className="text-[11px] text-sky-400 hover:underline"
                      >
                        Generate
                      </button>
                    </div>
                  </div>
                  <div className="mt-1">
                    <Input
                      type={showDbPassword ? "text" : "password"}
                      placeholder="Leave blank to keep current PostgreSQL password"
                      value={dbPasswordInput}
                      onChange={(e) => setDbPasswordInput(e.target.value)}
                    />
                  </div>
                </div>

                <div>
                  <div className="flex items-center justify-between">
                    <label className="text-xs font-medium text-slate-300">ADMIN_RECOVERY_KEY (Optional, 32+ chars)</label>
                    <div className="flex items-center space-x-2">
                      {recoveryKeyInput && (
                        <>
                          <button
                            type="button"
                            onClick={() => setShowRecoveryKey(!showRecoveryKey)}
                            className="text-[11px] text-slate-400 hover:text-slate-200 inline-flex items-center gap-1"
                            title={showRecoveryKey ? "Hide recovery key" : "Show recovery key"}
                          >
                            {showRecoveryKey ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                            <span>{showRecoveryKey ? "Hide" : "Show"}</span>
                          </button>
                          <button
                            type="button"
                            onClick={() => copyToClipboard(recoveryKeyInput, "rec")}
                            className="text-[11px] text-slate-400 hover:text-slate-200 inline-flex items-center gap-1"
                            title="Copy recovery key"
                          >
                            {copiedField === "rec" ? (
                              <Check className="h-3 w-3 text-emerald-400" />
                            ) : (
                              <Copy className="h-3 w-3" />
                            )}
                            <span className={copiedField === "rec" ? "text-emerald-400" : ""}>
                              {copiedField === "rec" ? "Copied" : "Copy"}
                            </span>
                          </button>
                        </>
                      )}
                      <button
                        type="button"
                        onClick={() => {
                          setRecoveryKeyInput(generateStrongSecret(48));
                          setShowRecoveryKey(true);
                        }}
                        className="text-[11px] text-sky-400 hover:underline"
                      >
                        Generate
                      </button>
                    </div>
                  </div>
                  <div className="mt-1">
                    <Input
                      type={showRecoveryKey ? "text" : "password"}
                      placeholder="Leave blank to keep current recovery key"
                      value={recoveryKeyInput}
                      onChange={(e) => setRecoveryKeyInput(e.target.value)}
                    />
                  </div>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-2 border-t border-slate-800/80">
                  <div>
                    <label className="text-xs font-medium text-slate-300">MICROSOFT_TENANT_ID</label>
                    <div className="mt-1">
                      <Input
                        placeholder="common"
                        value={msTenantIDInput}
                        onChange={(e) => setMsTenantIDInput(e.target.value)}
                      />
                    </div>
                  </div>
                  <div>
                    <label className="text-xs font-medium text-slate-300">MICROSOFT_CLIENT_ID</label>
                    <div className="mt-1">
                      <Input
                        placeholder="04b07795-8ddb-461a-bbee-02f9e1bf7b46"
                        value={msClientIDInput}
                        onChange={(e) => setMsClientIDInput(e.target.value)}
                      />
                    </div>
                  </div>
                </div>

                {secretsData?.k8s_connected && (
                  <label className="flex items-center space-x-2.5 cursor-pointer select-none pt-1">
                    <input
                      type="checkbox"
                      checked={restartOnSecretsSave}
                      onChange={(e) => setRestartOnSecretsSave(e.target.checked)}
                      className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-sky-500 focus:ring-sky-500"
                    />
                    <span className="text-xs text-slate-300">
                      Trigger rolling restart of <code className="text-slate-400">backend</code> deployment after updating <code className="text-slate-400">Secret/kraken-secrets</code>
                    </span>
                  </label>
                )}

                <div className="flex justify-end pt-3 border-t border-slate-800/80">
                  <Button type="submit" isLoading={isSavingSecrets} className="font-semibold">
                    <ShieldCheck className="h-4 w-4 mr-2" />
                    Save &amp; Sync Secrets
                  </Button>
                </div>
              </CardContent>
            </Card>
          </div>

          <div className="space-y-6">
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Secret Inventory &amp; Status</CardTitle>
                <CardDescription>
                  Values are never revealed after saving
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                {(secretsData?.secrets || []).map((item) => (
                  <div
                    key={item.key}
                    className="rounded-xl border border-slate-800 bg-slate-950/60 p-3 space-y-1.5"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-xs font-mono font-semibold text-white">{item.key}</span>
                      <Badge
                        variant={
                          item.configured
                            ? item.source === "auto_generated"
                              ? "pending"
                              : "running"
                            : "outline"
                        }
                        className="text-[10px]"
                      >
                        {item.configured ? item.source : "unset"}
                      </Badge>
                    </div>
                    <p className="text-[11px] text-slate-400">{item.description}</p>
                    {item.masked_hint && (
                      <div className="text-[10px] font-mono text-slate-500">
                        Active: {item.masked_hint}
                      </div>
                    )}
                  </div>
                ))}

                {secretsData?.k8s_connected && (
                  <div className="flex items-center justify-between pt-2 border-t border-slate-800 text-xs">
                    <span className="text-slate-400">K8s Secret/kraken-secrets</span>
                    <Badge variant={secretsData.k8s_secret_present ? "running" : "pending"}>
                      {secretsData.k8s_secret_present ? "Present" : "Will Create on Save"}
                    </Badge>
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </form>
      ) : (
      <form onSubmit={handleSave} className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Left 2 columns: Domain & Ingress Configuration */}
        <div className="lg:col-span-2 space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Globe className="h-5 w-5 text-sky-400" />
                <span>DNS Names (FQDNs) &amp; Kubernetes Ingress</span>
              </CardTitle>
              <CardDescription>
                Start with IP:Port on initial install, then add company DNS names (e.g. <code className="text-sky-300">kat.mycompany.com</code>) whenever DNS points to this cluster.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-5">
              {/* Ingress Toggle */}
              <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-4 flex items-start justify-between gap-4">
                <div className="space-y-1">
                  <div className="text-sm font-medium text-white">
                    Enable Kubernetes Ingress on Standard HTTPS (Port 443)
                  </div>
                  <p className="text-xs text-slate-400">
                    Creates/updates <code className="text-slate-300">Ingress/kraken-ingress</code> with TLS termination on port 443 for all configured DNS names, updates ConfigMaps &amp; Proxy Service, and restarts workloads automatically.
                  </p>
                </div>
                <label className="relative inline-flex items-center cursor-pointer shrink-0 mt-1">
                  <input
                    type="checkbox"
                    disabled={!isAdmin}
                    checked={ingressEnabled}
                    onChange={(e) => setIngressEnabled(e.target.checked)}
                    className="sr-only peer"
                  />
                  <div className="w-11 h-6 bg-slate-700 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-sky-500" />
                </label>
              </div>

              {/* Add Domains */}
              <div className="space-y-2">
                <label className="text-xs font-medium text-slate-300">
                  Configured DNS Names / FQDNs (supports multiple domains)
                </label>
                <div className="flex gap-2">
                  <Input
                    disabled={!isAdmin}
                    placeholder="kat.mycompany.com, kraken.internal.corp"
                    value={domainInput}
                    onChange={(e) => setDomainInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") {
                        e.preventDefault();
                        addDomainsFromInput();
                      }
                    }}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    disabled={!isAdmin || !domainInput.trim()}
                    onClick={addDomainsFromInput}
                    className="shrink-0"
                  >
                    <Plus className="h-4 w-4 mr-1" />
                    Add
                  </Button>
                </div>
                <p className="text-[11px] text-slate-500">
                  Tip: Enter one FQDN or multiple comma-separated names and press Enter. All domains are automatically included as Subject Alternative Names (SANs) in the TLS certificate and Ingress rules.
                </p>

                {domains.length > 0 ? (
                  <div className="flex flex-wrap gap-2 pt-2">
                    {domains.map((domain, idx) => (
                      <div
                        key={domain}
                        className="inline-flex items-center gap-2 rounded-lg border border-sky-500/30 bg-sky-500/10 px-3 py-1.5 text-xs text-sky-200 font-mono"
                      >
                        <span>{domain}</span>
                        {idx === 0 && (
                          <span className="text-[10px] px-1.5 py-0.5 rounded bg-sky-500/20 text-sky-300 font-sans">
                            Primary CN
                          </span>
                        )}
                        <a
                          href={`https://${domain}`}
                          target="_blank"
                          rel="noreferrer"
                          title={`Open https://${domain}`}
                          className="text-sky-400 hover:text-white"
                        >
                          <ExternalLink className="h-3 w-3" />
                        </a>
                        {isAdmin && (
                          <button
                            type="button"
                            onClick={() => removeDomain(domain)}
                            className="text-slate-400 hover:text-rose-400"
                            aria-label={`Remove ${domain}`}
                          >
                            <X className="h-3.5 w-3.5" />
                          </button>
                        )}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="rounded-lg border border-dashed border-slate-800 p-3 text-xs text-slate-500">
                    No FQDNs configured yet. Application remains accessible via direct NodePort / IP:Port.
                  </div>
                )}
              </div>

              {/* Ingress Options */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div>
                  <label className="text-xs font-medium text-slate-300">
                    Ingress Class Name (optional)
                  </label>
                  <div className="mt-1">
                    <Input
                      disabled={!isAdmin}
                      placeholder="e.g. nginx, traefik (blank = cluster default)"
                      value={ingressClassName}
                      onChange={(e) => setIngressClassName(e.target.value)}
                    />
                  </div>
                </div>

                <div>
                  <label className="text-xs font-medium text-slate-300">
                    cert-manager ClusterIssuer (optional)
                  </label>
                  <div className="mt-1">
                    <Input
                      disabled={!isAdmin}
                      placeholder="e.g. letsencrypt-prod"
                      value={certIssuer}
                      onChange={(e) => setCertIssuer(e.target.value)}
                    />
                  </div>
                </div>
              </div>

              <div className="space-y-2.5 pt-1">
                <label className="flex items-center space-x-2.5 cursor-pointer select-none">
                  <input
                    type="checkbox"
                    disabled={!isAdmin}
                    checked={standardHTTPSPort}
                    onChange={(e) => setStandardHTTPSPort(e.target.checked)}
                    className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-sky-500 focus:ring-sky-500"
                  />
                  <span className="text-xs text-slate-300">
                    Bind standard ports 80 / 443 on Nginx Proxy alongside 8000 / 8443
                  </span>
                </label>

                <label className="flex items-center space-x-2.5 cursor-pointer select-none">
                  <input
                    type="checkbox"
                    disabled={!isAdmin}
                    checked={restartDeployments}
                    onChange={(e) => setRestartDeployments(e.target.checked)}
                    className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-sky-500 focus:ring-sky-500"
                  />
                  <span className="text-xs text-slate-300">
                    Automatically rollout-restart Kubernetes workloads (<code className="text-slate-400">proxy</code>, <code className="text-slate-400">frontend</code>, <code className="text-slate-400">backend</code>) on save
                  </span>
                </label>
              </div>
            </CardContent>
          </Card>

          {/* TLS Certificate Mode Card */}
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <KeyRound className="h-5 w-5 text-emerald-400" />
                <span>TLS / SSL Certificate</span>
              </CardTitle>
              <CardDescription>
                Kraken automatically generates a multi-SAN X.509 certificate valid for all listed DNS names and stores it in <code className="text-slate-300">Secret/kraken-tls-secret</code>, or you can supply your corporate certificate.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <button
                  type="button"
                  disabled={!isAdmin}
                  onClick={() => setTlsMode("auto_self_signed")}
                  className={`rounded-xl border p-3.5 text-left transition-all ${
                    tlsMode === "auto_self_signed"
                      ? "border-sky-500/60 bg-sky-500/10 text-white"
                      : "border-slate-800 bg-slate-950/40 text-slate-400 hover:border-slate-700"
                  }`}
                >
                  <div className="text-xs font-semibold">Auto-Generate Multi-SAN Certificate</div>
                  <div className="text-[11px] text-slate-400 mt-1">
                    Automatically covers every configured FQDN + internal cluster SANs.
                  </div>
                </button>

                <button
                  type="button"
                  disabled={!isAdmin}
                  onClick={() => setTlsMode("custom")}
                  className={`rounded-xl border p-3.5 text-left transition-all ${
                    tlsMode === "custom"
                      ? "border-sky-500/60 bg-sky-500/10 text-white"
                      : "border-slate-800 bg-slate-950/40 text-slate-400 hover:border-slate-700"
                  }`}
                >
                  <div className="text-xs font-semibold">Custom PEM Certificate &amp; Key</div>
                  <div className="text-[11px] text-slate-400 mt-1">
                    Upload or paste a corporate CA-signed certificate and private key.
                  </div>
                </button>
              </div>

              {tlsMode === "custom" && (
                <div className="grid grid-cols-1 gap-4 pt-2">
                  <div>
                    <label className="text-xs font-medium text-slate-300">Certificate PEM (fullchain)</label>
                    <textarea
                      disabled={!isAdmin}
                      rows={5}
                      value={customCertPEM}
                      onChange={(e) => setCustomCertPEM(e.target.value)}
                      placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                      className="mt-1 w-full rounded-lg border border-slate-800 bg-slate-950 px-3 py-2 text-xs font-mono text-slate-200 focus:border-sky-500 focus:outline-none"
                    />
                  </div>
                  <div>
                    <label className="text-xs font-medium text-slate-300">Private Key PEM</label>
                    <textarea
                      disabled={!isAdmin}
                      rows={5}
                      value={customKeyPEM}
                      onChange={(e) => setCustomKeyPEM(e.target.value)}
                      placeholder="-----BEGIN PRIVATE KEY-----&#10;...&#10;-----END PRIVATE KEY-----"
                      className="mt-1 w-full rounded-lg border border-slate-800 bg-slate-950 px-3 py-2 text-xs font-mono text-slate-200 focus:border-sky-500 focus:outline-none"
                    />
                  </div>
                </div>
              )}

              {isAdmin && (
                <div className="flex flex-wrap items-center justify-end gap-3 pt-2 border-t border-slate-800/80">
                  <Button
                    type="submit"
                    isLoading={isSaving}
                    className="font-semibold"
                  >
                    <ShieldCheck className="h-4 w-4 mr-2" />
                    Save &amp; Apply Configuration
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        {/* Right Column: Active Certificate & Sync Status */}
        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Active Certificate Details</CardTitle>
              <CardDescription>
                Verified Subject Alternative Names &amp; fingerprint
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {config?.cert_fingerprint ? (
                <>
                  <div className="space-y-1">
                    <div className="text-[11px] uppercase tracking-wider text-slate-500">
                      SHA-256 Fingerprint
                    </div>
                    <div className="text-[11px] font-mono text-slate-300 break-all rounded-lg bg-slate-950/70 border border-slate-800 p-2">
                      {config.cert_fingerprint}
                    </div>
                  </div>

                  {config.cert_expires_at && (
                    <div className="space-y-1">
                      <div className="text-[11px] uppercase tracking-wider text-slate-500">
                        Expires At
                      </div>
                      <div className="text-xs text-slate-200 font-mono">
                        {new Date(config.cert_expires_at).toLocaleString()}
                      </div>
                    </div>
                  )}

                  {config.cert_sans && config.cert_sans.length > 0 && (
                    <div className="space-y-1.5">
                      <div className="text-[11px] uppercase tracking-wider text-slate-500">
                        Embedded SANs ({config.cert_sans.length})
                      </div>
                      <div className="flex flex-wrap gap-1.5">
                        {config.cert_sans.map((san) => (
                          <Badge key={san} variant="outline" className="font-mono text-[10px]">
                            {san}
                          </Badge>
                        ))}
                      </div>
                    </div>
                  )}

                  <div className="flex flex-col gap-2 pt-2">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={handleDownloadCert}
                      className="w-full"
                    >
                      <Download className="h-3.5 w-3.5 mr-2" />
                      Download cert.pem
                    </Button>

                    {isAdmin && (
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        isLoading={isRegenerating}
                        onClick={handleRegenerateCert}
                        className="w-full"
                      >
                        <RefreshCw className="h-3.5 w-3.5 mr-2" />
                        Regenerate Certificate
                      </Button>
                    )}
                  </div>
                </>
              ) : (
                <p className="text-xs text-slate-400">
                  No certificate stored in database yet. Save the configuration to generate and sync <code className="text-slate-300">kraken-tls-secret</code>.
                </p>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Kubernetes Sync Status</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-xs">
              <div className="flex items-center justify-between">
                <span className="text-slate-400">Last Status</span>
                <Badge
                  variant={
                    config?.last_apply_status === "success"
                      ? "running"
                      : config?.last_apply_status === "error"
                      ? "error"
                      : "outline"
                  }
                >
                  {config?.last_apply_status || "Pending"}
                </Badge>
              </div>

              {config?.last_applied_at && (
                <div className="flex items-center justify-between">
                  <span className="text-slate-400">Last Applied</span>
                  <span className="font-mono text-slate-300">
                    {new Date(config.last_applied_at).toLocaleTimeString()}
                  </span>
                </div>
              )}

              {config?.updated_by && (
                <div className="flex items-center justify-between">
                  <span className="text-slate-400">Updated By</span>
                  <span className="font-mono text-slate-300">{config.updated_by}</span>
                </div>
              )}

              {config?.last_apply_message && (
                <div className="rounded-lg border border-slate-800 bg-slate-950/70 p-2.5 text-[11px] text-slate-300 leading-relaxed">
                  {config.last_apply_message}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </form>
      )}
    </div>
  );
}
