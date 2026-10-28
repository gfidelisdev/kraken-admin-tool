"use client";

import React, { useState } from "react";
import { Wand2, CheckCircle2, AlertTriangle, XCircle, Loader2, ShieldCheck } from "lucide-react";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { provisionHostKey, ApiError } from "@/lib/api";
import { ProvisionKeyResult, ProvisionStep } from "@/types";

interface ProvisionKeyPanelProps {
  serverHost: string;
  serverPort: string;
  defaultUser: string;
  /** Called with the generated private key once the remote host is fully configured. */
  onProvisioned: (privateKey: string, sshUser: string) => void;
}

function StepIcon({ status }: { status: ProvisionStep["status"] }) {
  if (status === "ok") return <CheckCircle2 className="h-3.5 w-3.5 text-emerald-400 shrink-0" />;
  if (status === "skipped") return <AlertTriangle className="h-3.5 w-3.5 text-amber-400 shrink-0" />;
  if (status === "sudo_required") return <ShieldCheck className="h-3.5 w-3.5 text-amber-400 shrink-0" />;
  return <XCircle className="h-3.5 w-3.5 text-red-400 shrink-0" />;
}

export function ProvisionKeyPanel({ serverHost, serverPort, defaultUser, onProvisioned }: ProvisionKeyPanelProps) {
  const [open, setOpen] = useState(false);
  const [user, setUser] = useState(defaultUser || "root");
  const [password, setPassword] = useState("");
  const [sudoPassword, setSudoPassword] = useState("");
  const [needsSudo, setNeedsSudo] = useState(false);
  const [sudoReason, setSudoReason] = useState("");
  const [pendingKey, setPendingKey] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<ProvisionKeyResult | null>(null);

  const reset = () => {
    setPassword("");
    setSudoPassword("");
    setNeedsSudo(false);
    setPendingKey(undefined);
  };

  const run = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await provisionHostKey({
        server_host: serverHost.trim(),
        server_port: serverPort.trim() || "22",
        ssh_user: user.trim() || "root",
        ssh_password: password,
        sudo_password: needsSudo ? sudoPassword : undefined,
        private_key: pendingKey,
      });
      setResult(res);
      if (res.success && res.private_key) {
        onProvisioned(res.private_key, user.trim() || "root");
        reset(); // credentials are never kept after use
      }
    } catch (e) {
      if (e instanceof ApiError && e.data && typeof e.data === "object") {
        const data = e.data as ProvisionKeyResult;
        setResult(data);
        if (data.code === "sudo_required" || data.code === "invalid_sudo_password") {
          setNeedsSudo(true);
          setPendingKey(data.private_key || pendingKey);
          setSudoReason(
            data.code === "invalid_sudo_password"
              ? "A senha do sudo está incorreta. Tente novamente."
              : (data.steps.find((s) => s.status === "sudo_required")?.message ?? "")
          );
          if (data.code === "invalid_sudo_password") setSudoPassword("");
          return;
        }
        setError(data.message || e.message);
      } else {
        setError(e instanceof Error ? e.message : "Falha ao configurar o acesso por chave.");
      }
    } finally {
      setBusy(false);
    }
  };

  const canRun = !!serverHost.trim() && !!user.trim() && !!password && (!needsSudo || !!sudoPassword) && !busy;

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex w-full items-center gap-2 rounded-lg border border-sky-500/30 bg-sky-500/10 px-3 py-2 text-left text-[11px] text-sky-300 hover:bg-sky-500/15 transition-colors"
      >
        <Wand2 className="h-4 w-4 shrink-0" />
        <span>
          <strong>Configurar automaticamente.</strong> Informe usuário e senha do host uma única vez e o Kraken gera a
          chave e faz toda a configuração remota.
        </span>
      </button>
    );
  }

  return (
    <div className="space-y-2.5 rounded-lg border border-sky-500/30 bg-slate-950/70 p-3">
      <div className="flex items-center gap-2 text-xs font-semibold text-sky-300">
        <Wand2 className="h-4 w-4" /> Configuração automática de acesso por chave
      </div>
      <p className="text-[11px] text-slate-400">
        As credenciais abaixo são usadas apenas agora para acessar o host e instalar a chave. Elas <strong>não são
        armazenadas</strong>.
      </p>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
        <div>
          <label className="text-[11px] font-medium text-slate-400">Usuário do host</label>
          <Input value={user} onChange={(e) => setUser(e.target.value)} autoComplete="off" />
        </div>
        <div>
          <label className="text-[11px] font-medium text-slate-400">Senha do usuário</label>
          <Input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
          />
        </div>
      </div>

      {needsSudo && (
        <div className="rounded-md border border-amber-500/30 bg-amber-500/10 p-2.5 space-y-1.5">
          <div className="flex items-start gap-1.5 text-[11px] text-amber-300">
            <ShieldCheck className="h-3.5 w-3.5 mt-0.5 shrink-0" />
            <span>
              Para concluir é necessário executar comandos com <code>sudo</code> no host.
              {sudoReason ? ` ${sudoReason}` : ""}
            </span>
          </div>
          <label className="text-[11px] font-medium text-slate-400">Senha do sudo</label>
          <Input
            type="password"
            value={sudoPassword}
            onChange={(e) => setSudoPassword(e.target.value)}
            autoComplete="new-password"
          />
        </div>
      )}

      {error && <div className="rounded-md border border-red-500/30 bg-red-500/10 p-2 text-[11px] text-red-300">{error}</div>}

      {result && result.steps.length > 0 && (
        <ul className="space-y-1 rounded-md border border-slate-800 bg-slate-900/60 p-2">
          {result.steps.map((s, i) => (
            <li key={i} className="flex items-start gap-1.5 text-[11px] text-slate-300">
              <StepIcon status={s.status} />
              <span>
                {s.name}
                {s.message ? <span className="text-slate-500"> — {s.message}</span> : null}
              </span>
            </li>
          ))}
        </ul>
      )}

      {result?.success && (
        <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 p-2 text-[11px] text-emerald-300">
          Host configurado. A chave privada foi preenchida acima; clique em &quot;Testar&quot; e depois salve o host.
        </div>
      )}

      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={() => { reset(); setOpen(false); }} disabled={busy}>
          Fechar
        </Button>
        <Button type="button" size="sm" onClick={run} disabled={!canRun}>
          {busy ? (
            <>
              <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> Configurando...
            </>
          ) : needsSudo ? (
            "Continuar com sudo"
          ) : (
            "Configurar host"
          )}
        </Button>
      </div>
    </div>
  );
}
