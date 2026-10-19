"use client";

import React, { useState, useEffect, useMemo, useRef } from "react";
import {
  Server,
  Plus,
  Search,
  CheckCircle2,
  AlertCircle,
  Radio,
  Trash2,
  Edit,
  Shield,
  Box,
  Disc,
  Lock,
  Unlock,
  Check,
  RefreshCw,
  Key,
  HelpCircle,
  BookOpen,
  Terminal,
  UserCheck,
  Copy,
  ChevronDown,
  ChevronRight,
  XCircle,
} from "lucide-react";
import { useServer } from "@/context/ServerContext";
import { api, testServerStream } from "@/lib/api";
import {
  DockerServer,
  CreateServerPayload,
  TestServerPayload,
  ServerDiagnosticLog,
} from "@/types";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Modal } from "@/components/ui/Modal";
import { ConfirmActionModal } from "@/components/modals/ConfirmActionModal";
import {
  HostSetupGuideModal,
  HostGuideTopic,
} from "@/components/modals/HostSetupGuideModal";
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
  TableEmpty,
} from "@/components/ui/Table";

type ConnectionType = "ssh" | "tcp" | "tls" | "local";

const CONNECTION_PRESETS: {
  id: HostGuideTopic;
  type: ConnectionType;
  label: string;
  sublabel: string;
  icon: React.ElementType;
}[] = [
  {
    id: "ssh-password",
    type: "ssh",
    label: "SSH (Usuário e Senha)",
    sublabel: "Porta 22 padrão",
    icon: UserCheck,
  },
  {
    id: "ssh-key",
    type: "ssh",
    label: "SSH (Chave / Cert)",
    sublabel: "Chave privada PEM / OpenSSH",
    icon: Key,
  },
  {
    id: "remote-tcp",
    type: "tcp",
    label: "Remote TCP (2375)",
    sublabel: "Rede local ou VPN",
    icon: Server,
  },
  {
    id: "secure-tls",
    type: "tls",
    label: "Secure TLS (2376)",
    sublabel: "mTLS com certificados",
    icon: Shield,
  },
  {
    id: "local-socket",
    type: "local",
    label: "Socket Local",
    sublabel: "/var/run/docker.sock",
    icon: Terminal,
  },
];

export default function ServersPage() {
  const { servers, activeServer, setActiveServer, refreshServers, isLoading } = useServer();
  const [search, setSearch] = useState("");
  const [isRefreshing, setIsRefreshing] = useState(false);

  // Modal State
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingServer, setEditingServer] = useState<DockerServer | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<DockerServer | null>(null);

  // Form State
  const [connectionType, setConnectionType] = useState<ConnectionType>("ssh");
  const [name, setName] = useState("");
  const [serverHost, setServerHost] = useState("");
  const [serverPort, setServerPort] = useState("22");
  const [localSocketPath, setLocalSocketPath] = useState("/var/run/docker.sock");
  const [isManualUrl, setIsManualUrl] = useState(false);
  const [hostUrl, setHostUrl] = useState("");

  // TLS Options
  const [tlsVerify, setTlsVerify] = useState(false);
  const [tlsCaCert, setTlsCaCert] = useState("");
  const [tlsCert, setTlsCert] = useState("");
  const [tlsKey, setTlsKey] = useState("");
  const [showCertFields, setShowCertFields] = useState(false);

  // SSH Options
  const [sshUser, setSshUser] = useState("root");
  const [sshAuthMethod, setSshAuthMethod] = useState<"key" | "password">("password");
  const [sshPassword, setSshPassword] = useState("");
  const [sshPrivateKey, setSshPrivateKey] = useState("");
  const [sshKeyPassphrase, setSshKeyPassphrase] = useState("");

  const [isDefault, setIsDefault] = useState(false);

  // Diagnostic Console State
  const [diagnosticLogs, setDiagnosticLogs] = useState<ServerDiagnosticLog[]>([]);
  const [isTesting, setIsTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const consoleBottomRef = useRef<HTMLDivElement | null>(null);

  // Guide Modal State
  const [isGuideOpen, setIsGuideOpen] = useState(false);
  const [guideTopic, setGuideTopic] = useState<HostGuideTopic>("ssh-password");

  // Row Action Loading State
  const [actionLoadingId, setActionLoadingId] = useState<number | null>(null);

  // Auto-scroll console
  useEffect(() => {
    if (diagnosticLogs.length > 0 && consoleBottomRef.current) {
      consoleBottomRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [diagnosticLogs]);

  // Compute URL based on fields
  const computedUrl = useMemo(() => {
    if (isManualUrl && hostUrl.trim()) {
      return hostUrl.trim();
    }
    const cleanHost = serverHost.trim();
    if (connectionType === "ssh") {
      if (!cleanHost) return "";
      const port = serverPort.trim() || "22";
      const user = sshUser.trim() || "root";
      return `ssh://${user}@${cleanHost}:${port}`;
    }
    if (connectionType === "local") {
      return `unix://${localSocketPath.trim() || "/var/run/docker.sock"}`;
    }
    if (connectionType === "tcp") {
      if (!cleanHost) return "";
      const port = serverPort.trim() || "2375";
      return `tcp://${cleanHost}:${port}`;
    }
    if (connectionType === "tls") {
      if (!cleanHost) return "";
      const port = serverPort.trim() || "2376";
      return `tcp://${cleanHost}:${port}`;
    }
    return hostUrl.trim();
  }, [connectionType, serverHost, serverPort, sshUser, localSocketPath, isManualUrl, hostUrl]);

  const openGuide = (topic: HostGuideTopic) => {
    setGuideTopic(topic);
    setIsGuideOpen(true);
  };

  const handleManualRefresh = async () => {
    setIsRefreshing(true);
    await refreshServers();
    setIsRefreshing(false);
  };

  const handleOpenAddModal = () => {
    setEditingServer(null);
    setName("");
    setConnectionType("ssh");
    setServerHost("");
    setServerPort("22");
    setLocalSocketPath("/var/run/docker.sock");
    setHostUrl("");
    setIsManualUrl(false);

    setTlsVerify(false);
    setTlsCaCert("");
    setTlsCert("");
    setTlsKey("");
    setShowCertFields(false);

    setSshUser("root");
    setSshAuthMethod("password");
    setSshPassword("");
    setSshPrivateKey("");
    setSshKeyPassphrase("");

    setIsDefault(false);
    setDiagnosticLogs([]);
    setTestResult(null);
    setFormError(null);
    setIsModalOpen(true);
  };

  const handleOpenEditModal = (server: DockerServer) => {
    setEditingServer(server);
    setName(server.name);
    setHostUrl(server.host_url);
    setIsDefault(server.is_default);
    setTlsVerify(server.tls_verify);
    setTlsCaCert(server.tls_ca_cert || "");
    setTlsCert(server.tls_cert || "");
    setTlsKey("");
    setShowCertFields(server.tls_verify);

    setSshUser(server.ssh_user || "root");
    setSshAuthMethod((server.ssh_auth_method as "key" | "password") || "password");
    setSshPassword("");
    setSshPrivateKey("");
    setSshKeyPassphrase("");

    // Infer connection type and decompose URL
    const rawUrl = server.host_url || "";
    if (rawUrl.startsWith("ssh://")) {
      setConnectionType("ssh");
      setIsManualUrl(false);
      try {
        const withoutScheme = rawUrl.replace(/^ssh:\/\//, "");
        let user = server.ssh_user || "";
        let hostPort = withoutScheme;
        if (withoutScheme.includes("@")) {
          const parts = withoutScheme.split("@");
          user = user || parts[0];
          hostPort = parts[1];
        }
        let host = hostPort;
        let port = "22";
        if (hostPort.includes(":")) {
          const parts = hostPort.split(":");
          host = parts[0];
          port = parts[1].split("/")[0];
        }
        setServerHost(host);
        setServerPort(port || "22");
        setSshUser(user || "root");
      } catch {
        setIsManualUrl(true);
      }
    } else if (rawUrl.startsWith("unix://")) {
      setConnectionType("local");
      setIsManualUrl(false);
      setLocalSocketPath(rawUrl.replace(/^unix:\/\//, "") || "/var/run/docker.sock");
    } else if (rawUrl.startsWith("tcp://")) {
      if (server.tls_verify) {
        setConnectionType("tls");
      } else {
        setConnectionType("tcp");
      }
      setIsManualUrl(false);
      try {
        const withoutScheme = rawUrl.replace(/^tcp:\/\//, "");
        let host = withoutScheme;
        let port = server.tls_verify ? "2376" : "2375";
        if (withoutScheme.includes(":")) {
          const parts = withoutScheme.split(":");
          host = parts[0];
          port = parts[1];
        }
        setServerHost(host);
        setServerPort(port);
      } catch {
        setIsManualUrl(true);
      }
    } else {
      setIsManualUrl(true);
    }

    setDiagnosticLogs([]);
    setTestResult(null);
    setFormError(null);
    setIsModalOpen(true);
  };

  const handleSelectGuidePreset = (topic: HostGuideTopic) => {
    switch (topic) {
      case "local-socket":
        setConnectionType("local");
        setLocalSocketPath("/var/run/docker.sock");
        setTlsVerify(false);
        setShowCertFields(false);
        setIsManualUrl(false);
        break;
      case "remote-tcp":
        setConnectionType("tcp");
        setServerPort("2375");
        setTlsVerify(false);
        setShowCertFields(false);
        setIsManualUrl(false);
        break;
      case "secure-tls":
        setConnectionType("tls");
        setServerPort("2376");
        setTlsVerify(true);
        setShowCertFields(true);
        setIsManualUrl(false);
        break;
      case "ssh-key":
        setConnectionType("ssh");
        setServerPort("22");
        setSshAuthMethod("key");
        if (!sshUser) setSshUser("root");
        setTlsVerify(false);
        setShowCertFields(false);
        setIsManualUrl(false);
        break;
      case "ssh-password":
        setConnectionType("ssh");
        setServerPort("22");
        setSshAuthMethod("password");
        if (!sshUser) setSshUser("root");
        setTlsVerify(false);
        setShowCertFields(false);
        setIsManualUrl(false);
        break;
    }
  };

  const handleTestConnection = async () => {
    const finalUrl = computedUrl;
    if (connectionType !== "local" && !serverHost.trim() && !hostUrl.trim()) {
      setFormError("Informe o nome do servidor ou endereço IP para testar a conexão");
      return;
    }

    setIsTesting(true);
    setTestResult(null);
    setFormError(null);
    setDiagnosticLogs([
      {
        level: "info",
        message: `Iniciando verificação de conexão para ${serverHost.trim() || finalUrl}...`,
        timestamp: new Date().toLocaleTimeString(),
      },
    ]);

    try {
      const payload: TestServerPayload = {
        host_url: finalUrl || undefined,
        server_host: serverHost.trim() || undefined,
        server_port: serverPort.trim() || undefined,
        tls_verify: connectionType === "tls" ? true : tlsVerify,
        tls_ca_cert: tlsCaCert.trim() || undefined,
        tls_cert: tlsCert.trim() || undefined,
        tls_key: tlsKey.trim() || undefined,
        ...(connectionType === "ssh" || finalUrl.startsWith("ssh://")
          ? {
              ssh_user: sshUser.trim() || undefined,
              ssh_auth_method: sshAuthMethod,
              ssh_password: sshAuthMethod === "password" ? sshPassword || undefined : undefined,
              ssh_private_key: sshAuthMethod === "key" ? sshPrivateKey.trim() || undefined : undefined,
              ssh_key_passphrase: sshAuthMethod === "key" ? sshKeyPassphrase || undefined : undefined,
            }
          : {}),
      };

      const res = await testServerStream(payload, (newLog) => {
        setDiagnosticLogs((prev) => [...prev, newLog]);
      });

      if (res.status === "CONNECTED") {
        setTestResult({
          success: true,
          message: `Conectado com sucesso! Docker Engine ${res.version || "respondendo"}`,
        });
      } else {
        setTestResult({
          success: false,
          message: res.error || res.message || "Falha na conexão com o daemon Docker",
        });
      }
    } catch (err: any) {
      const errMsg = err?.message || "Falha ao estabelecer conexão com o daemon Docker";
      setDiagnosticLogs((prev) => [
        ...prev,
        {
          level: "error",
          message: errMsg,
          timestamp: new Date().toLocaleTimeString(),
        },
      ]);
      setTestResult({
        success: false,
        message: errMsg,
      });
    } finally {
      setIsTesting(false);
    }
  };

  const handleSaveServer = async (e: React.FormEvent) => {
    e.preventDefault();
    const finalUrl = computedUrl;
    if (!name.trim()) {
      setFormError("O nome amigável do servidor é obrigatório");
      return;
    }
    if (!finalUrl.trim()) {
      setFormError("Endereço IP ou hostname do servidor é obrigatório");
      return;
    }

    setIsSaving(true);
    setFormError(null);

    setDiagnosticLogs((prev) => [
      ...prev,
      {
        level: "info",
        message: `Salvando e validando host '${name.trim()}' (${finalUrl})...`,
        timestamp: new Date().toLocaleTimeString(),
      },
    ]);

    try {
      const payload: CreateServerPayload = {
        name: name.trim(),
        host_url: finalUrl.trim(),
        server_host: serverHost.trim() || undefined,
        server_port: serverPort.trim() || undefined,
        tls_verify: connectionType === "tls" ? true : tlsVerify,
        tls_ca_cert: tlsCaCert.trim() || undefined,
        tls_cert: tlsCert.trim() || undefined,
        tls_key: tlsKey.trim() || undefined,
        is_default: isDefault,
        ...(connectionType === "ssh" || finalUrl.startsWith("ssh://")
          ? {
              ssh_user: sshUser.trim() || undefined,
              ssh_auth_method: sshAuthMethod,
              ssh_password: sshAuthMethod === "password" ? sshPassword || undefined : undefined,
              ssh_private_key: sshAuthMethod === "key" ? sshPrivateKey.trim() || undefined : undefined,
              ssh_key_passphrase: sshAuthMethod === "key" ? sshKeyPassphrase || undefined : undefined,
            }
          : {}),
      };

      if (editingServer) {
        await api.put(`/api/docker/servers/${editingServer.id}`, payload);
      } else {
        await api.post("/api/docker/servers", payload);
      }

      await refreshServers();
      setIsModalOpen(false);
    } catch (err: any) {
      const errMsg = err?.message || "Falha ao salvar servidor";
      setFormError(errMsg);
      setDiagnosticLogs((prev) => [
        ...prev,
        {
          level: "error",
          message: `Erro ao salvar: ${errMsg}`,
          timestamp: new Date().toLocaleTimeString(),
        },
      ]);
    } finally {
      setIsSaving(false);
    }
  };

  const handleSetDefault = async (id: number) => {
    setActionLoadingId(id);
    try {
      await api.post(`/api/docker/servers/${id}/default`);
      await refreshServers();
    } catch (err: any) {
      alert(err?.message || "Falha ao definir servidor padrão");
    } finally {
      setActionLoadingId(null);
    }
  };

  const handleDeleteServer = async () => {
    if (!deleteTarget) return;
    try {
      await api.delete(`/api/docker/servers/${deleteTarget.id}`);
      setDeleteTarget(null);
      await refreshServers();
    } catch (err: any) {
      alert(err?.message || "Falha ao excluir servidor");
    }
  };

  // Filter servers
  const filteredServers = useMemo(() => {
    return servers.filter((s) => {
      const query = search.toLowerCase().trim();
      if (!query) return true;
      return (
        s.name.toLowerCase().includes(query) ||
        s.host_url.toLowerCase().includes(query) ||
        s.status.toLowerCase().includes(query)
      );
    });
  }, [servers, search]);

  const onlineCount = servers.filter((s) => s.status === "CONNECTED").length;
  const offlineCount = servers.filter((s) => s.status === "UNREACHABLE").length;

  return (
    <div className="space-y-6">
      {/* Top Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white flex items-center gap-2.5">
            <Server className="h-6 w-6 text-sky-400" />
            Servidores & Hosts Docker Remotos
          </h1>
          <p className="text-xs text-slate-400 mt-1">
            Gerencie múltiplos nós Docker e clusters remotos via SSH, TCP ou TLS.
          </p>
        </div>

        <div className="flex items-center space-x-2">
          <Button
            variant="outline"
            size="sm"
            onClick={handleManualRefresh}
            disabled={isRefreshing || isLoading}
          >
            <RefreshCw
              className={`mr-1.5 h-3.5 w-3.5 ${isRefreshing ? "animate-spin" : ""}`}
            />
            Atualizar Status
          </Button>
          <Button variant="docker" size="sm" onClick={handleOpenAddModal}>
            <Plus className="mr-1.5 h-4 w-4" />
            Adicionar Host Remoto
          </Button>
        </div>
      </div>

      {/* Metric Cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className="rounded-xl border border-slate-800 bg-slate-900/60 p-4">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-slate-400">Total de Hosts</span>
            <Server className="h-4 w-4 text-sky-400" />
          </div>
          <p className="mt-2 text-2xl font-bold text-white">{servers.length}</p>
          <p className="text-[11px] text-slate-500 mt-1">Nós Docker locais e remotos</p>
        </div>

        <div className="rounded-xl border border-slate-800 bg-slate-900/60 p-4">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-slate-400">Conectados & Online</span>
            <CheckCircle2 className="h-4 w-4 text-emerald-400" />
          </div>
          <p className="mt-2 text-2xl font-bold text-emerald-400">{onlineCount}</p>
          <p className="text-[11px] text-slate-500 mt-1">Respondendo ao Ping</p>
        </div>

        <div className="rounded-xl border border-slate-800 bg-slate-900/60 p-4">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-slate-400">Inacessíveis / Offline</span>
            <AlertCircle className="h-4 w-4 text-rose-400" />
          </div>
          <p className="mt-2 text-2xl font-bold text-rose-400">{offlineCount}</p>
          <p className="text-[11px] text-slate-500 mt-1">Verifique credenciais ou firewall</p>
        </div>

        <div className="rounded-xl border border-slate-800 bg-slate-900/60 p-4">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-slate-400">Sessão Ativa no Kraken</span>
            <Radio className="h-4 w-4 text-indigo-400 animate-pulse" />
          </div>
          <p className="mt-2 text-lg font-bold text-white truncate">
            {activeServer?.name || "Nenhum"}
          </p>
          <p className="text-[11px] text-slate-400 font-mono truncate mt-0.5">
            {activeServer?.host_url || "Selecione um servidor"}
          </p>
        </div>
      </div>

      {/* Search */}
      <div className="flex items-center justify-between gap-4">
        <div className="relative max-w-sm flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-500" />
          <Input
            placeholder="Buscar por nome, URL ou status..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-9"
          />
        </div>
      </div>

      {/* Servers Table */}
      <div className="rounded-xl border border-slate-800 bg-slate-900/40 overflow-hidden shadow-sm">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Nome do Host</TableHead>
              <TableHead>URL de Conexão</TableHead>
              <TableHead>Segurança / Protocolo</TableHead>
              <TableHead>Status & Versão</TableHead>
              <TableHead>Cargas de Trabalho</TableHead>
              <TableHead className="text-right">Ações</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filteredServers.length === 0 ? (
              <TableEmpty
                colSpan={6}
                message={
                  search ? "Nenhum servidor corresponde à pesquisa." : "Nenhum servidor Docker registrado."
                }
              />
            ) : (
              filteredServers.map((server) => {
                const isSelected = activeServer?.id === server.id;
                const isOnline = server.status === "CONNECTED";
                const isSsh = server.host_url.toLowerCase().startsWith("ssh://");

                return (
                  <TableRow
                    key={server.id}
                    className={isSelected ? "bg-sky-500/5 hover:bg-sky-500/10" : undefined}
                  >
                    <TableCell>
                      <div className="flex items-center space-x-2">
                        <span className="font-semibold text-white">{server.name}</span>
                        {isSelected && (
                          <span className="inline-flex items-center rounded-md bg-sky-500/20 border border-sky-500/40 px-1.5 py-0.5 text-[10px] font-semibold text-sky-300">
                            Ativo
                          </span>
                        )}
                        {server.is_default && (
                          <span className="inline-flex items-center rounded-md bg-slate-800 border border-slate-700 px-1.5 py-0.5 text-[10px] text-slate-300">
                            Padrão
                          </span>
                        )}
                      </div>
                    </TableCell>

                    <TableCell>
                      <span className="font-mono text-xs text-slate-300 select-all">
                        {server.host_url}
                      </span>
                    </TableCell>

                    <TableCell>
                      {isSsh ? (
                        <span className="inline-flex items-center space-x-1 rounded bg-indigo-500/10 border border-indigo-500/30 px-1.5 py-0.5 text-[11px] text-indigo-300">
                          <Key className="h-3 w-3" />
                          <span>
                            {server.ssh_auth_method === "password"
                              ? "SSH (Senha)"
                              : "SSH (Chave/Cert)"}
                          </span>
                        </span>
                      ) : server.tls_verify ? (
                        <span className="inline-flex items-center space-x-1 rounded bg-amber-500/10 border border-amber-500/30 px-1.5 py-0.5 text-[11px] text-amber-400">
                          <Lock className="h-3 w-3" />
                          <span>TLS Seguro</span>
                        </span>
                      ) : (
                        <span className="inline-flex items-center space-x-1 text-slate-500 text-xs font-mono">
                          <Unlock className="h-3 w-3" />
                          <span>Padrão TCP/Socket</span>
                        </span>
                      )}
                    </TableCell>

                    <TableCell>
                      <div className="flex flex-col space-y-0.5">
                        <div className="flex items-center space-x-1.5">
                          <span
                            className={`h-2 w-2 rounded-full ${
                              isOnline ? "bg-emerald-400" : "bg-rose-400"
                            }`}
                          />
                          <span
                            className={`text-xs font-medium ${
                              isOnline ? "text-emerald-400" : "text-rose-400"
                            }`}
                          >
                            {isOnline ? "Online" : "Inacessível"}
                          </span>
                        </div>
                        {server.version && (
                          <span className="text-[10px] font-mono text-slate-400">
                            Docker {server.version}
                          </span>
                        )}
                        {server.err_msg && (
                          <span
                            className="text-[10px] text-rose-400/80 line-clamp-1 max-w-[200px]"
                            title={server.err_msg}
                          >
                            {server.err_msg}
                          </span>
                        )}
                      </div>
                    </TableCell>

                    <TableCell>
                      <div className="flex items-center space-x-3 text-xs text-slate-300">
                        <span className="flex items-center space-x-1" title="Containers">
                          <Box className="h-3.5 w-3.5 text-sky-400" />
                          <span>{server.containers_count}</span>
                        </span>
                        <span className="flex items-center space-x-1" title="Imagens">
                          <Disc className="h-3.5 w-3.5 text-indigo-400" />
                          <span>{server.images_count}</span>
                        </span>
                      </div>
                    </TableCell>

                    <TableCell className="text-right">
                      <div className="flex items-center justify-end space-x-1.5">
                        {!isSelected ? (
                          <Button
                            variant="outline"
                            size="sm"
                            className="text-xs h-7 px-2"
                            onClick={() => setActiveServer(server)}
                          >
                            Conectar
                          </Button>
                        ) : (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="text-xs h-7 px-2 text-sky-400 cursor-default"
                            disabled
                          >
                            <Check className="h-3.5 w-3.5 mr-1" />
                            Conectado
                          </Button>
                        )}

                        {!server.is_default && (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="text-xs h-7 px-2 text-slate-400 hover:text-white"
                            onClick={() => handleSetDefault(server.id)}
                            disabled={actionLoadingId === server.id}
                            title="Tornar servidor padrão do sistema"
                          >
                            Padrão
                          </Button>
                        )}

                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 w-7 p-0 text-slate-400 hover:text-white"
                          onClick={() => handleOpenEditModal(server)}
                          title="Editar Servidor"
                        >
                          <Edit className="h-3.5 w-3.5" />
                        </Button>

                        <Button
                          variant="ghost"
                          size="sm"
                          className="h-7 w-7 p-0 text-slate-400 hover:text-rose-400"
                          onClick={() => setDeleteTarget(server)}
                          disabled={servers.length <= 1}
                          title={servers.length <= 1 ? "Não é possível excluir o único host" : "Excluir Host"}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </div>

      {/* Add / Edit Docker Server Modal */}
      <Modal
        isOpen={isModalOpen}
        onClose={() => setIsModalOpen(false)}
        title={editingServer ? "Editar Host Docker" : "Adicionar Host Docker Remoto"}
        description="Configure o acesso ao daemon Docker via SSH, TCP ou Socket com verificação em tempo real."
        maxWidth="xl"
      >
        <form onSubmit={handleSaveServer} className="space-y-4">
          {/* Quick Presets with Help Links */}
          <div className="space-y-2">
            <div className="flex items-center justify-between pb-1 border-b border-slate-800/80">
              <span className="text-xs font-medium text-slate-300">Modelos de Conexão Rápida</span>
              <button
                type="button"
                onClick={() => openGuide(guideTopic)}
                className="inline-flex items-center gap-1 text-[11px] font-medium text-sky-400 hover:text-sky-300 hover:underline transition-colors"
              >
                <BookOpen className="h-3.5 w-3.5" />
                Guia passo a passo de configuração
              </button>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-5 gap-1.5">
              {CONNECTION_PRESETS.map((preset) => {
                const Icon = preset.icon;
                const isCurrent =
                  (preset.id === "ssh-password" && connectionType === "ssh" && sshAuthMethod === "password") ||
                  (preset.id === "ssh-key" && connectionType === "ssh" && sshAuthMethod === "key") ||
                  (preset.id === "remote-tcp" && connectionType === "tcp") ||
                  (preset.id === "secure-tls" && connectionType === "tls") ||
                  (preset.id === "local-socket" && connectionType === "local");

                return (
                  <div
                    key={preset.id}
                    className={`flex flex-col justify-between rounded-lg border p-2 transition-all ${
                      isCurrent
                        ? "border-sky-500 bg-sky-500/10 shadow-sm"
                        : "border-slate-800 bg-slate-900/60 hover:border-slate-700"
                    }`}
                  >
                    <button
                      type="button"
                      onClick={() => handleSelectGuidePreset(preset.id)}
                      className="flex flex-col items-start text-left w-full"
                    >
                      <div className="flex items-center gap-1.5 text-sky-400">
                        <Icon className="h-3.5 w-3.5 shrink-0" />
                        <span className="text-[11px] font-semibold text-slate-200 truncate">
                          {preset.label}
                        </span>
                      </div>
                      <span className="text-[10px] text-slate-500 font-mono truncate mt-0.5">
                        {preset.sublabel}
                      </span>
                    </button>
                    <div className="mt-1.5 pt-1 border-t border-slate-800/80 flex items-center justify-end">
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          openGuide(preset.id);
                        }}
                        className="inline-flex items-center gap-0.5 text-[10px] text-slate-400 hover:text-sky-400 transition-colors"
                        title="Ver instruções de configuração"
                      >
                        <HelpCircle className="h-3 w-3" />
                        <span>Ajuda</span>
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          {/* Server Friendly Name */}
          <div>
            <label className="text-xs font-medium text-slate-300">Nome Amigável do Servidor</label>
            <div className="mt-1">
              <Input
                placeholder="ex: AWS Worker Node, Kubernetes Master ou Servidor Local"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
            </div>
          </div>

          {/* Form Fields Based on Connection Type */}
          {connectionType === "ssh" && (
            <div className="space-y-3 rounded-xl border border-sky-500/30 bg-slate-950/70 p-3.5">
              <div className="flex items-center justify-between border-b border-slate-800/80 pb-2">
                <div className="flex items-center space-x-2">
                  <Key className="h-4 w-4 text-sky-400" />
                  <span className="text-xs font-semibold text-slate-200">
                    Acesso Remoto via SSH
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => openGuide(sshAuthMethod === "password" ? "ssh-password" : "ssh-key")}
                  className="inline-flex items-center gap-1 text-[11px] text-sky-400 hover:text-sky-300 hover:underline"
                >
                  <HelpCircle className="h-3 w-3" />
                  Como configurar o SSH no host remoto?
                </button>
              </div>

              {/* Host and Port separate fields */}
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <div className="sm:col-span-2">
                  <label className="text-[11px] font-medium text-slate-400">
                    Nome do Servidor ou Endereço IP
                  </label>
                  <div className="mt-1">
                    <Input
                      placeholder="ex: 192.168.1.100 ou docker.empresa.com"
                      value={serverHost}
                      onChange={(e) => {
                        setServerHost(e.target.value);
                        setTestResult(null);
                      }}
                      required
                    />
                  </div>
                </div>

                <div>
                  <label className="text-[11px] font-medium text-slate-400">Porta SSH</label>
                  <div className="mt-1">
                    <Input
                      type="number"
                      placeholder="22"
                      value={serverPort}
                      onChange={(e) => {
                        setServerPort(e.target.value);
                        setTestResult(null);
                      }}
                    />
                  </div>
                </div>
              </div>

              {/* SSH User & Auth Mode */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <label className="text-[11px] font-medium text-slate-400">Usuário SSH</label>
                  <div className="mt-1">
                    <Input
                      placeholder="ex: root ou ubuntu"
                      value={sshUser}
                      onChange={(e) => {
                        setSshUser(e.target.value);
                        setTestResult(null);
                      }}
                      required
                    />
                  </div>
                </div>

                <div>
                  <label className="text-[11px] font-medium text-slate-400">
                    Método de Autenticação
                  </label>
                  <div className="grid grid-cols-2 gap-1.5 p-1 bg-slate-900 rounded-lg border border-slate-800 mt-1">
                    <button
                      type="button"
                      onClick={() => {
                        setSshAuthMethod("password");
                        setTestResult(null);
                      }}
                      className={`py-1 px-2 rounded text-xs font-medium transition-all ${
                        sshAuthMethod === "password"
                          ? "bg-sky-500/20 text-sky-300 border border-sky-500/40"
                          : "text-slate-400 hover:text-slate-200"
                      }`}
                    >
                      Usuário e Senha
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        setSshAuthMethod("key");
                        setTestResult(null);
                      }}
                      className={`py-1 px-2 rounded text-xs font-medium transition-all ${
                        sshAuthMethod === "key"
                          ? "bg-sky-500/20 text-sky-300 border border-sky-500/40"
                          : "text-slate-400 hover:text-slate-200"
                      }`}
                    >
                      Chave / Cert
                    </button>
                  </div>
                </div>
              </div>

              {/* Password Field */}
              {sshAuthMethod === "password" && (
                <div>
                  <label className="text-[11px] font-medium text-slate-400">Senha SSH</label>
                  <div className="mt-1">
                    <Input
                      type="password"
                      placeholder="Digite a senha de login do usuário SSH"
                      value={sshPassword}
                      onChange={(e) => {
                        setSshPassword(e.target.value);
                        setTestResult(null);
                      }}
                      required
                    />
                  </div>
                </div>
              )}

              {/* Key Fields */}
              {sshAuthMethod === "key" && (
                <div className="space-y-2">
                  <div>
                    <label className="text-[11px] font-medium text-slate-400">
                      Chave Privada SSH (PEM / OpenSSH)
                    </label>
                    <textarea
                      rows={3}
                      placeholder="-----BEGIN OPENSSH PRIVATE KEY----- ... -----END OPENSSH PRIVATE KEY-----"
                      value={sshPrivateKey}
                      onChange={(e) => {
                        setSshPrivateKey(e.target.value);
                        setTestResult(null);
                      }}
                      className="mt-1 w-full rounded-lg border border-slate-800 bg-slate-900 p-2 font-mono text-[11px] text-slate-300 placeholder:text-slate-600 focus:border-sky-500 focus:outline-none"
                    />
                  </div>
                  <div>
                    <label className="text-[11px] font-medium text-slate-400">
                      Passphrase da Chave (Opcional)
                    </label>
                    <div className="mt-1">
                      <Input
                        type="password"
                        placeholder="Deixe em branco se a chave não tiver senha"
                        value={sshKeyPassphrase}
                        onChange={(e) => {
                          setSshKeyPassphrase(e.target.value);
                          setTestResult(null);
                        }}
                      />
                    </div>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* TCP / TLS Remote Host Fields */}
          {(connectionType === "tcp" || connectionType === "tls") && (
            <div className="space-y-3 rounded-xl border border-slate-800 bg-slate-950/60 p-3.5">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <div className="sm:col-span-2">
                  <label className="text-[11px] font-medium text-slate-400">
                    Nome do Servidor ou Endereço IP
                  </label>
                  <div className="mt-1">
                    <Input
                      placeholder="ex: 192.168.1.100 ou docker.local"
                      value={serverHost}
                      onChange={(e) => {
                        setServerHost(e.target.value);
                        setTestResult(null);
                      }}
                      required
                    />
                  </div>
                </div>
                <div>
                  <label className="text-[11px] font-medium text-slate-400">Porta Docker</label>
                  <div className="mt-1">
                    <Input
                      type="number"
                      placeholder={connectionType === "tls" ? "2376" : "2375"}
                      value={serverPort}
                      onChange={(e) => {
                        setServerPort(e.target.value);
                        setTestResult(null);
                      }}
                    />
                  </div>
                </div>
              </div>

              {connectionType === "tls" && (
                <div className="space-y-2.5 pt-2 border-t border-slate-800">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-semibold text-slate-300">Certificados TLS</span>
                    <button
                      type="button"
                      onClick={() => openGuide("secure-tls")}
                      className="text-[11px] text-sky-400 hover:underline"
                    >
                      Como gerar certificados TLS?
                    </button>
                  </div>
                  <div>
                    <label className="text-[11px] text-slate-400">CA Certificate (PEM)</label>
                    <textarea
                      rows={2}
                      placeholder="-----BEGIN CERTIFICATE-----"
                      value={tlsCaCert}
                      onChange={(e) => setTlsCaCert(e.target.value)}
                      className="mt-1 w-full rounded border border-slate-800 bg-slate-900 p-2 font-mono text-[11px] text-slate-300"
                    />
                  </div>
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                    <div>
                      <label className="text-[11px] text-slate-400">Client Cert (PEM)</label>
                      <textarea
                        rows={2}
                        value={tlsCert}
                        onChange={(e) => setTlsCert(e.target.value)}
                        className="mt-1 w-full rounded border border-slate-800 bg-slate-900 p-2 font-mono text-[11px] text-slate-300"
                      />
                    </div>
                    <div>
                      <label className="text-[11px] text-slate-400">Client Key (PEM)</label>
                      <textarea
                        rows={2}
                        value={tlsKey}
                        onChange={(e) => setTlsKey(e.target.value)}
                        className="mt-1 w-full rounded border border-slate-800 bg-slate-900 p-2 font-mono text-[11px] text-slate-300"
                      />
                    </div>
                  </div>
                </div>
              )}
            </div>
          )}

          {/* Local Socket Field */}
          {connectionType === "local" && (
            <div className="rounded-xl border border-slate-800 bg-slate-950/60 p-3.5 space-y-2">
              <label className="text-xs font-medium text-slate-300">Caminho do Socket Unix</label>
              <Input
                placeholder="/var/run/docker.sock"
                value={localSocketPath}
                onChange={(e) => {
                  setLocalSocketPath(e.target.value);
                  setTestResult(null);
                }}
                required
              />
              <p className="text-[11px] text-slate-500 font-mono">
                Padrão Linux: /var/run/docker.sock
              </p>
            </div>
          )}

          {/* Auto URL Preview & Manual Override */}
          <div className="flex items-center justify-between text-[11px] text-slate-500 pt-1">
            <span className="font-mono text-slate-400">
              URL resultante:{" "}
              <strong className="text-sky-400 select-all">
                {computedUrl || "(preencha os dados do host)"}
              </strong>
            </span>
            <button
              type="button"
              onClick={() => {
                if (!isManualUrl && computedUrl) {
                  setHostUrl(computedUrl);
                }
                setIsManualUrl(!isManualUrl);
              }}
              className="text-slate-400 hover:text-white underline text-[11px]"
            >
              {isManualUrl ? "Voltar aos campos automáticos" : "Editar URL manualmente"}
            </button>
          </div>

          {isManualUrl && (
            <div className="space-y-1">
              <label className="text-xs font-medium text-slate-300">
                Docker Host URL Completa
              </label>
              <Input
                value={hostUrl}
                onChange={(e) => {
                  setHostUrl(e.target.value);
                  setTestResult(null);
                }}
                placeholder="ssh://root@192.168.1.100:22 ou tcp://192.168.1.100:2375"
              />
            </div>
          )}

          {/* Interactive Diagnostic Console Output */}
          <div className="rounded-xl border border-slate-800 bg-slate-950 overflow-hidden shadow-inner">
            <div className="flex items-center justify-between px-3 py-2 bg-slate-900 border-b border-slate-800 text-xs">
              <div className="flex items-center space-x-2">
                <Terminal className="h-3.5 w-3.5 text-sky-400" />
                <span className="font-semibold text-slate-200">
                  Console de Diagnóstico em Tempo Real
                </span>
                {isTesting && (
                  <span className="inline-flex items-center gap-1 text-[10px] text-sky-400 animate-pulse font-mono">
                    <RefreshCw className="h-3 w-3 animate-spin" />
                    Testando...
                  </span>
                )}
              </div>
              {diagnosticLogs.length > 0 && (
                <button
                  type="button"
                  onClick={() => setDiagnosticLogs([])}
                  className="text-[10px] text-slate-400 hover:text-white"
                >
                  Limpar Console
                </button>
              )}
            </div>

            <div className="p-3 font-mono text-[11px] max-h-48 overflow-y-auto space-y-1.5 scrollbar-thin scrollbar-thumb-slate-800 bg-slate-950/90">
              {diagnosticLogs.length === 0 ? (
                <p className="text-slate-600 italic">
                  Nenhum teste executado ainda. Clique em &quot;Testar Conexão&quot; abaixo para acompanhar cada etapa em tempo real.
                </p>
              ) : (
                diagnosticLogs.map((log, idx) => {
                  let badge = "INFO";
                  let badgeStyle = "bg-sky-500/10 text-sky-400 border-sky-500/30";
                  if (log.level === "success") {
                    badge = "OK";
                    badgeStyle = "bg-emerald-500/10 text-emerald-400 border-emerald-500/30";
                  } else if (log.level === "warning") {
                    badge = "WARN";
                    badgeStyle = "bg-amber-500/10 text-amber-400 border-amber-500/30";
                  } else if (log.level === "error") {
                    badge = "ERR";
                    badgeStyle = "bg-rose-500/10 text-rose-400 border-rose-500/30";
                  }

                  return (
                    <div key={idx} className="space-y-1">
                      <div className="flex items-start gap-2 leading-relaxed">
                        <span className="text-slate-600 shrink-0 text-[10px]">
                          [{log.timestamp}]
                        </span>
                        <span
                          className={`inline-block px-1 py-0.5 rounded border text-[9px] font-bold shrink-0 ${badgeStyle}`}
                        >
                          {badge}
                        </span>
                        <span
                          className={`break-words ${
                            log.level === "error"
                              ? "text-rose-300 font-medium"
                              : log.level === "warning"
                              ? "text-amber-300"
                              : log.level === "success"
                              ? "text-emerald-300 font-medium"
                              : "text-slate-300"
                          }`}
                        >
                          {log.message}
                        </span>
                      </div>
                      {log.suggestion && (
                        <div className="ml-16 rounded bg-slate-900 border border-amber-500/30 p-2 text-[10px] text-amber-300 flex items-start justify-between gap-2">
                          <span className="select-all font-mono leading-relaxed">
                            {log.suggestion}
                          </span>
                          <button
                            type="button"
                            onClick={() => navigator.clipboard.writeText(log.suggestion || "")}
                            className="shrink-0 text-slate-400 hover:text-white"
                            title="Copiar comando"
                          >
                            <Copy className="h-3 w-3" />
                          </button>
                        </div>
                      )}
                    </div>
                  );
                })
              )}
              <div ref={consoleBottomRef} />
            </div>
          </div>

          {/* Default Server Checkbox */}
          <div className="flex items-center space-x-2">
            <input
              type="checkbox"
              id="isDefault"
              checked={isDefault}
              onChange={(e) => setIsDefault(e.target.checked)}
              className="h-4 w-4 rounded border-slate-800 bg-slate-900 text-sky-500 focus:ring-sky-500"
            />
            <label htmlFor="isDefault" className="text-xs text-slate-300 cursor-pointer">
              Definir este host como o servidor padrão do sistema
            </label>
          </div>

          {/* Test Connection Result Box */}
          {testResult && (
            <div
              className={`flex items-start space-x-2 rounded-lg p-3 text-xs ${
                testResult.success
                  ? "bg-emerald-500/10 border border-emerald-500/30 text-emerald-300"
                  : "bg-rose-500/10 border border-rose-500/30 text-rose-300"
              }`}
            >
              {testResult.success ? (
                <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400 mt-0.5" />
              ) : (
                <AlertCircle className="h-4 w-4 shrink-0 text-rose-400 mt-0.5" />
              )}
              <span className="font-medium">{testResult.message}</span>
            </div>
          )}

          {formError && (
            <div className="flex items-center space-x-2 rounded-lg border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
              <AlertCircle className="h-4 w-4 shrink-0" />
              <span>{formError}</span>
            </div>
          )}

          {/* Action Buttons */}
          <div className="flex items-center justify-between pt-3 border-t border-slate-800">
            <Button
              type="button"
              variant="outline"
              onClick={handleTestConnection}
              disabled={isTesting || (!serverHost.trim() && !hostUrl.trim() && connectionType !== "local")}
            >
              <RefreshCw
                className={`mr-1.5 h-3.5 w-3.5 ${isTesting ? "animate-spin" : ""}`}
              />
              {isTesting ? "Testando..." : "Testar Conexão"}
            </Button>

            <div className="flex items-center space-x-2">
              <Button type="button" variant="outline" onClick={() => setIsModalOpen(false)}>
                Cancelar
              </Button>
              <Button
                type="submit"
                variant="docker"
                disabled={isSaving || !name || (!serverHost && !hostUrl && connectionType !== "local")}
              >
                {isSaving ? "Salvando..." : editingServer ? "Salvar Alterações" : "Cadastrar Host"}
              </Button>
            </div>
          </div>
        </form>
      </Modal>

      {/* Delete Confirmation Modal */}
      <ConfirmActionModal
        isOpen={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDeleteServer}
        title="Remover Host Docker"
        description={`Tem certeza que deseja remover o servidor "${deleteTarget?.name}" (${deleteTarget?.host_url})? Qualquer sessão ativa será redirecionada para o servidor padrão.`}
        confirmLabel="Remover Servidor"
      />

      {/* Host Setup Guide Modal */}
      <HostSetupGuideModal
        isOpen={isGuideOpen}
        onClose={() => setIsGuideOpen(false)}
        initialTopic={guideTopic}
        onSelectPreset={(topic) => {
          handleSelectGuidePreset(topic);
          setIsGuideOpen(false);
        }}
      />
    </div>
  );
}
