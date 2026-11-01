"use client";

import React, { useState, useEffect, useMemo, useRef, useCallback } from "react";
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
  ShieldCheck,
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
  Package,
  ShieldAlert,
  KeyRound,
  Download,
  Minimize2,
  FolderSearch,
} from "lucide-react";
import { SourceDirsModal } from "@/components/modals/SourceDirsModal";
import { useServer } from "@/context/ServerContext";
import {
  api,
  testServerStream,
  streamServersStatus,
  getHostPackages,
  checkHostPackages,
  upgradeHostPackages,
  upgradeHostPackagesStream,
  getActiveHostUpgradeJob,
  listActiveHostUpgradeJobs,
  updateHostPackageManager,
  updateHostSudoPolicy,
  clearHostSudoPassword,
} from "@/lib/api";
import {
  DockerServer,
  CreateServerPayload,
  TestServerPayload,
  ServerDiagnosticLog,
  HostPackageStatus,
  PackageUpdate,
  PackageUpgradeLog,
  PackageUpgradeResult,
  ActiveUpgradeJob,
} from "@/types";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Modal } from "@/components/ui/Modal";
import { ProvisionKeyPanel } from "@/components/modals/ProvisionKeyPanel";
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
  SortableTableHead,
  TableBody,
  TableCell,
  TableEmpty,
} from "@/components/ui/Table";
import { SortControl } from "@/components/ui/SortControl";
import { sortItems, SortDirection } from "@/lib/sort";

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
    label: "SSH (Usuário/Senha)",
    sublabel: "Porta 22 com senha",
    icon: UserCheck,
  },
  {
    id: "ssh-key",
    type: "ssh",
    label: "SSH (Chave Privada)",
    sublabel: "Recomendado para Nuvem",
    icon: Key,
  },
  {
    id: "secure-tls",
    type: "tls",
    label: "Secure TLS (2376)",
    sublabel: "mTLS com certificados",
    icon: Shield,
  },
  {
    id: "remote-tcp",
    type: "tcp",
    label: "Remote TCP (2375)",
    sublabel: "Rede local ou VPN",
    icon: Server,
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
  const {
    servers,
    activeServer,
    setActiveServer,
    refreshServers,
    updateServerState,
    setAllServersStatus,
    isLoading,
  } = useServer();
  const [search, setSearch] = useState("");
  const [sortKey, setSortKey] = useState<string>("name");
  const [sortDirection, setSortDirection] = useState<SortDirection>("asc");

  const handleSort = (key: string) => {
    if (sortKey === key) {
      setSortDirection((prev) => (prev === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDirection("asc");
    }
  };
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

  // Credential & Sudo Policy State
  const [credentialPolicy, setCredentialPolicy] = useState<"save_securely" | "always_prompt">("save_securely");
  const [sudoPolicy, setSudoPolicy] = useState<"save_securely" | "always_prompt" | "">("always_prompt");
  const [sudoPassword, setSudoPassword] = useState("");
  const [formPackageManager, setFormPackageManager] = useState<string>("");

  // Package Management Modal State
  const [packageModalServer, setPackageModalServer] = useState<DockerServer | null>(null);
  const [packageStatus, setPackageStatus] = useState<HostPackageStatus | null>(null);
  const [selectedPackageManager, setSelectedPackageManager] = useState<string>("");
  const [isPackagesLoading, setIsPackagesLoading] = useState(false);
  const [selectedPackages, setSelectedPackages] = useState<string[]>([]);
  const [packageActionError, setPackageActionError] = useState<string | null>(null);
  const [packageActionSuccess, setPackageActionSuccess] = useState<string | null>(null);

  // Sudo Password Prompt Dialog State
  const [isSudoPromptOpen, setIsSudoPromptOpen] = useState(false);
  const [sudoPromptPendingAction, setSudoPromptPendingAction] = useState<"check" | "upgrade" | null>(null);
  const [sudoPromptPendingPackages, setSudoPromptPendingPackages] = useState<string[] | undefined>(undefined);
  const [promptSudoPassword, setPromptSudoPassword] = useState("");
  const [promptSaveSudo, setPromptSaveSudo] = useState(false);

  // Active Background Upgrade Jobs Map
  const [activeJobsMap, setActiveJobsMap] = useState<Record<number, ActiveUpgradeJob>>({});
  const [backgroundNotice, setBackgroundNotice] = useState<string | null>(null);

  // Live Package Upgrade Console State (Scoped per Server ID for concurrent upgrades)
  const [upgradingServers, setUpgradingServers] = useState<Record<number, boolean>>({});
  const [upgradeLogsByServer, setUpgradeLogsByServer] = useState<Record<number, PackageUpgradeLog[]>>({});
  const [upgradeResultsByServer, setUpgradeResultsByServer] = useState<Record<number, PackageUpgradeResult | null>>({});
  const [upgradeDurationsByServer, setUpgradeDurationsByServer] = useState<Record<number, number>>({});
  const [consoleOpenServers, setConsoleOpenServers] = useState<Record<number, boolean>>({});
  const [isLogsCopied, setIsLogsCopied] = useState(false);
  const streamControllersRef = useRef<Record<number, AbortController>>({});
  const packageModalServerRef = useRef<DockerServer | null>(null);
  packageModalServerRef.current = packageModalServer;
  const refreshServersRef = useRef(refreshServers);
  refreshServersRef.current = refreshServers;

  // Derived state for the currently active modal's server
  const currentModalServerId = packageModalServer?.id;
  const isUpgrading = currentModalServerId
    ? Boolean(upgradingServers[currentModalServerId] || activeJobsMap[currentModalServerId]?.status === "running")
    : false;
  const upgradeLogs = useMemo(
    () => (currentModalServerId ? upgradeLogsByServer[currentModalServerId] || [] : []),
    [currentModalServerId, upgradeLogsByServer]
  );
  const upgradeResult = currentModalServerId ? (upgradeResultsByServer[currentModalServerId] || null) : null;
  const upgradeDuration = currentModalServerId ? (upgradeDurationsByServer[currentModalServerId] || 0) : 0;
  const isUpgradeConsoleOpen = currentModalServerId ? Boolean(consoleOpenServers[currentModalServerId]) : false;

  const setIsUpgradeConsoleOpen = (open: boolean) => {
    if (!packageModalServer) return;
    setConsoleOpenServers((prev) => ({ ...prev, [packageModalServer.id]: open }));
  };

  const setUpgradeLogs = (updater: PackageUpgradeLog[] | ((prev: PackageUpgradeLog[]) => PackageUpgradeLog[])) => {
    if (!packageModalServerRef.current) return;
    const sId = packageModalServerRef.current.id;
    setUpgradeLogsByServer((prev) => ({
      ...prev,
      [sId]: typeof updater === "function" ? updater(prev[sId] || []) : updater,
    }));
  };

  const setUpgradeResult = (res: PackageUpgradeResult | null) => {
    if (!packageModalServerRef.current) return;
    const sId = packageModalServerRef.current.id;
    setUpgradeResultsByServer((prev) => ({
      ...prev,
      [sId]: res,
    }));
  };

  const setUpgradeDuration = (updater: number | ((prev: number) => number)) => {
    if (!packageModalServerRef.current) return;
    const sId = packageModalServerRef.current.id;
    setUpgradeDurationsByServer((prev) => ({
      ...prev,
      [sId]: typeof updater === "function" ? updater(prev[sId] || 0) : updater,
    }));
  };

  // Source Search Directories Modal State
  const [sourceModalOpen, setSourceModalOpen] = useState(false);
  const [selectedSourceServer, setSelectedSourceServer] = useState<DockerServer | null>(null);
  const upgradeConsoleBottomRef = useRef<HTMLDivElement | null>(null);

  // Poll for background upgrade jobs across servers
  useEffect(() => {
    let isMounted = true;
    const fetchActiveJobs = async () => {
      try {
        const jobs = await listActiveHostUpgradeJobs();
        if (!isMounted) return;
        const mapping: Record<number, ActiveUpgradeJob> = {};
        for (const j of jobs) {
          mapping[j.server_id] = {
            has_active_job: true,
            server_id: j.server_id,
            server_name: j.server_name,
            status: j.status as any,
            package_manager: j.package_manager,
            started_at: j.started_at,
            logs_count: j.logs_count,
          };
        }
        setActiveJobsMap((prev) => {
          const prevKeys = Object.keys(prev);
          const hadFinished = prevKeys.some((k) => !mapping[Number(k)]);
          if (hadFinished) {
            refreshServersRef.current();
          }
          return mapping;
        });
      } catch {
        // Polling failure ignored (offline or transient)
      }
    };

    fetchActiveJobs();
    const interval = setInterval(fetchActiveJobs, 4000);
    return () => {
      isMounted = false;
      clearInterval(interval);
    };
  }, []);

  // Auto-scroll console
  useEffect(() => {
    if (diagnosticLogs.length > 0 && consoleBottomRef.current) {
      consoleBottomRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [diagnosticLogs]);

  useEffect(() => {
    if (upgradeLogs.length > 0 && upgradeConsoleBottomRef.current) {
      upgradeConsoleBottomRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [upgradeLogs]);

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

  const startLiveStatusProbe = useCallback(async () => {
    setIsRefreshing(true);
    setAllServersStatus("CONNECTING");

    try {
      await streamServersStatus((update) => {
        updateServerState(update);
      });
    } catch (err) {
      console.warn("Live status stream ended or failed:", err);
    } finally {
      setIsRefreshing(false);
    }
  }, [setAllServersStatus, updateServerState]);

  const handleManualRefresh = async () => {
    await startLiveStatusProbe();
  };

  const hasProbedOnMount = useRef(false);
  useEffect(() => {
    if (!hasProbedOnMount.current) {
      hasProbedOnMount.current = true;
      startLiveStatusProbe();
    }
  }, [startLiveStatusProbe]);

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

    setCredentialPolicy("save_securely");
    setSudoPolicy("always_prompt");
    setSudoPassword("");
    setFormPackageManager("");

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

    setCredentialPolicy((server.credential_policy as "save_securely" | "always_prompt") || "save_securely");
    setSudoPolicy((server.sudo_policy as "save_securely" | "always_prompt" | "") || "always_prompt");
    setSudoPassword("");
    setFormPackageManager(server.package_manager || "");

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
    setGuideTopic(topic);
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
        credential_policy: credentialPolicy,
        sudo_policy: sudoPolicy || undefined,
        sudo_password: sudoPassword.trim() || undefined,
        package_manager: formPackageManager || undefined,
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

  const handleOpenPackagesModal = async (server: DockerServer) => {
    // If switching between servers while a UI stream was actively running, abort previous UI stream cleanly
    if (packageModalServer && packageModalServer.id !== server.id) {
      const prevId = packageModalServer.id;
      if (streamControllersRef.current[prevId]) {
        streamControllersRef.current[prevId].abort();
        delete streamControllersRef.current[prevId];
      }
    }

    setPackageModalServer(server);
    setPackageActionError(null);
    setPackageActionSuccess(null);
    setSelectedPackages([]);
    setSelectedPackageManager(server.package_manager || "");
    setIsPackagesLoading(true);

    try {
      // 1. Check if an active or recent upgrade job exists on this host
      const activeJob = await getActiveHostUpgradeJob(server.id);
      if (activeJob && activeJob.has_active_job) {
        setConsoleOpenServers((prev) => ({ ...prev, [server.id]: true }));
        setUpgradingServers((prev) => ({ ...prev, [server.id]: true }));
        if (activeJob.duration_seconds !== undefined) {
          setUpgradeDurationsByServer((prev) => ({ ...prev, [server.id]: activeJob.duration_seconds || 0 }));
        } else if (activeJob.started_at) {
          const elapsed = Math.round((Date.now() - new Date(activeJob.started_at).getTime()) / 1000);
          setUpgradeDurationsByServer((prev) => ({ ...prev, [server.id]: elapsed > 0 ? elapsed : 0 }));
        }
        // Immediately reattach to stream
        setTimeout(() => {
          handleTriggerUpgradePackages(undefined, undefined, true);
        }, 50);
      } else if (activeJob && (activeJob.status === "success" || activeJob.status === "failed")) {
        if (activeJob.result) {
          setUpgradeResultsByServer((prev) => ({ ...prev, [server.id]: activeJob.result || null }));
          if (activeJob.duration_seconds !== undefined) {
            setUpgradeDurationsByServer((prev) => ({ ...prev, [server.id]: activeJob.duration_seconds || 0 }));
          }
        }
      }

      const data = await getHostPackages(server.id);
      setPackageStatus(data);
      if (data.package_manager && data.package_manager !== "unknown") {
        setSelectedPackageManager(data.package_manager);
      }
      setPackageModalServer((prev) => prev && prev.id === server.id ? {
        ...prev,
        has_saved_sudo_password: data.has_saved_sudo_password,
        sudo_policy: data.sudo_policy,
        package_manager: data.package_manager || prev.package_manager,
      } : prev);
    } catch (err: any) {
      setPackageActionError(err?.message || "Falha ao carregar pacotes do servidor");
    } finally {
      setIsPackagesLoading(false);
    }
  };

  const handleChangePackageManager = async (newPm: string) => {
    if (!packageModalServer) return;
    setSelectedPackageManager(newPm);
    setPackageActionError(null);
    try {
      await updateHostPackageManager(packageModalServer.id, newPm);
      packageModalServer.package_manager = newPm;
      setPackageStatus((prev) => (prev ? { ...prev, package_manager: newPm } : prev));
      setPackageActionSuccess(`Gerenciador definido como: ${newPm ? newPm.toUpperCase() : "Automático"}.`);
      await refreshServers();
    } catch (err: any) {
      setPackageActionError("Falha ao salvar gerenciador de pacotes: " + (err?.message || err));
    }
  };

  const handleTriggerCheckPackages = async (overridePassword?: string, saveSudoPolicy?: string) => {
    if (!packageModalServer) return;
    const hasSaved = packageStatus?.has_saved_sudo_password ?? packageModalServer.has_saved_sudo_password;
    const pwd = overridePassword !== undefined ? overridePassword : (hasSaved ? undefined : "");
    setIsPackagesLoading(true);
    setPackageActionError(null);
    setPackageActionSuccess(null);
    try {
      const pm = selectedPackageManager && selectedPackageManager !== "auto" ? selectedPackageManager : undefined;
      const updated = await checkHostPackages(packageModalServer.id, pwd, pm, saveSudoPolicy);
      setPackageStatus(updated);
      if (updated.package_manager && updated.package_manager !== "unknown") {
        setSelectedPackageManager(updated.package_manager);
        packageModalServer.package_manager = updated.package_manager;
      }
      if (updated.has_saved_sudo_password !== undefined) {
        packageModalServer.has_saved_sudo_password = updated.has_saved_sudo_password;
        packageModalServer.sudo_policy = updated.sudo_policy;
      }
      const count = updated.available_updates_count ?? updated.updates_count ?? (updated.updates ? updated.updates.length : 0);
      if (count === 0) {
        setPackageActionSuccess("Verificação concluída! Nenhuma atualização encontrada.");
      } else if (count === 1) {
        setPackageActionSuccess("Verificação concluída! 1 atualização encontrada.");
      } else {
        setPackageActionSuccess(`Verificação concluída! ${count} atualizações encontradas.`);
      }
      await refreshServers();
    } catch (err: any) {
      const errMsg = err?.message || "";
      const errCode = (err as any)?.data?.code;
      if (errCode === "PACKAGE_MANAGER_UNKNOWN" || errMsg.includes("identify package manager")) {
        setPackageActionError("Não foi possível detectar o gerenciador de pacotes. Selecione-o no menu suspenso.");
      } else if (
        errCode === "SUDO_PASSWORD_REQUIRED" ||
        errCode === "INVALID_SUDO_PASSWORD" ||
        errMsg.toLowerCase().includes("sudo password required") ||
        errMsg.toLowerCase().includes("invalid sudo password")
      ) {
        setSudoPromptPendingAction("check");
        setIsSudoPromptOpen(true);
        if (errCode === "INVALID_SUDO_PASSWORD") {
          setPackageActionError("A senha de sudo fornecida foi rejeitada pelo servidor remoto. Por favor, verifique.");
        }
      } else {
        setPackageActionError(errMsg || "Falha ao verificar atualizações de pacotes");
      }
    } finally {
      setIsPackagesLoading(false);
    }
  };

  const handleTriggerUpgradePackages = async (
    overridePackages?: string[],
    overridePassword?: string,
    isReattach: boolean = false,
    saveSudoPolicy?: string
  ) => {
    if (!packageModalServer) return;
    const targetServerId = packageModalServer.id;
    const targetServerName = packageModalServer.name;
    const hasSaved = packageStatus?.has_saved_sudo_password ?? packageModalServer.has_saved_sudo_password;
    const pwd = isReattach ? undefined : (overridePassword !== undefined ? overridePassword : (hasSaved ? undefined : ""));
    const pkgsToUpgrade = isReattach ? undefined : (overridePackages !== undefined ? overridePackages : (selectedPackages.length > 0 ? selectedPackages : undefined));
    const pm = isReattach ? undefined : (selectedPackageManager && selectedPackageManager !== "auto" ? selectedPackageManager : undefined);

    setPackageActionError(null);
    setPackageActionSuccess(null);
    setIsPackagesLoading(true);

    // Open console view and start streaming for this specific server
    if (!isReattach) {
      setUpgradeLogsByServer((prev) => ({ ...prev, [targetServerId]: [] }));
      setUpgradeResultsByServer((prev) => ({ ...prev, [targetServerId]: null }));
      setUpgradeDurationsByServer((prev) => ({ ...prev, [targetServerId]: 0 }));
    }
    setUpgradingServers((prev) => ({ ...prev, [targetServerId]: true }));
    setConsoleOpenServers((prev) => ({ ...prev, [targetServerId]: true }));
    const start = Date.now();

    // Isolated AbortController for this server's UI stream
    const controller = new AbortController();
    streamControllersRef.current[targetServerId] = controller;

    try {
      const res = await upgradeHostPackagesStream(
        targetServerId,
        pkgsToUpgrade,
        pwd,
        pm,
        (log) => {
          setUpgradeLogsByServer((prev) => ({
            ...prev,
            [targetServerId]: [...(prev[targetServerId] || []), log],
          }));
        },
        controller.signal,
        saveSudoPolicy,
        isReattach ? "GET" : "POST"
      );

      const elapsed = Math.round((Date.now() - start) / 1000);
      setUpgradeDurationsByServer((prev) => ({
        ...prev,
        [targetServerId]: (prev[targetServerId] || 0) + elapsed,
      }));
      setUpgradeResultsByServer((prev) => ({
        ...prev,
        [targetServerId]: res,
      }));

      if (res.success) {
        if (packageModalServerRef.current?.id === targetServerId) {
          setPackageActionSuccess(res.message || "Pacotes atualizados com sucesso!");
          setSelectedPackages([]);
          const updated = await getHostPackages(targetServerId);
          setPackageStatus(updated);
        }
        await refreshServers();
      } else {
        if (packageModalServerRef.current?.id === targetServerId) {
          setPackageActionError(res.message || "Falha na atualização dos pacotes");
        }
      }
    } catch (err: any) {
      if (err?.name === "AbortError" || err?.message?.includes("aborted")) {
        // UI stream disconnected/backgrounded intentionally.
        // Remote job continues unaffected on the host.
        return;
      }
      const errMsg = err?.message || "";
      const errCode = (err as any)?.data?.code;
      if (errCode === "PACKAGE_MANAGER_UNKNOWN" || errMsg.includes("identify package manager")) {
        if (packageModalServerRef.current?.id === targetServerId) {
          setPackageActionError("Não foi possível detectar o gerenciador de pacotes. Selecione-o no menu suspenso.");
          setConsoleOpenServers((prev) => ({ ...prev, [targetServerId]: false }));
        }
      } else if (
        errCode === "SUDO_PASSWORD_REQUIRED" ||
        errCode === "INVALID_SUDO_PASSWORD" ||
        errMsg.toLowerCase().includes("sudo password required") ||
        errMsg.toLowerCase().includes("invalid sudo password")
      ) {
        if (packageModalServerRef.current?.id === targetServerId) {
          setConsoleOpenServers((prev) => ({ ...prev, [targetServerId]: false }));
          setSudoPromptPendingAction("upgrade");
          setSudoPromptPendingPackages(pkgsToUpgrade);
          setIsSudoPromptOpen(true);
          if (errCode === "INVALID_SUDO_PASSWORD") {
            setPackageActionError("A senha de sudo fornecida foi rejeitada pelo servidor remoto. Por favor, verifique.");
          }
        }
      } else {
        if (packageModalServerRef.current?.id === targetServerId) {
          setPackageActionError(errMsg || "Falha ao atualizar pacotes");
        }
        setUpgradeResultsByServer((prev) => ({
          ...prev,
          [targetServerId]: {
            success: false,
            message: errMsg || "Falha no processo de atualização",
            error: errMsg,
          },
        }));
      }
    } finally {
      setUpgradingServers((prev) => {
        const next = { ...prev };
        delete next[targetServerId];
        return next;
      });
      delete streamControllersRef.current[targetServerId];
      if (packageModalServerRef.current?.id === targetServerId) {
        setIsPackagesLoading(false);
      }
    }
  };

  const handleSendUpgradeToBackground = () => {
    if (!packageModalServer) return;
    const serverId = packageModalServer.id;
    const serverName = packageModalServer.name;

    // Cleanly abort UI stream reader without touching the remote detached upgrade
    if (streamControllersRef.current[serverId]) {
      streamControllersRef.current[serverId].abort();
      delete streamControllersRef.current[serverId];
    }

    setUpgradingServers((prev) => {
      const next = { ...prev };
      delete next[serverId];
      return next;
    });

    setActiveJobsMap((prev) => ({
      ...prev,
      [serverId]: {
        has_active_job: true,
        server_id: serverId,
        server_name: serverName,
        status: "running",
        started_at: new Date().toISOString(),
      },
    }));

    setPackageModalServer(null);
    setBackgroundNotice(`A atualização do host "${serverName}" continua executando em segundo plano. Você pode abrir e atualizar outros hosts simultaneamente.`);
    setTimeout(() => setBackgroundNotice(null), 8000);
  };

  const handleCopyUpgradeLogs = () => {
    const text = upgradeLogs
      .map((l) => `[${l.timestamp ? new Date(l.timestamp).toLocaleTimeString() : ""}] [${(l.level || "stdout").toUpperCase()}] ${l.message}`)
      .join("\n");
    navigator.clipboard.writeText(text);
    setIsLogsCopied(true);
    setTimeout(() => setIsLogsCopied(false), 2000);
  };

  const handleSudoPromptSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!packageModalServer || !promptSudoPassword) return;

    const enteredPwd = promptSudoPassword;
    const shouldSave = promptSaveSudo;
    const savePolicy = shouldSave ? "save_securely" : undefined;
    setIsSudoPromptOpen(false);
    setPromptSudoPassword("");

    if (shouldSave) {
      packageModalServer.has_saved_sudo_password = true;
      packageModalServer.sudo_policy = "save_securely";
      setPackageStatus((prev) => prev ? { ...prev, has_saved_sudo_password: true, sudo_policy: "save_securely" } : prev);
      try {
        await updateHostSudoPolicy(packageModalServer.id, "save_securely", enteredPwd);
      } catch {
        // Ignored or logged - the upgrade/check request will also persist it via save_sudo_policy
      }
    }

    if (sudoPromptPendingAction === "check") {
      await handleTriggerCheckPackages(enteredPwd, savePolicy);
    } else if (sudoPromptPendingAction === "upgrade") {
      await handleTriggerUpgradePackages(sudoPromptPendingPackages, enteredPwd, false, savePolicy);
    }
    setSudoPromptPendingAction(null);
    setSudoPromptPendingPackages(undefined);
  };

  // Filter & sort servers
  const filteredServers = useMemo(() => {
    const list = servers.filter((s) => {
      const query = search.toLowerCase().trim();
      if (!query) return true;
      return (
        s.name.toLowerCase().includes(query) ||
        s.host_url.toLowerCase().includes(query) ||
        s.status.toLowerCase().includes(query)
      );
    });

    const customGetters = {
      name: (s: DockerServer) => s.name,
      host_url: (s: DockerServer) => s.host_url,
      status: (s: DockerServer) => s.status,
      updates: (s: DockerServer) => s.available_updates_count ?? 0,
      containers: (s: DockerServer) => s.containers_count ?? 0,
    };

    return sortItems(list, sortKey, sortDirection, customGetters);
  }, [servers, search, sortKey, sortDirection]);

  const onlineCount = servers.filter((s) => s.status === "CONNECTED").length;
  const offlineCount = servers.filter((s) => s.status === "UNREACHABLE").length;
  const connectingCount = servers.filter((s) => s.status === "CONNECTING").length;
  const hostUpdates = packageStatus?.updates || [];

  return (
    <div className="space-y-6">
      {/* Background Upgrade Notice Banner */}
      {backgroundNotice && (
        <div className="p-3.5 rounded-xl bg-sky-500/10 border border-sky-500/30 text-sky-200 text-xs flex items-center justify-between shadow-lg animate-in fade-in">
          <div className="flex items-center gap-2.5">
            <RefreshCw className="h-4 w-4 text-sky-400 animate-spin shrink-0" />
            <span>{backgroundNotice}</span>
          </div>
          <button
            onClick={() => setBackgroundNotice(null)}
            className="text-sky-400 hover:text-white text-xs px-2 py-0.5 rounded hover:bg-sky-500/20"
          >
            ✕
          </button>
        </div>
      )}

      {/* Global Indicator for Servers with Active Upgrades */}
      {Object.keys(activeJobsMap).length > 0 && !backgroundNotice && (
        <div className="p-3 rounded-xl bg-amber-500/10 border border-amber-500/30 text-amber-200 text-xs flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <RefreshCw className="h-4 w-4 text-amber-400 animate-spin shrink-0" />
            <span>
              <strong>{Object.keys(activeJobsMap).length}</strong> servidor(es) com atualização de pacotes executando em segundo plano no host.
            </span>
          </div>
          <span className="text-[11px] text-amber-300 font-mono hidden sm:inline">
            Processos persistentes em conexões SSH simultâneas
          </span>
        </div>
      )}
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
            {connectingCount > 0 ? `Verificando (${connectingCount})...` : "Atualizar Status"}
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
          <p className="text-[11px] text-slate-500 mt-1">
            {connectingCount > 0 ? `${connectingCount} verificando em paralelo...` : "Respondendo ao Ping"}
          </p>
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

      {/* Parallel Probe Progress Banner */}
      {connectingCount > 0 && (
        <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 p-3 text-amber-300 text-xs flex items-center justify-between shadow-sm animate-pulse">
          <div className="flex items-center gap-2.5">
            <RefreshCw className="h-4 w-4 animate-spin text-amber-400 shrink-0" />
            <span>
              Verificando status de <strong>{connectingCount}</strong> host(s) em paralelo. Os status estão como <em>Tentando conectar...</em> e atualizarão automaticamente.
            </span>
          </div>
          <span className="font-mono text-[10px] uppercase tracking-wider px-2 py-0.5 rounded bg-amber-500/20 border border-amber-500/40 text-amber-300 font-bold shrink-0 hidden sm:inline">
            Paralelo Ativo
          </span>
        </div>
      )}

      {/* Search and Sort */}
      <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-4">
        <div className="relative max-w-sm flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-500" />
          <Input
            placeholder="Buscar por nome, URL ou status..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-9"
          />
        </div>

        <SortControl
          label="Ordenar Hosts:"
          options={[
            { value: "name", label: "Nome do Host" },
            { value: "status", label: "Status" },
            { value: "updates", label: "Atualizações SO" },
            { value: "containers", label: "Containers" },
            { value: "host_url", label: "URL de Conexão" },
          ]}
          currentKey={sortKey}
          currentDirection={sortDirection}
          onKeyChange={(k) => setSortKey(k)}
          onDirectionToggle={() =>
            setSortDirection((prev) => (prev === "asc" ? "desc" : "asc"))
          }
        />
      </div>

      {/* Servers Table */}
      <div className="rounded-xl border border-slate-800 bg-slate-900/40 overflow-hidden shadow-sm">
        <Table aria-label="Tabela de servidores Docker e hosts">
          <TableHeader>
            <TableRow>
              <SortableTableHead
                sortKey="name"
                currentSortKey={sortKey}
                currentDirection={sortDirection}
                onSort={handleSort}
              >
                Nome do Host
              </SortableTableHead>
              <SortableTableHead
                sortKey="host_url"
                currentSortKey={sortKey}
                currentDirection={sortDirection}
                onSort={handleSort}
              >
                URL de Conexão
              </SortableTableHead>
              <TableHead>Segurança / Credenciais</TableHead>
              <SortableTableHead
                sortKey="status"
                currentSortKey={sortKey}
                currentDirection={sortDirection}
                onSort={handleSort}
              >
                Status & Versão
              </SortableTableHead>
              <SortableTableHead
                sortKey="updates"
                currentSortKey={sortKey}
                currentDirection={sortDirection}
                onSort={handleSort}
              >
                Pacotes / SO
              </SortableTableHead>
              <SortableTableHead
                sortKey="containers"
                currentSortKey={sortKey}
                currentDirection={sortDirection}
                onSort={handleSort}
              >
                Cargas de Trabalho
              </SortableTableHead>
              <TableHead className="text-right">Ações</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filteredServers.length === 0 ? (
              <TableEmpty
                colSpan={7}
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
                      <div className="flex flex-col gap-1">
                        {isSsh ? (
                          <span className="inline-flex items-center space-x-1 rounded bg-indigo-500/10 border border-indigo-500/30 px-1.5 py-0.5 text-[11px] text-indigo-300 w-fit">
                            <Key className="h-3 w-3" />
                            <span>
                              {server.ssh_auth_method === "password"
                                ? "SSH (Senha)"
                                : "SSH (Chave/Cert)"}
                            </span>
                          </span>
                        ) : server.tls_verify ? (
                          <span className="inline-flex items-center space-x-1 rounded bg-amber-500/10 border border-amber-500/30 px-1.5 py-0.5 text-[11px] text-amber-400 w-fit">
                            <Lock className="h-3 w-3" />
                            <span>TLS Seguro</span>
                          </span>
                        ) : (
                          <span className="inline-flex items-center space-x-1 text-slate-500 text-xs font-mono w-fit">
                            <Unlock className="h-3 w-3" />
                            <span>Padrão TCP/Socket</span>
                          </span>
                        )}

                        <span className="text-[10px] text-slate-400 font-mono">
                          {server.credential_policy === "always_prompt" ? (
                            <span className="text-amber-400/90 font-medium">⚡ Sempre solicitar</span>
                          ) : (
                            <span className="text-emerald-400/90 font-medium">🔒 Salvo criptografado</span>
                          )}
                        </span>
                      </div>
                    </TableCell>

                    <TableCell>
                      <div className="flex flex-col space-y-0.5">
                        <div className="flex items-center space-x-1.5">
                          <span
                            className={`h-2 w-2 rounded-full ${
                              server.status === "CONNECTING"
                                ? "bg-amber-400 animate-pulse"
                                : isOnline
                                ? "bg-emerald-400"
                                : "bg-rose-400"
                            }`}
                          />
                          <span
                            className={`text-xs font-medium flex items-center gap-1.5 ${
                              server.status === "CONNECTING"
                                ? "text-amber-400 font-semibold"
                                : isOnline
                                ? "text-emerald-400"
                                : "text-rose-400"
                            }`}
                          >
                            {server.status === "CONNECTING" ? (
                              <>
                                <RefreshCw className="h-3 w-3 animate-spin text-amber-400 shrink-0" />
                                <span>Tentando conectar...</span>
                              </>
                            ) : isOnline ? (
                              "Online"
                            ) : (
                              "Inacessível"
                            )}
                          </span>
                        </div>
                        {server.version && server.status !== "CONNECTING" && (
                          <span className="text-[10px] font-mono text-slate-400">
                            Docker {server.version}
                          </span>
                        )}
                        {server.err_msg && server.status !== "CONNECTING" && (
                          <span
                            className="text-[10px] text-rose-400/80 line-clamp-1 max-w-[200px]"
                            title={server.err_msg}
                          >
                            {server.err_msg}
                          </span>
                        )}
                      </div>
                    </TableCell>

                    {/* Pacotes / SO Column */}
                    <TableCell>
                      <div className="flex flex-col gap-1 text-xs">
                        <div className="flex items-center gap-1.5">
                          <span className="uppercase text-[10px] font-mono px-1.5 py-0.5 rounded bg-slate-800 text-slate-300 font-bold">
                            {server.package_manager || "desconhecido"}
                          </span>
                          {activeJobsMap[server.id] ? (
                            <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-semibold bg-sky-500/20 text-sky-300 border border-sky-500/30 animate-pulse">
                              <RefreshCw className="h-2.5 w-2.5 animate-spin" />
                              Atualizando...
                            </span>
                          ) : (server.available_updates_count || 0) > 0 ? (
                            <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-semibold bg-amber-500/20 text-amber-300 border border-amber-500/30">
                              {server.available_updates_count} atualizações
                            </span>
                          ) : (
                            <span className="text-[10px] text-slate-500">Em dia</span>
                          )}
                        </div>
                        {(server.security_updates_count || 0) > 0 && (
                          <span className="inline-flex items-center gap-1 text-[10px] font-semibold text-rose-400">
                            <ShieldAlert className="h-3 w-3" />
                            {server.security_updates_count} de segurança
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
                        <Button
                          variant={activeJobsMap[server.id] ? "default" : "outline"}
                          size="sm"
                          className={`text-xs h-7 px-2 ${
                            activeJobsMap[server.id]
                              ? "bg-sky-600 hover:bg-sky-500 text-white font-medium shadow-sm animate-pulse"
                              : "border-slate-700 hover:border-slate-600 text-sky-300 bg-slate-900/40"
                          }`}
                          onClick={() => handleOpenPackagesModal(server)}
                          title={
                            activeJobsMap[server.id]
                              ? "Clique para retomar a visualização da atualização em segundo plano"
                              : "Gerenciar pacotes e atualizações do sistema operacional"
                          }
                        >
                          {activeJobsMap[server.id] ? (
                            <>
                              <RefreshCw className="h-3.5 w-3.5 mr-1 animate-spin" />
                              Retomar
                            </>
                          ) : (
                            <>
                              <Package className="h-3.5 w-3.5 mr-1 text-sky-400" />
                              Pacotes
                            </>
                          )}
                        </Button>

                        <Button
                          variant="outline"
                          size="sm"
                          className="text-xs h-7 px-2 border-slate-700 hover:border-slate-600 text-indigo-300 bg-slate-900/40"
                          onClick={() => {
                            setSelectedSourceServer(server);
                            setSourceModalOpen(true);
                          }}
                          title="Configurar diretórios de busca e escanear arquivos Dockerfile/Compose"
                        >
                          <FolderSearch className="h-3.5 w-3.5 mr-1 text-indigo-400" />
                          Fontes
                        </Button>

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
        maxWidth="2xl"
      >
        <form onSubmit={handleSaveServer} className="space-y-4">
          {/* Quick Presets with Help Links */}
          <div className="space-y-2">
            <div className="flex items-center justify-between pb-1 border-b border-slate-800/80">
              <span className="text-xs font-semibold text-slate-300">Modelos de Conexão Rápida</span>
              <button
                type="button"
                onClick={() => openGuide(guideTopic)}
                className="inline-flex items-center gap-1.5 text-xs font-medium text-sky-400 hover:text-sky-300 hover:underline transition-colors"
              >
                <BookOpen className="h-3.5 w-3.5" />
                <span>Guia passo a passo de configuração</span>
              </button>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 gap-2.5">
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
                    onClick={() => {
                      handleSelectGuidePreset(preset.id);
                      setGuideTopic(preset.id);
                    }}
                    className={`group relative flex flex-col justify-between rounded-xl border p-2.5 transition-all cursor-pointer min-w-0 ${
                      isCurrent
                        ? "border-sky-500 bg-sky-500/10 shadow-sm ring-1 ring-sky-500/30"
                        : "border-slate-800 bg-slate-900/60 hover:border-slate-700 hover:bg-slate-900"
                    }`}
                  >
                    <div className="flex items-start justify-between gap-1.5 min-w-0">
                      <div className="flex items-center gap-2 min-w-0">
                        <div
                          className={`p-1.5 rounded-lg shrink-0 ${
                            isCurrent
                              ? "bg-sky-500/20 text-sky-400"
                              : "bg-slate-800 text-slate-400 group-hover:text-slate-300"
                          }`}
                        >
                          <Icon className="h-4 w-4" />
                        </div>
                        <div className="min-w-0">
                          <p className="text-xs font-semibold text-slate-200 truncate">
                            {preset.label}
                          </p>
                          <p className="text-[10px] text-slate-400 truncate mt-0.5">
                            {preset.sublabel}
                          </p>
                        </div>
                      </div>

                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          openGuide(preset.id);
                        }}
                        className="p-1 text-slate-500 hover:text-sky-400 rounded-md hover:bg-slate-800/80 transition-colors shrink-0 -mr-1 -mt-1"
                        title="Ver instruções de configuração desta conexão"
                      >
                        <HelpCircle className="h-3.5 w-3.5" />
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
                  className="inline-flex items-center gap-1 text-[11px] text-sky-400 hover:text-sky-300 hover:underline shrink-0"
                >
                  <HelpCircle className="h-3 w-3" />
                  <span className="hidden sm:inline">Como configurar o SSH no host remoto?</span>
                  <span className="sm:hidden">Guia SSH</span>
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
                  <ProvisionKeyPanel
                    serverHost={serverHost}
                    serverPort={serverPort}
                    defaultUser={sshUser}
                    onProvisioned={(key, user) => {
                      setSshPrivateKey(key);
                      setSshUser(user);
                      setSshKeyPassphrase("");
                      setTestResult(null);
                    }}
                  />
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

          {/* Security & Credential Storage Policy */}
          <div className="space-y-3 rounded-xl border border-slate-800 bg-slate-950/70 p-3.5">
            <div className="flex items-center space-x-2 border-b border-slate-800/80 pb-2">
              <Shield className="h-4 w-4 text-emerald-400" />
              <span className="text-xs font-semibold text-slate-200">
                Políticas de Credenciais e Segurança
              </span>
            </div>

            {/* Host Credentials Storage Choice */}
            <div>
              <label className="text-[11px] font-medium text-slate-300 block mb-1">
                Armazenamento de Credenciais do Host (SSH / TLS)
              </label>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                <button
                  type="button"
                  onClick={() => setCredentialPolicy("save_securely")}
                  className={`p-2.5 rounded-lg border text-left transition-all ${
                    credentialPolicy === "save_securely"
                      ? "bg-emerald-500/10 border-emerald-500/40 text-emerald-300"
                      : "bg-slate-900 border-slate-800 text-slate-400 hover:border-slate-700"
                  }`}
                >
                  <div className="text-xs font-medium flex items-center gap-1.5">
                    <Lock className="h-3.5 w-3.5" />
                    Salvar com Segurança
                  </div>
                  <div className="text-[10px] text-slate-400 mt-0.5">
                    Criptografado com AES-GCM no banco de dados. Nunca exposto em logs ou APIs.
                  </div>
                </button>

                <button
                  type="button"
                  onClick={() => setCredentialPolicy("always_prompt")}
                  className={`p-2.5 rounded-lg border text-left transition-all ${
                    credentialPolicy === "always_prompt"
                      ? "bg-amber-500/10 border-amber-500/40 text-amber-300"
                      : "bg-slate-900 border-slate-800 text-slate-400 hover:border-slate-700"
                  }`}
                >
                  <div className="text-xs font-medium flex items-center gap-1.5">
                    <Unlock className="h-3.5 w-3.5" />
                    Sempre Solicitar
                  </div>
                  <div className="text-[10px] text-slate-400 mt-0.5">
                    Zero armazenamento. Credenciais são mantidas apenas na sessão efêmera.
                  </div>
                </button>
              </div>
            </div>

            {/* Sudo Privilege Escalation Policy */}
            {connectionType === "local" || hostUrl.startsWith("unix://") ? (
              <div className="pt-2 border-t border-slate-800/80">
                <div className="rounded-lg border border-sky-500/20 bg-sky-500/10 p-3 text-xs text-sky-300 flex items-start gap-2">
                  <ShieldCheck className="h-4 w-4 shrink-0 text-sky-400 mt-0.5" />
                  <div>
                    <div className="font-semibold text-slate-200">Acesso Administrativo Direto (Host Root)</div>
                    <div className="text-[11px] text-slate-400 mt-0.5 leading-relaxed">
                      Este servidor local utiliza integração direta via namespace do host (<code className="text-sky-300">nsenter</code>). As operações de verificação e atualização de pacotes são executadas nativamente com privilégios de root, sem necessidade de senha de sudo.
                    </div>
                  </div>
                </div>
              </div>
            ) : (
              <div className="pt-2 border-t border-slate-800/80">
                <label className="text-[11px] font-medium text-slate-300 block mb-1">
                  Privilégios de Administrador (Sudo / Gerenciamento de Pacotes)
                </label>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                  <button
                    type="button"
                    onClick={() => setSudoPolicy("always_prompt")}
                    className={`p-2.5 rounded-lg border text-left transition-all ${
                      sudoPolicy === "always_prompt"
                        ? "bg-sky-500/10 border-sky-500/40 text-sky-300"
                        : "bg-slate-900 border-slate-800 text-slate-400 hover:border-slate-700"
                    }`}
                  >
                    <div className="text-xs font-medium flex items-center gap-1.5">
                      <KeyRound className="h-3.5 w-3.5" />
                      Sempre Solicitar Sudo
                    </div>
                    <div className="text-[10px] text-slate-400 mt-0.5">
                      Solicita a senha a cada verificação ou atualização que exigir root.
                    </div>
                  </button>

                  <button
                    type="button"
                    onClick={() => setSudoPolicy("save_securely")}
                    className={`p-2.5 rounded-lg border text-left transition-all ${
                      sudoPolicy === "save_securely"
                        ? "bg-emerald-500/10 border-emerald-500/40 text-emerald-300"
                        : "bg-slate-900 border-slate-800 text-slate-400 hover:border-slate-700"
                    }`}
                  >
                    <div className="text-xs font-medium flex items-center gap-1.5">
                      <Lock className="h-3.5 w-3.5" />
                      Salvar Sudo Criptografado
                    </div>
                    <div className="text-[10px] text-slate-400 mt-0.5">
                      Senha de sudo salva com criptografia AES-GCM para atualizações automáticas.
                    </div>
                  </button>
                </div>

                {sudoPolicy === "save_securely" && (
                  <div className="mt-2.5">
                    <label className="text-[11px] font-medium text-slate-400">
                      Senha de Sudo do Host
                    </label>
                    <Input
                      type="password"
                      placeholder="Digite a senha de sudo / root"
                      value={sudoPassword}
                      onChange={(e) => setSudoPassword(e.target.value)}
                      className="mt-1"
                    />
                    <p className="text-[10px] text-slate-500 mt-1">
                      Enviada ao comando sudo exclusivamente via stdin seguro. Nunca exposta em ps ou logs.
                    </p>
                  </div>
                )}
              </div>
            )}

            {/* Package Manager Preference */}
            <div className="space-y-1.5 pt-2 border-t border-slate-800">
              <label className="text-xs font-medium text-slate-300 flex items-center justify-between">
                <span>Gerenciador de Pacotes do Host</span>
                <span className="text-[10px] text-slate-500 font-normal">Opcional</span>
              </label>
              <select
                value={formPackageManager}
                onChange={(e) => setFormPackageManager(e.target.value)}
                className="w-full bg-slate-900 border border-slate-800 rounded-lg px-3 py-2 text-xs text-white focus:outline-none focus:ring-1 focus:ring-sky-500"
              >
                <option value="">Automático (Detectar ao checar atualizações)</option>
                <option value="apt">APT (Debian, Ubuntu, Linux Mint)</option>
                <option value="dnf">DNF (Fedora, RHEL 8+, Rocky, AlmaLinux)</option>
                <option value="yum">YUM (CentOS 7, RHEL 7)</option>
                <option value="apk">APK (Alpine Linux)</option>
                <option value="pacman">Pacman (Arch Linux, Manjaro)</option>
                <option value="zypper">Zypper (openSUSE, SLES)</option>
              </select>
              <p className="text-[10px] text-slate-500">
                Selecione manualmente caso seu nó execute uma distribuição customizada ou a auto-detecção falhe.
              </p>
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

      {/* Server Packages Management Modal */}
      {packageModalServer && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/75 backdrop-blur-sm animate-in fade-in duration-150">
          <div className="w-full max-w-4xl max-h-[90vh] flex flex-col rounded-2xl bg-slate-900 border border-slate-800 shadow-2xl overflow-hidden">
            {/* Header */}
            <div className="flex items-center justify-between p-5 border-b border-slate-800 bg-slate-950/60 shrink-0">
              <div className="flex items-center gap-3">
                <div className="p-2.5 rounded-xl bg-sky-500/10 border border-sky-500/20 text-sky-400">
                  <Package className="h-6 w-6" />
                </div>
                <div>
                  <h3 className="text-lg font-bold text-white flex items-center gap-2">
                    Gerenciador de Pacotes do Host
                    <span className="text-xs font-mono font-normal px-2 py-0.5 rounded bg-slate-800 text-slate-300">
                      {packageModalServer.name}
                    </span>
                  </h3>
                  <div className="text-xs text-slate-400 flex flex-wrap items-center gap-2 mt-1">
                    <div className="flex items-center gap-1.5">
                      <span className="text-slate-400">Gerenciador:</span>
                      <select
                        value={selectedPackageManager || packageStatus?.package_manager || packageModalServer.package_manager || ""}
                        onChange={(e) => handleChangePackageManager(e.target.value)}
                        disabled={isPackagesLoading}
                        className="bg-slate-950 border border-slate-700 hover:border-slate-600 rounded-md px-2 py-0.5 text-xs font-mono font-medium text-sky-400 focus:outline-none focus:ring-1 focus:ring-sky-500 cursor-pointer"
                        title="Selecione o gerenciador de pacotes do host"
                      >
                        <option value="">Auto-detectar</option>
                        <option value="apt">APT (Debian, Ubuntu, Mint)</option>
                        <option value="dnf">DNF (Fedora, RHEL 8+, Rocky, Alma)</option>
                        <option value="yum">YUM (CentOS 7, RHEL 7)</option>
                        <option value="apk">APK (Alpine Linux)</option>
                        <option value="pacman">Pacman (Arch Linux, Manjaro)</option>
                        <option value="zypper">Zypper (openSUSE, SLES)</option>
                      </select>
                    </div>
                    <span>•</span>
                    <span>
                      {packageStatus?.requires_sudo === false || packageModalServer?.host_url?.startsWith("unix://")
                        ? "Privilégios: ⚡ Root Direto (Host)"
                        : (packageStatus?.has_saved_sudo_password ?? packageModalServer.has_saved_sudo_password)
                        ? "Sudo: 🔒 Senha Salva"
                        : "Sudo: ⚡ Solicitar Sempre"}
                    </span>
                    {packageStatus?.last_checked && (
                      <>
                        <span>•</span>
                        <span>Última checagem: {new Date(packageStatus.last_checked).toLocaleString()}</span>
                      </>
                    )}
                  </div>
                </div>
              </div>

              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleTriggerCheckPackages()}
                  disabled={isPackagesLoading || isUpgrading}
                  className="border-slate-700 bg-slate-800/60 hover:bg-slate-700 text-slate-200"
                >
                  <RefreshCw className={`h-3.5 w-3.5 mr-1.5 ${isPackagesLoading && !isUpgrading ? "animate-spin" : ""}`} />
                  Verificar Atualizações
                </Button>
                <button
                  onClick={isUpgrading ? handleSendUpgradeToBackground : () => setPackageModalServer(null)}
                  className="p-1.5 text-slate-400 hover:text-white rounded-lg hover:bg-slate-800 transition"
                  title={isUpgrading ? "Minimizar / Manter atualização em segundo plano" : "Fechar"}
                >
                  ✕
                </button>
              </div>
            </div>

            {/* Notifications */}
            {packageActionSuccess && (
              <div className="mx-5 mt-4 p-3 rounded-xl bg-emerald-500/10 border border-emerald-500/20 text-emerald-300 text-xs flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400" />
                  <span>{packageActionSuccess}</span>
                </div>
                <button onClick={() => setPackageActionSuccess(null)} className="text-emerald-400 hover:underline">
                  ✕
                </button>
              </div>
            )}

            {packageActionError && (
              <div className="mx-5 mt-4 p-3 rounded-xl bg-rose-500/10 border border-rose-500/20 text-rose-300 text-xs flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
                  <span>{packageActionError}</span>
                </div>
                <button onClick={() => setPackageActionError(null)} className="text-rose-400 hover:underline">
                  ✕
                </button>
              </div>
            )}

            {/* Package Manager Fallback Selector Banner when unknown or failed */}
            {((!packageStatus?.package_manager || packageStatus?.package_manager === "unknown" || packageActionError?.includes("gerenciador")) &&
              (!selectedPackageManager || selectedPackageManager === "unknown")) && (
              <div className="mx-5 mt-4 p-3.5 rounded-xl bg-amber-500/10 border border-amber-500/30 text-amber-300 text-xs flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3">
                <div className="flex items-center gap-2.5">
                  <AlertCircle className="h-5 w-5 shrink-0 text-amber-400" />
                  <div>
                    <span className="font-semibold block text-white">Gerenciador de pacotes não identificado</span>
                    <span className="text-slate-300 text-[11px]">
                      A auto-detecção não localizou o gerenciador deste SO. Selecione-o no seletor para verificar e atualizar:
                    </span>
                  </div>
                </div>
                <div className="flex items-center gap-2 w-full sm:w-auto">
                  <select
                    value={selectedPackageManager}
                    onChange={(e) => handleChangePackageManager(e.target.value)}
                    className="bg-slate-950 border border-amber-500/40 rounded-lg px-2.5 py-1 text-xs font-mono font-medium text-white focus:outline-none focus:ring-1 focus:ring-amber-500"
                  >
                    <option value="">Selecione um gerenciador...</option>
                    <option value="apt">APT (Debian, Ubuntu, Mint)</option>
                    <option value="dnf">DNF (Fedora, RHEL 8+, Rocky, Alma)</option>
                    <option value="yum">YUM (CentOS 7, RHEL 7)</option>
                    <option value="apk">APK (Alpine Linux)</option>
                    <option value="pacman">Pacman (Arch Linux, Manjaro)</option>
                    <option value="zypper">Zypper (openSUSE, SLES)</option>
                  </select>
                  {selectedPackageManager && (
                    <Button
                      size="sm"
                      onClick={() => handleTriggerCheckPackages()}
                      disabled={isPackagesLoading}
                      className="bg-amber-600 hover:bg-amber-500 text-white text-xs whitespace-nowrap"
                    >
                      Verificar Agora
                    </Button>
                  )}
                </div>
              </div>
            )}

            {/* View Switcher button if previous logs exist */}
            {upgradeLogs.length > 0 && !isUpgradeConsoleOpen && (
              <div className="mx-5 mt-3 flex justify-end">
                <button
                  type="button"
                  onClick={() => setIsUpgradeConsoleOpen(true)}
                  className="text-xs text-sky-400 hover:text-sky-300 flex items-center gap-1.5 py-1 px-2.5 rounded-lg bg-sky-500/10 border border-sky-500/20 hover:bg-sky-500/20 transition cursor-pointer"
                >
                  <Terminal className="h-3.5 w-3.5" />
                  Ver saída da última atualização ({upgradeLogs.length} linhas)
                </button>
              </div>
            )}

            {isUpgradeConsoleOpen ? (
              /* --- LIVE UPGRADE PROGRESS AND RESULT CONSOLE --- */
              <div className="flex-1 flex flex-col p-5 overflow-hidden gap-4">
                {/* Console Status Bar */}
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-4 rounded-xl border border-slate-800 bg-slate-950/60 shrink-0">
                  <div className="flex items-center gap-3">
                    {isUpgrading ? (
                      <div className="p-2.5 rounded-xl bg-sky-500/10 border border-sky-500/20 text-sky-400 animate-spin">
                        <RefreshCw className="h-5 w-5" />
                      </div>
                    ) : upgradeResult?.success ? (
                      <div className="p-2.5 rounded-xl bg-emerald-500/10 border border-emerald-500/20 text-emerald-400">
                        <CheckCircle2 className="h-5 w-5" />
                      </div>
                    ) : (
                      <div className="p-2.5 rounded-xl bg-rose-500/10 border border-rose-500/20 text-rose-400">
                        <AlertCircle className="h-5 w-5" />
                      </div>
                    )}
                    <div>
                      <h4 className="text-sm font-bold text-white flex items-center gap-2">
                        {isUpgrading
                          ? "Atualizando Pacotes do Servidor..."
                          : upgradeResult?.success
                          ? "Atualização Concluída com Sucesso!"
                          : "Falha na Atualização de Pacotes"}
                        {isUpgrading && (
                          <span className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-mono bg-sky-500/20 text-sky-300 animate-pulse">
                            AO VIVO
                          </span>
                        )}
                      </h4>
                      <p className="text-xs text-slate-400 mt-0.5">
                        {isUpgrading
                          ? `Executando processo no host (${packageModalServer.name}). Acompanhe os logs abaixo:`
                          : upgradeResult?.message || "Processo finalizado."}
                      </p>
                    </div>
                  </div>

                  <div className="flex items-center gap-2 shrink-0">
                    {upgradeDuration > 0 && (
                      <span className="text-xs font-mono px-2.5 py-1 rounded-lg bg-slate-800 text-slate-300 border border-slate-700">
                        ⏱️ {upgradeDuration}s
                      </span>
                    )}
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={handleCopyUpgradeLogs}
                      disabled={upgradeLogs.length === 0}
                      className="border-slate-700 bg-slate-800/80 hover:bg-slate-700 text-xs text-slate-200"
                      title="Copiar saída do terminal"
                    >
                      {isLogsCopied ? (
                        <>
                          <Check className="h-3.5 w-3.5 mr-1 text-emerald-400" />
                          Copiado!
                        </>
                      ) : (
                        <>
                          <Copy className="h-3.5 w-3.5 mr-1 text-slate-400" />
                          Copiar Saída
                        </>
                      )}
                    </Button>
                  </div>
                </div>

                {/* Result Summary Cards (shown when process finished) */}
                {upgradeResult && (
                  <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 shrink-0">
                    <div
                      className={`p-3.5 rounded-xl border ${
                        upgradeResult.success
                          ? "border-emerald-500/30 bg-emerald-500/5"
                          : "border-rose-500/30 bg-rose-500/5"
                      }`}
                    >
                      <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 block">
                        Status Final
                      </span>
                      <span
                        className={`text-sm font-bold mt-1 block flex items-center gap-1.5 ${
                          upgradeResult.success ? "text-emerald-400" : "text-rose-400"
                        }`}
                      >
                        {upgradeResult.success ? (
                          <>
                            <CheckCircle2 className="h-4 w-4 shrink-0" />
                            Concluído com Sucesso
                          </>
                        ) : (
                          <>
                            <AlertCircle className="h-4 w-4 shrink-0" />
                            Ocorreram Erros
                          </>
                        )}
                      </span>
                    </div>

                    <div className="p-3.5 rounded-xl border border-slate-800 bg-slate-900/60">
                      <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 block">
                        Duração Total
                      </span>
                      <span className="text-sm font-bold text-white mt-1 block font-mono">
                        {upgradeDuration} segundo(s)
                      </span>
                    </div>

                    <div className="p-3.5 rounded-xl border border-slate-800 bg-slate-900/60">
                      <span className="text-[11px] font-semibold uppercase tracking-wider text-slate-400 block">
                        Pacotes Restantes
                      </span>
                      <div className="flex items-center gap-2 mt-1">
                        <span className="text-sm font-bold text-white font-mono">
                          {upgradeResult.remaining_updates_count ?? 0}
                        </span>
                        <span className="text-xs text-slate-400">
                          ({upgradeResult.security_updates_count ?? 0} segurança)
                        </span>
                      </div>
                    </div>
                  </div>
                )}

                {/* Live Terminal Window */}
                <div className="flex-1 flex flex-col rounded-xl border border-slate-800 bg-slate-950 overflow-hidden shadow-2xl min-h-[260px]">
                  <div className="flex items-center justify-between px-3 py-2 bg-slate-900/90 border-b border-slate-800 shrink-0">
                    <div className="flex items-center gap-2">
                      <div className="flex items-center gap-1.5 mr-2">
                        <div className="w-2.5 h-2.5 rounded-full bg-rose-500/80" />
                        <div className="w-2.5 h-2.5 rounded-full bg-amber-500/80" />
                        <div className="w-2.5 h-2.5 rounded-full bg-emerald-500/80" />
                      </div>
                      <Terminal className="h-3.5 w-3.5 text-sky-400" />
                      <span className="text-xs font-mono text-slate-300">
                        root@{packageModalServer.name}:~# upgrade-packages
                      </span>
                    </div>
                    <span className="text-[10px] font-mono text-slate-500">
                      {upgradeLogs.length} linha(s) de saída
                    </span>
                  </div>

                  <div className="flex-1 p-4 font-mono text-xs overflow-y-auto space-y-1 bg-slate-950/95 scrollbar-thin scrollbar-thumb-slate-800">
                    {upgradeLogs.length === 0 ? (
                      <p className="text-slate-500 italic flex items-center gap-2">
                        <RefreshCw className="h-3.5 w-3.5 animate-spin text-sky-400" />
                        Conectando ao host e iniciando comando de atualização...
                      </p>
                    ) : (
                      upgradeLogs.map((log, index) => {
                        let levelBadge = "";
                        let color = "text-slate-300";
                        if (log.level === "info") {
                          levelBadge = "[INFO] ";
                          color = "text-sky-400 font-semibold";
                        } else if (log.level === "stderr" || log.level === "error") {
                          levelBadge = "[ERR] ";
                          color = "text-rose-400";
                        }

                        return (
                          <div key={index} className="flex items-start gap-2 leading-relaxed break-all">
                            <span className="text-slate-600 select-none shrink-0 text-[10px]">
                              {log.timestamp ? new Date(log.timestamp).toLocaleTimeString() : ""}
                            </span>
                            <span className={color}>
                              {levelBadge}
                              {log.message}
                            </span>
                          </div>
                        );
                      })
                    )}
                    {isUpgrading && (
                      <div className="flex items-center gap-2 text-sky-400 text-xs font-mono pt-1">
                        <span className="inline-block w-2 h-3.5 bg-sky-400 animate-pulse" />
                      </div>
                    )}
                    <div ref={upgradeConsoleBottomRef} />
                  </div>
                </div>
              </div>
            ) : (
              /* --- NORMAL UPDATES LIST VIEW --- */
              <>
                {/* Updates Summary Banner */}
                <div className="p-5 grid grid-cols-1 sm:grid-cols-3 gap-3 border-b border-slate-800/80 shrink-0 bg-slate-950/20">
                  <div className="p-3 rounded-xl border border-slate-800 bg-slate-900/60">
                    <span className="text-[11px] font-medium text-slate-400 uppercase tracking-wider block">
                      Atualizações Disponíveis
                    </span>
                    <span className="text-2xl font-bold text-white mt-1 block">
                      {packageStatus?.available_updates_count ?? packageStatus?.updates_count ?? packageModalServer.available_updates_count ?? 0}
                    </span>
                  </div>

                  <div className="p-3 rounded-xl border border-slate-800 bg-slate-900/60">
                    <span className="text-[11px] font-medium text-slate-400 uppercase tracking-wider block">
                      Atualizações de Segurança
                    </span>
                    <span className={`text-2xl font-bold mt-1 block ${
                      (packageStatus?.security_updates_count || packageStatus?.security_count || packageModalServer.security_updates_count || 0) > 0
                        ? "text-rose-400"
                        : "text-emerald-400"
                    }`}>
                      {packageStatus?.security_updates_count ?? packageStatus?.security_count ?? packageModalServer.security_updates_count ?? 0}
                    </span>
                  </div>

                  <div className="p-3 rounded-xl border border-slate-800 bg-slate-900/60 flex flex-col justify-between">
                    <span className="text-[11px] font-medium text-slate-400 uppercase tracking-wider block">
                      Ação Rápida
                    </span>
                    <Button
                      size="sm"
                      onClick={() => handleTriggerUpgradePackages()}
                      disabled={isPackagesLoading || isUpgrading || ((packageStatus?.available_updates_count ?? packageStatus?.updates_count ?? packageModalServer.available_updates_count ?? 0) === 0)}
                      className="bg-emerald-600 hover:bg-emerald-500 text-white shadow-lg shadow-emerald-600/20 mt-1"
                    >
                      <Download className="h-3.5 w-3.5 mr-1.5" />
                      {selectedPackages.length > 0
                        ? `Atualizar Selecionados (${selectedPackages.length})`
                        : "Atualizar Todos os Pacotes"}
                    </Button>
                  </div>
                </div>

                {/* Package Updates List Table */}
                <div className="flex-1 overflow-y-auto p-5">
                  {isPackagesLoading && !packageStatus ? (
                    <div className="flex flex-col items-center justify-center p-12 text-slate-400">
                      <RefreshCw className="h-8 w-8 animate-spin text-sky-400 mb-3" />
                      <p className="text-sm">Carregando status dos pacotes...</p>
                    </div>
                  ) : !packageStatus || hostUpdates.length === 0 ? (
                    <div className="flex flex-col items-center justify-center p-12 text-slate-400 border border-slate-800/80 rounded-2xl bg-slate-950/30">
                      <CheckCircle2 className="h-12 w-12 text-emerald-400 mb-3" />
                      <h4 className="text-base font-semibold text-slate-200">Todos os pacotes estão em dia!</h4>
                      <p className="text-xs text-slate-500 mt-1 max-w-sm text-center">
                        Nenhuma atualização pendente detectada para este sistema operacional.
                      </p>
                    </div>
                  ) : (
                    <div className="rounded-xl border border-slate-800 overflow-hidden bg-slate-950/40">
                      <div className="flex items-center justify-between p-3 bg-slate-900/80 border-b border-slate-800 text-xs">
                        <div className="flex items-center gap-2">
                          <input
                            type="checkbox"
                            id="select-all-pkgs"
                            checked={
                              selectedPackages.length > 0 &&
                              selectedPackages.length === hostUpdates.length
                            }
                            onChange={(e) => {
                              if (e.target.checked) {
                                setSelectedPackages(hostUpdates.map((p) => p.name));
                              } else {
                                setSelectedPackages([]);
                              }
                            }}
                            className="rounded border-slate-700 text-sky-500 focus:ring-sky-500"
                          />
                          <label htmlFor="select-all-pkgs" className="text-slate-300 font-medium cursor-pointer">
                            Selecionar Todos ({hostUpdates.length} pacotes)
                          </label>
                        </div>

                        {selectedPackages.length > 0 && (
                          <span className="text-sky-400 font-mono text-[11px]">
                            {selectedPackages.length} selecionado(s)
                          </span>
                        )}
                      </div>

                      <div className="divide-y divide-slate-800/60 max-h-96 overflow-y-auto">
                        {hostUpdates.map((pkg) => {
                          const isSelected = selectedPackages.includes(pkg.name);
                          return (
                            <div
                              key={pkg.name}
                              className={`flex items-center justify-between p-3 transition hover:bg-slate-900/50 ${
                                isSelected ? "bg-sky-500/5" : ""
                              }`}
                            >
                              <div className="flex items-center gap-3">
                                <input
                                  type="checkbox"
                                  checked={isSelected}
                                  onChange={(e) => {
                                    if (e.target.checked) {
                                      setSelectedPackages((prev) => [...prev, pkg.name]);
                                    } else {
                                      setSelectedPackages((prev) => prev.filter((n) => n !== pkg.name));
                                    }
                                  }}
                                  className="rounded border-slate-700 text-sky-500 focus:ring-sky-500"
                                />
                                <div>
                                  <div className="flex items-center gap-2">
                                    <span className="font-semibold text-white text-xs font-mono">
                                      {pkg.name}
                                    </span>
                                    {pkg.is_security && (
                                      <span className="inline-flex items-center gap-1 rounded bg-rose-500/10 border border-rose-500/30 px-1.5 py-0.2 text-[10px] font-bold text-rose-400">
                                        <ShieldAlert className="h-3 w-3" />
                                        SEGURANÇA
                                      </span>
                                    )}
                                  </div>
                                  <div className="text-[11px] font-mono text-slate-400 mt-0.5">
                                    <span className="text-slate-500">{pkg.current_version || "instalado"}</span>
                                    <span className="mx-1.5 text-slate-600">→</span>
                                    <span className="text-emerald-400 font-medium">{pkg.new_version}</span>
                                    {pkg.repository && (
                                      <span className="text-slate-500 ml-2">({pkg.repository})</span>
                                    )}
                                  </div>
                                </div>
                              </div>

                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => {
                                  setSelectedPackages([pkg.name]);
                                  handleTriggerUpgradePackages([pkg.name]);
                                }}
                                disabled={isPackagesLoading || isUpgrading}
                                className="text-xs text-sky-400 hover:text-white"
                              >
                                Atualizar
                              </Button>
                            </div>
                          );
                        })}
                      </div>
                    </div>
                  )}
                </div>
              </>
            )}

            {/* Footer */}
            <div className="p-4 border-t border-slate-800 bg-slate-950/60 flex items-center justify-between shrink-0">
              <span className="text-[11px] text-slate-500">
                {isUpgradeConsoleOpen && isUpgrading
                  ? "Atualização em andamento. Você pode enviar para segundo plano e retomar a qualquer momento."
                  : "Executado com privilégios restritos e sanitização anti-injeção de comandos."}
              </span>
              <div className="flex items-center gap-2">
                {isUpgrading ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={handleSendUpgradeToBackground}
                    className="border-sky-500/40 bg-sky-500/10 hover:bg-sky-500/20 text-sky-300"
                    title="Manter a atualização executando no host em segundo plano e fechar esta janela"
                  >
                    <Minimize2 className="h-3.5 w-3.5 mr-1.5" />
                    Segundo Plano
                  </Button>
                ) : (
                  <>
                    {isUpgradeConsoleOpen && (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => setIsUpgradeConsoleOpen(false)}
                        className="border-slate-700 bg-slate-800 hover:bg-slate-700 text-slate-200"
                      >
                        <Package className="h-3.5 w-3.5 mr-1.5" />
                        Voltar para Lista
                      </Button>
                    )}
                    <Button
                      variant={isUpgradeConsoleOpen && upgradeResult?.success ? "default" : "outline"}
                      size="sm"
                      onClick={() => setPackageModalServer(null)}
                      className={
                        isUpgradeConsoleOpen && upgradeResult?.success
                          ? "bg-emerald-600 hover:bg-emerald-500 text-white"
                          : "border-slate-700 text-slate-300"
                      }
                    >
                      {isUpgradeConsoleOpen && upgradeResult?.success ? "Concluir" : "Fechar"}
                    </Button>
                  </>
                )}
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Sudo Password Prompt Dialog */}
      {isSudoPromptOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm animate-in fade-in duration-150">
          <div className="w-full max-w-md rounded-2xl bg-slate-900 border border-slate-800 p-6 shadow-2xl space-y-4">
            <div className="flex items-center gap-3">
              <div className="p-2.5 rounded-xl bg-amber-500/10 border border-amber-500/20 text-amber-400">
                <KeyRound className="h-6 w-6" />
              </div>
              <div>
                <h3 className="text-base font-bold text-white">
                  Autenticação Sudo Necessária
                </h3>
                <p className="text-xs text-slate-400 mt-0.5">
                  Host: <strong className="text-slate-200">{packageModalServer?.name}</strong>
                </p>
              </div>
            </div>

            <p className="text-xs text-slate-300 leading-relaxed">
              A operação solicitada ({sudoPromptPendingAction === "check" ? "verificar repositórios" : "atualizar pacotes"}) exige privilégios de superusuário (<code className="text-sky-400 font-mono">sudo</code>).
            </p>

            <form onSubmit={handleSudoPromptSubmit} className="space-y-4">
              <div>
                <label className="text-[11px] font-semibold text-slate-300 uppercase tracking-wider block mb-1">
                  Senha de Sudo
                </label>
                <Input
                  type="password"
                  placeholder="Digite a senha de sudo / root"
                  value={promptSudoPassword}
                  onChange={(e) => setPromptSudoPassword(e.target.value)}
                  className="bg-slate-950 border-slate-800 text-white"
                  autoFocus
                  required
                />
                <p className="text-[10px] text-slate-500 mt-1">
                  A senha é transmitida com segurança via stdin e não fica visível em logs ou argumentos de processo.
                </p>
              </div>

              <div className="flex items-start gap-2 p-3 rounded-xl bg-slate-950 border border-slate-800">
                <input
                  type="checkbox"
                  id="save-sudo-choice"
                  checked={promptSaveSudo}
                  onChange={(e) => setPromptSaveSudo(e.target.checked)}
                  className="rounded border-slate-700 text-sky-500 focus:ring-sky-500 mt-0.5"
                />
                <label htmlFor="save-sudo-choice" className="text-xs text-slate-300 cursor-pointer">
                  Salvar esta senha criptografada para não precisar digitar novamente neste host
                </label>
              </div>

              <div className="flex items-center justify-end gap-2.5 pt-2 border-t border-slate-800">
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setIsSudoPromptOpen(false);
                    setPromptSudoPassword("");
                    setSudoPromptPendingAction(null);
                  }}
                  className="border-slate-700 text-slate-300"
                >
                  Cancelar
                </Button>
                <Button
                  type="submit"
                  className="bg-sky-500 hover:bg-sky-400 text-white shadow-lg shadow-sky-500/20"
                >
                  Confirmar e Executar
                </Button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Source Directories Modal */}
      {selectedSourceServer && (
        <SourceDirsModal
          isOpen={sourceModalOpen}
          onClose={() => {
            setSourceModalOpen(false);
            setSelectedSourceServer(null);
          }}
          serverId={selectedSourceServer.id}
          serverName={selectedSourceServer.name}
          onScanCompleted={refreshServers}
        />
      )}
    </div>
  );
}
