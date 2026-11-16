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
} from "lucide-react";
import { useAuth } from "@/context/AuthContext";
import {
  getNetworkConfig,
  updateNetworkConfig,
  regenerateNetworkCertificate,
} from "@/lib/api";
import { NetworkConfigResponse } from "@/types";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";

export default function DomainIngressSettingsPage() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";

  const [config, setConfig] = useState<NetworkConfigResponse | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isRegenerating, setIsRegenerating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

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
    } catch (err: any) {
      setError(err?.message || "Failed to load domain and ingress configuration.");
    } finally {
      setIsLoading(false);
    }
  }, []);

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
    </div>
  );
}
