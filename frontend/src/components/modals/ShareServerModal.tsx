"use client";

import React, { useEffect, useMemo, useState } from "react";
import { Loader2, Search, Users } from "lucide-react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { api, getServerAccess, updateServerAccess } from "@/lib/api";
import type { DockerServer, User } from "@/types";

interface ShareServerModalProps {
  isOpen: boolean;
  onClose: () => void;
  server: DockerServer | null;
  onSaved?: () => void;
}

const ROLE_STYLES: Record<string, string> = {
  admin: "bg-rose-500/10 text-rose-300 border-rose-500/30",
  operator: "bg-amber-500/10 text-amber-300 border-amber-500/30",
  viewer: "bg-sky-500/10 text-sky-300 border-sky-500/30",
};

export function ShareServerModal({ isOpen, onClose, server, onSaved }: ShareServerModalProps) {
  const [users, setUsers] = useState<User[]>([]);
  const [ownerId, setOwnerId] = useState<number | null>(null);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [search, setSearch] = useState("");
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!isOpen || !server) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    setSearch("");
    Promise.all([api.get<User[]>("/api/auth/users"), getServerAccess(server.id)])
      .then(([allUsers, access]) => {
        if (cancelled) return;
        setUsers(allUsers.filter((u) => u.status === "active" || !u.status));
        setOwnerId(access.owner_id);
        setSelected(new Set(access.user_ids ?? []));
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Falha ao carregar usuários");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [isOpen, server]);

  const visibleUsers = useMemo(() => {
    const q = search.trim().toLowerCase();
    return users
      .filter((u) => u.id !== ownerId)
      .filter((u) => !q || u.username.toLowerCase().includes(q) || u.email?.toLowerCase().includes(q));
  }, [users, ownerId, search]);

  const toggle = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const handleSave = async () => {
    if (!server) return;
    setSaving(true);
    setError(null);
    try {
      await updateServerAccess(server.id, Array.from(selected));
      onSaved?.();
      onClose();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Falha ao salvar compartilhamento");
    } finally {
      setSaving(false);
    }
  };

  const owner = users.find((u) => u.id === ownerId);

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={server ? `Compartilhar host: ${server.name}` : "Compartilhar host"}
      description="Usuários selecionados passam a ver e monitorar este host. As permissões de cada um continuam limitadas pelo seu papel (viewer só leitura, operator pode operar). Credenciais e detalhes de conexão não são expostos."
      maxWidth="lg"
    >
      <div className="space-y-4">
        {owner && (
          <p className="text-xs text-slate-400">
            Proprietário: <span className="font-medium text-slate-200">{owner.username}</span>
          </p>
        )}
        <div className="relative">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Buscar usuário..."
            className="pl-9"
          />
        </div>

        <div className="max-h-80 overflow-y-auto rounded-lg border border-slate-800 divide-y divide-slate-800">
          {loading ? (
            <div className="flex items-center justify-center gap-2 py-10 text-sm text-slate-400">
              <Loader2 className="h-4 w-4 animate-spin" /> Carregando...
            </div>
          ) : visibleUsers.length === 0 ? (
            <div className="flex flex-col items-center gap-2 py-10 text-sm text-slate-500">
              <Users className="h-5 w-5" /> Nenhum usuário disponível.
            </div>
          ) : (
            visibleUsers.map((u) => (
              <label
                key={u.id}
                className="flex cursor-pointer items-center gap-3 px-3 py-2.5 hover:bg-slate-800/40"
              >
                <input
                  type="checkbox"
                  checked={selected.has(u.id)}
                  onChange={() => toggle(u.id)}
                  className="h-4 w-4 rounded border-slate-600 bg-slate-900 accent-sky-500"
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-white">{u.username}</p>
                  {u.email && <p className="truncate text-xs text-slate-500">{u.email}</p>}
                </div>
                <span
                  className={`rounded-md border px-1.5 py-0.5 text-[10px] font-semibold uppercase ${
                    ROLE_STYLES[u.role] ?? "bg-slate-800 text-slate-300 border-slate-700"
                  }`}
                >
                  {u.role}
                </span>
              </label>
            ))
          )}
        </div>

        {error && (
          <div className="rounded-lg border border-rose-500/30 bg-rose-500/10 p-3 text-sm text-rose-300">{error}</div>
        )}

        <div className="flex items-center justify-between">
          <span className="text-xs text-slate-500">{selected.size} usuário(s) com acesso</span>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={onClose} disabled={saving}>
              Cancelar
            </Button>
            <Button onClick={handleSave} isLoading={saving} disabled={loading}>
              Salvar
            </Button>
          </div>
        </div>
      </div>
    </Modal>
  );
}
