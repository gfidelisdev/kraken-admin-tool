"use client";

import React, { useState, useEffect, useMemo, useRef, useCallback } from "react";
import {
  getPlugins,
  getStorePlugins,
  inspectPluginUpload,
  inspectPluginURL,
  installPluginUpload,
  installPluginURL,
  enablePlugin,
  disablePlugin,
  deletePlugin,
  getCatalogSources,
  addCatalogSource,
  deleteCatalogSource,
  syncCatalogSource,
  getTrustedKeys,
  addTrustedKey,
  deleteTrustedKey,
  getAIGuide,
  rebuildPlugin,
} from "@/lib/api";
import {
  InstalledPlugin,
  PluginCatalogItem,
  PluginCatalogSource,
  PluginTrustedKey,
  PluginInspectResponse,
} from "@/types";
import { useAuth } from "@/context/AuthContext";
import { usePlugins } from "@/context/PluginContext";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Modal } from "@/components/ui/Modal";
import { SortControl } from "@/components/ui/SortControl";
import { sortItems, SortDirection } from "@/lib/sort";
import {
  Puzzle,
  Download,
  Upload,
  Link as LinkIcon,
  ShoppingBag,
  ShieldCheck,
  ShieldAlert,
  Play,
  Square,
  Trash2,
  ExternalLink,
  RefreshCw,
  Search,
  CheckCircle2,
  AlertCircle,
  FileCode,
  KeyRound,
  Layers,
  Info,
  Server,
  Plus,
  BookOpen,
  Bot,
  Copy,
  Check,
  FileDown,
  Sparkles,
  Hammer,
  Zap,
} from "lucide-react";
import Link from "next/link";

export default function PluginsPage() {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const { refreshPlugins: notifyContextRefresh } = usePlugins();

  const [activeTab, setActiveTab] = useState<"installed" | "store" | "install" | "keys" | "ai-guide">("installed");

  // Installed Plugins state
  const [plugins, setPlugins] = useState<InstalledPlugin[]>([]);
  const [isLoadingPlugins, setIsLoadingPlugins] = useState(true);
  const [pluginSearch, setPluginSearch] = useState("");
  const [sortKey, setSortKey] = useState<string>("name");
  const [sortDirection, setSortDirection] = useState<SortDirection>("asc");
  const [actionLoadingId, setActionLoadingId] = useState<string | null>(null);

  // Store Catalog state
  const [storePlugins, setStorePlugins] = useState<PluginCatalogItem[]>([]);
  const [isLoadingStore, setIsLoadingStore] = useState(false);
  const [storeSearch, setStoreSearch] = useState("");
  const [catalogSources, setCatalogSources] = useState<PluginCatalogSource[]>([]);
  const [isManagingCatalogs, setIsManagingCatalogs] = useState(false);
  const [newCatalogName, setNewCatalogName] = useState("");
  const [newCatalogURL, setNewCatalogURL] = useState("");

  // Direct Install state
  const [installURL, setInstallURL] = useState("");
  const [expectedSHA, setExpectedSHA] = useState("");
  const [isInspecting, setIsInspecting] = useState(false);
  const [inspectResult, setInspectResult] = useState<PluginInspectResponse | null>(null);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [isInstalling, setIsInstalling] = useState(false);
  const [installSuccessMessage, setInstallSuccessMessage] = useState<string | null>(null);
  const [installErrorMessage, setInstallErrorMessage] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  // AI Guide state
  const [aiGuideMarkdown, setAiGuideMarkdown] = useState<string>("");
  const [isLoadingGuide, setIsLoadingGuide] = useState(false);
  const [hasCopiedGuide, setHasCopiedGuide] = useState(false);

  // Details Modal
  const [detailsModalPlugin, setDetailsModalPlugin] = useState<InstalledPlugin | PluginCatalogItem | null>(null);

  // Update Plugin Modal state
  const [updateTargetPlugin, setUpdateTargetPlugin] = useState<InstalledPlugin | null>(null);
  const [updateFile, setUpdateFile] = useState<File | null>(null);
  const [isUpdating, setIsUpdating] = useState(false);
  const [updateError, setUpdateError] = useState<string | null>(null);
  const [updateSuccess, setUpdateSuccess] = useState<string | null>(null);
  const [updateInspect, setUpdateInspect] = useState<PluginInspectResponse | null>(null);
  const [isInspectingUpdate, setIsInspectingUpdate] = useState(false);
  const [enableAfterUpdate, setEnableAfterUpdate] = useState(true);
  const updateFileInputRef = useRef<HTMLInputElement | null>(null);

  // Trusted Keys state
  const [trustedKeys, setTrustedKeys] = useState<PluginTrustedKey[]>([]);
  const [isLoadingKeys, setIsLoadingKeys] = useState(false);
  const [newKeyName, setNewKeyName] = useState("");
  const [newPublicKey, setNewPublicKey] = useState("");

  // Fetch AI Guide
  const fetchAIGuide = useCallback(async () => {
    try {
      setIsLoadingGuide(true);
      const res = await getAIGuide();
      if (res && res.content) {
        setAiGuideMarkdown(res.content);
        return;
      }
    } catch {
      // fallback to static file
    }

    try {
      const raw = await fetch("/docs/KRAKEN_PLUGIN_AGENT_GUIDE.md");
      if (raw.ok) {
        const text = await raw.text();
        setAiGuideMarkdown(text);
      }
    } catch {
      // ignore
    } finally {
      setIsLoadingGuide(false);
    }
  }, []);

  const handleCopyGuide = () => {
    if (!aiGuideMarkdown) return;
    navigator.clipboard.writeText(aiGuideMarkdown);
    setHasCopiedGuide(true);
    setTimeout(() => setHasCopiedGuide(false), 3000);
  };

  // Fetch installed plugins
  const fetchInstalledPlugins = useCallback(async () => {
    try {
      setIsLoadingPlugins(true);
      const res = await getPlugins();
      setPlugins(res.plugins || []);
    } catch {
      // ignore
    } finally {
      setIsLoadingPlugins(false);
    }
  }, []);

  // Fetch store plugins
  const fetchStorePlugins = useCallback(async () => {
    try {
      setIsLoadingStore(true);
      const res = await getStorePlugins();
      setStorePlugins(res.plugins || []);
      const cats = await getCatalogSources();
      setCatalogSources(cats || []);
    } catch {
      // ignore
    } finally {
      setIsLoadingStore(false);
    }
  }, []);

  // Fetch trusted keys
  const fetchKeys = useCallback(async () => {
    if (!isAdmin) return;
    try {
      setIsLoadingKeys(true);
      const keys = await getTrustedKeys();
      setTrustedKeys(keys || []);
    } catch {
      // ignore
    } finally {
      setIsLoadingKeys(false);
    }
  }, [isAdmin]);

  useEffect(() => {
    fetchInstalledPlugins();
  }, [fetchInstalledPlugins]);

  useEffect(() => {
    if (activeTab === "store") {
      fetchStorePlugins();
    } else if (activeTab === "keys") {
      fetchKeys();
    } else if (activeTab === "ai-guide") {
      if (!aiGuideMarkdown) fetchAIGuide();
    }
  }, [activeTab, fetchStorePlugins, fetchKeys, fetchAIGuide, aiGuideMarkdown]);

  // Actions on installed plugins
  const handleTogglePlugin = async (p: InstalledPlugin) => {
    setActionLoadingId(p.plugin_id);
    try {
      if (p.status === "enabled") {
        await disablePlugin(p.plugin_id);
      } else {
        await enablePlugin(p.plugin_id);
      }
      await fetchInstalledPlugins();
      await notifyContextRefresh();
    } catch (err: any) {
      alert(err?.message || "Falha ao alterar status do plugin");
    } finally {
      setActionLoadingId(null);
    }
  };

  const handleDeletePlugin = async (p: InstalledPlugin) => {
    if (!confirm(`Tem certeza que deseja desinstalar o plugin "${p.name}"?`)) return;
    setActionLoadingId(p.plugin_id);
    try {
      await deletePlugin(p.plugin_id);
      await fetchInstalledPlugins();
      await notifyContextRefresh();
    } catch (err: any) {
      alert(err?.message || "Falha ao desinstalar plugin");
    } finally {
      setActionLoadingId(null);
    }
  };

  const handleRebuildPlugin = async (p: InstalledPlugin) => {
    setActionLoadingId(p.plugin_id);
    try {
      const res = await rebuildPlugin(p.plugin_id);
      const isContainer = p.runtime_type === "container";
      const kindLabel = isContainer ? "Imagem do container" : "Módulo WASM";
      alert(`Plugin "${p.name}": ${kindLabel} reconstruído com sucesso via ${res.result?.compiler || "compilador integrado"}! (${((res.result?.size_bytes || 0) / 1024).toFixed(1)} KB)`);
      await fetchInstalledPlugins();
      await notifyContextRefresh();
    } catch (err: any) {
      alert(err?.message || "Falha ao recompilar plugin");
    } finally {
      setActionLoadingId(null);
    }
  };

  const handleSelectUpdateFile = async (file: File) => {
    setUpdateFile(file);
    setUpdateError(null);
    setUpdateSuccess(null);
    setUpdateInspect(null);
    try {
      setIsInspectingUpdate(true);
      const insp = await inspectPluginUpload(file);
      setUpdateInspect(insp);
    } catch (err: any) {
      setUpdateError(err?.message || "Falha ao inspecionar o arquivo do plugin.");
    } finally {
      setIsInspectingUpdate(false);
    }
  };

  const handleConfirmUpdate = async () => {
    if (!updateTargetPlugin || !updateFile) return;
    try {
      setIsUpdating(true);
      setUpdateError(null);
      setUpdateSuccess(null);

      const permissions = updateInspect?.manifest?.permissions || [];
      await installPluginUpload(updateFile, undefined, permissions, enableAfterUpdate);

      setUpdateSuccess(`Plugin "${updateTargetPlugin.name}" atualizado com sucesso!`);
      await fetchInstalledPlugins();
      await notifyContextRefresh();
      setTimeout(() => {
        setUpdateTargetPlugin(null);
        setUpdateFile(null);
        setUpdateInspect(null);
        setUpdateSuccess(null);
      }, 1500);
    } catch (err: any) {
      setUpdateError(err?.message || "Falha ao atualizar plugin.");
    } finally {
      setIsUpdating(false);
    }
  };

  // Direct Install: inspect file or URL
  const handleInspect = async (e: React.FormEvent) => {
    e.preventDefault();
    setInstallErrorMessage(null);
    setInstallSuccessMessage(null);
    setInspectResult(null);

    if (!selectedFile && !installURL.trim()) {
      setInstallErrorMessage("Selecione um arquivo .zip/.tar.gz ou digite uma URL direta para download.");
      return;
    }

    try {
      setIsInspecting(true);
      if (selectedFile) {
        const insp = await inspectPluginUpload(selectedFile);
        setInspectResult(insp);
      } else {
        const insp = await inspectPluginURL(installURL.trim());
        setInspectResult(insp);
      }
    } catch (err: any) {
      setInstallErrorMessage(err?.message || "Falha ao inspecionar pacote do plugin.");
    } finally {
      setIsInspecting(false);
    }
  };

  // Confirm and install
  const handleConfirmInstall = async () => {
    if (!inspectResult) return;
    try {
      setIsInstalling(true);
      setInstallErrorMessage(null);
      const manifest: any = inspectResult.manifest || (inspectResult as any).Manifest || {};
      const permissions: string[] = manifest.permissions || manifest.Permissions || [];
      const pluginName = manifest.name || manifest.Name || "Plugin";

      if (selectedFile) {
        await installPluginUpload(
          selectedFile,
          expectedSHA.trim() || undefined,
          permissions,
          true
        );
      } else {
        await installPluginURL(
          installURL.trim(),
          expectedSHA.trim() || undefined,
          permissions,
          true
        );
      }

      setInstallSuccessMessage(`Plugin "${pluginName}" instalado e ativado com sucesso!`);
      setInspectResult(null);
      setSelectedFile(null);
      setInstallURL("");
      setExpectedSHA("");
      await fetchInstalledPlugins();
      await notifyContextRefresh();
      setActiveTab("installed");
    } catch (err: any) {
      setInstallErrorMessage(err?.message || "Falha ao instalar plugin.");
    } finally {
      setIsInstalling(false);
    }
  };

  // 1-Click Install from Online Store
  const handleInstallFromStore = async (item: PluginCatalogItem) => {
    try {
      setActionLoadingId(item.id);
      await installPluginURL(item.download_url, item.sha256, item.permissions, true);
      await fetchStorePlugins();
      await fetchInstalledPlugins();
      await notifyContextRefresh();
    } catch (err: any) {
      alert(err?.message || "Falha ao instalar plugin da loja");
    } finally {
      setActionLoadingId(null);
    }
  };

  // Catalog Sources management
  const handleAddCatalog = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newCatalogURL.trim()) return;
    try {
      await addCatalogSource(newCatalogName.trim() || newCatalogURL.trim(), newCatalogURL.trim());
      setNewCatalogName("");
      setNewCatalogURL("");
      await fetchStorePlugins();
    } catch (err: any) {
      alert(err?.message || "Falha ao adicionar catálogo");
    }
  };

  const handleDeleteCatalog = async (id: number) => {
    try {
      await deleteCatalogSource(id);
      await fetchStorePlugins();
    } catch (err: any) {
      alert(err?.message || "Falha ao remover catálogo");
    }
  };

  const handleSyncCatalog = async (id: number) => {
    try {
      await syncCatalogSource(id);
      await fetchStorePlugins();
    } catch (err: any) {
      alert(err?.message || "Falha ao sincronizar catálogo");
    }
  };

  // Trusted Keys management
  const handleAddKey = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newKeyName.trim() || !newPublicKey.trim()) return;
    try {
      await addTrustedKey(newKeyName.trim(), newPublicKey.trim());
      setNewKeyName("");
      setNewPublicKey("");
      await fetchKeys();
    } catch (err: any) {
      alert(err?.message || "Falha ao registrar chave pública");
    }
  };

  const handleDeleteKey = async (id: number) => {
    try {
      await deleteTrustedKey(id);
      await fetchKeys();
    } catch (err: any) {
      alert(err?.message || "Falha ao excluir chave");
    }
  };

  // Filtered & sorted plugins
  const filteredInstalled = useMemo(() => {
    const list = plugins.filter((p) => {
      const q = pluginSearch.toLowerCase().trim();
      if (!q) return true;
      return (
        p.name.toLowerCase().includes(q) ||
        p.plugin_id.toLowerCase().includes(q) ||
        p.description.toLowerCase().includes(q) ||
        p.author.toLowerCase().includes(q)
      );
    });

    return sortItems(list, sortKey, sortDirection, {
      name: (p) => p.name,
      status: (p) => p.status,
      version: (p) => p.version,
      runtime: (p) => p.runtime_type,
    });
  }, [plugins, pluginSearch, sortKey, sortDirection]);

  const filteredStore = useMemo(() => {
    return storePlugins.filter((p) => {
      const q = storeSearch.toLowerCase().trim();
      if (!q) return true;
      return (
        p.name.toLowerCase().includes(q) ||
        p.id.toLowerCase().includes(q) ||
        p.description.toLowerCase().includes(q) ||
        p.author.toLowerCase().includes(q)
      );
    });
  }, [storePlugins, storeSearch]);

  const sortOptions = [
    { value: "name", label: "Nome" },
    { value: "status", label: "Status" },
    { value: "version", label: "Versão" },
    { value: "runtime", label: "Runtime" },
  ];

  return (
    <div className="space-y-6">
      {/* Top Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-white tracking-tight flex items-center gap-2.5">
            <Puzzle className="h-6 w-6 text-sky-400" />
            Central de Plugins & Extensões
          </h1>
          <p className="text-xs text-slate-400 mt-1">
            Gerencie extensões da plataforma, instale pacotes (.zip / .tar.gz) ou explore a Loja Online.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setActiveTab("ai-guide");
              if (!aiGuideMarkdown) fetchAIGuide();
            }}
            className="border-emerald-500/30 bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-300 shrink-0"
          >
            <Bot className="h-3.5 w-3.5 mr-1.5" />
            Guia para IAs (.md)
          </Button>
          {isAdmin && (
            <Button
              size="sm"
              onClick={() => setActiveTab("install")}
              className="bg-sky-500 hover:bg-sky-600 text-white shadow-lg shadow-sky-500/20 shrink-0"
            >
              <Upload className="h-3.5 w-3.5 mr-1.5" />
              Instalar Plugin
            </Button>
          )}
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              fetchInstalledPlugins();
              if (activeTab === "store") fetchStorePlugins();
            }}
            className="border-slate-700 bg-slate-900/60 hover:bg-slate-800 text-slate-200 shrink-0"
          >
            <RefreshCw className="h-3.5 w-3.5 mr-1.5" />
            Atualizar
          </Button>
        </div>
      </div>

      {/* Tabs Navigation */}
      <div className="flex flex-wrap border-b border-slate-800 gap-x-6 gap-y-2 text-xs font-medium">
        <button
          onClick={() => setActiveTab("installed")}
          className={`pb-3 flex items-center gap-2 border-b-2 transition ${
            activeTab === "installed"
              ? "border-sky-500 text-sky-400 font-semibold"
              : "border-transparent text-slate-400 hover:text-slate-200"
          }`}
        >
          <Puzzle className="h-4 w-4" />
          Plugins Instalados ({plugins.length})
        </button>

        <button
          onClick={() => setActiveTab("store")}
          className={`pb-3 flex items-center gap-2 border-b-2 transition ${
            activeTab === "store"
              ? "border-sky-500 text-sky-400 font-semibold"
              : "border-transparent text-slate-400 hover:text-slate-200"
          }`}
        >
          <ShoppingBag className="h-4 w-4" />
          Loja Online (Catálogos)
        </button>

        {isAdmin && (
          <button
            onClick={() => setActiveTab("install")}
            className={`pb-3 flex items-center gap-2 border-b-2 transition ${
              activeTab === "install"
                ? "border-sky-500 text-sky-400 font-semibold"
                : "border-transparent text-slate-400 hover:text-slate-200"
            }`}
          >
            <Upload className="h-4 w-4" />
            Instalar por URL / Arquivo
          </button>
        )}

        {isAdmin && (
          <button
            onClick={() => setActiveTab("keys")}
            className={`pb-3 flex items-center gap-2 border-b-2 transition ${
              activeTab === "keys"
                ? "border-sky-500 text-sky-400 font-semibold"
                : "border-transparent text-slate-400 hover:text-slate-200"
            }`}
          >
            <KeyRound className="h-4 w-4" />
            Chaves Confiáveis (Ed25519)
          </button>
        )}

        <button
          onClick={() => {
            setActiveTab("ai-guide");
            if (!aiGuideMarkdown) fetchAIGuide();
          }}
          className={`pb-3 flex items-center gap-2 border-b-2 transition ${
            activeTab === "ai-guide"
              ? "border-emerald-500 text-emerald-400 font-semibold"
              : "border-transparent text-slate-400 hover:text-slate-200"
          }`}
        >
          <Bot className="h-4 w-4" />
          Guia para Agentes de IA (.md)
        </button>
      </div>

      {/* TAB 1: INSTALLED PLUGINS */}
      {activeTab === "installed" && (
        <div className="space-y-4">
          {/* Toolbar */}
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
            <div className="relative w-full sm:w-72">
              <Search className="absolute left-3 top-2.5 h-3.5 w-3.5 text-slate-500" />
              <input
                type="text"
                placeholder="Buscar plugins instalados..."
                value={pluginSearch}
                onChange={(e) => setPluginSearch(e.target.value)}
                className="w-full bg-slate-900 border border-slate-800 rounded-lg pl-8 pr-3 py-1.5 text-xs text-white placeholder-slate-500 focus:outline-none focus:ring-1 focus:ring-sky-500"
              />
            </div>

            <SortControl
              options={sortOptions}
              currentKey={sortKey}
              currentDirection={sortDirection}
              onKeyChange={setSortKey}
              onDirectionToggle={() => setSortDirection((d) => (d === "asc" ? "desc" : "asc"))}
            />
          </div>

          {isLoadingPlugins ? (
            <div className="p-12 text-center text-slate-400 text-xs flex items-center justify-center gap-2">
              <RefreshCw className="h-4 w-4 animate-spin text-sky-400" />
              Carregando plugins instalados...
            </div>
          ) : filteredInstalled.length === 0 ? (
            <div className="p-12 text-center rounded-2xl border border-slate-800 bg-slate-900/40 text-slate-400 space-y-3">
              <Puzzle className="h-10 w-10 text-slate-600 mx-auto" />
              <div className="text-sm font-semibold text-white">Nenhum plugin instalado</div>
              <p className="text-xs max-w-sm mx-auto text-slate-400">
                Instale pacotes de extensão (.zip ou .tar.gz) diretamente ou navegue pelos plugins disponíveis na Loja Online.
              </p>
              <div className="pt-2 flex justify-center gap-3">
                <Button size="sm" onClick={() => setActiveTab("store")}>
                  <ShoppingBag className="h-3.5 w-3.5 mr-1.5" />
                  Explorar Loja Online
                </Button>
                {isAdmin && (
                  <Button variant="outline" size="sm" onClick={() => setActiveTab("install")}>
                    <Upload className="h-3.5 w-3.5 mr-1.5" />
                    Instalar Manualmente
                  </Button>
                )}
              </div>
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {filteredInstalled.map((p) => {
                let perms: string[] = [];
                try {
                  perms = JSON.parse(p.granted_permissions || "[]");
                } catch {}

                return (
                  <div
                    key={p.plugin_id}
                    className="p-5 rounded-2xl bg-slate-900 border border-slate-800 hover:border-slate-700/80 transition flex flex-col justify-between shadow-lg"
                  >
                    <div>
                      <div className="flex items-start justify-between gap-3 mb-2">
                        <div className="flex items-center gap-2.5">
                          <div className="p-2 rounded-xl bg-sky-500/10 border border-sky-500/20 text-sky-400">
                            <Puzzle className="h-5 w-5" />
                          </div>
                          <div>
                            <h3 className="text-sm font-bold text-white tracking-tight leading-snug">
                              {p.name}
                            </h3>
                            <span className="text-[11px] font-mono text-slate-400">
                              v{p.version} • {p.author || "Autor desconhecido"}
                            </span>
                          </div>
                        </div>

                        <Badge
                          variant={
                            p.status === "enabled"
                              ? "running"
                              : p.status === "error"
                              ? "error"
                              : "stopped"
                          }
                          className="text-[10px]"
                        >
                          {p.status === "enabled"
                            ? "Ativo"
                            : p.status === "error"
                            ? "Erro"
                            : "Desativado"}
                        </Badge>
                      </div>

                      <p className="text-xs text-slate-400 line-clamp-2 mt-2 mb-3">
                        {p.description || "Nenhuma descrição informada."}
                      </p>

                      <div className="flex flex-wrap gap-1.5 mb-4">
                        <span className="text-[10px] font-mono uppercase px-1.5 py-0.5 rounded bg-slate-800 text-sky-300 font-semibold">
                          {p.runtime_type === "wasm" ? "WASM Sandbox" : p.runtime_type === "container" ? "Docker Sidecar" : "UI Only"}
                        </span>
                        {p.signature_verified ? (
                          <span className="text-[10px] flex items-center gap-1 px-1.5 py-0.5 rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                            <ShieldCheck className="h-3 w-3" /> Assinado
                          </span>
                        ) : (
                          <span className="text-[10px] flex items-center gap-1 px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-300 border border-amber-500/20">
                            SHA-256 verificado
                          </span>
                        )}
                        {perms.length > 0 && (
                          <span className="text-[10px] px-1.5 py-0.5 rounded bg-slate-800 text-slate-300">
                            {perms.length} permissões
                          </span>
                        )}
                      </div>

                      {p.last_error && (
                        <div className="p-2 mb-3 rounded-lg bg-rose-500/10 border border-rose-500/20 text-rose-300 text-[11px]">
                          <strong>Erro:</strong> {p.last_error}
                        </div>
                      )}
                    </div>

                    <div className="pt-3 border-t border-slate-800 flex flex-wrap items-center justify-between gap-2">
                      <div className="flex flex-wrap items-center gap-1.5">
                        {p.status === "enabled" && (
                          <Link href={`/plugins/${encodeURIComponent(p.plugin_id)}`}>
                            <Button size="sm" variant="default" className="text-xs h-7 px-2.5 shrink-0">
                              <ExternalLink className="h-3 w-3 mr-1" />
                              Abrir
                            </Button>
                          </Link>
                        )}
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => setDetailsModalPlugin(p)}
                          className="text-xs h-7 px-2 text-slate-400 hover:text-white shrink-0"
                        >
                          Detalhes
                        </Button>
                      </div>

                      {isAdmin && (
                        <div className="flex flex-wrap items-center gap-1.5">
                          <Button
                            size="sm"
                            variant={p.status === "enabled" ? "outline" : "default"}
                            onClick={() => handleTogglePlugin(p)}
                            disabled={actionLoadingId === p.plugin_id}
                            className="text-xs h-7 px-2 shrink-0"
                          >
                            {actionLoadingId === p.plugin_id ? (
                              <RefreshCw className="h-3 w-3 animate-spin" />
                            ) : p.status === "enabled" ? (
                              <>
                                <Square className="h-3 w-3 mr-1" />
                                Desativar
                              </>
                            ) : (
                              <>
                                <Play className="h-3 w-3 mr-1" />
                                Ativar
                              </>
                            )}
                          </Button>

                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => {
                              setUpdateTargetPlugin(p);
                              setEnableAfterUpdate(p.status === "enabled");
                              setUpdateFile(null);
                              setUpdateError(null);
                              setUpdateSuccess(null);
                              setUpdateInspect(null);
                            }}
                            disabled={actionLoadingId === p.plugin_id}
                            className="text-xs h-7 px-2 text-indigo-400 hover:text-indigo-300 hover:bg-indigo-500/10 border-slate-800 shrink-0"
                            title="Atualizar plugin enviando um novo arquivo .zip / .tar.gz (mesmo com a mesma versão)"
                          >
                            <Upload className="h-3 w-3 mr-1" />
                            Atualizar
                          </Button>

                          {(p.runtime_type === "wasm" || p.runtime_type === "container") && (
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => handleRebuildPlugin(p)}
                              disabled={actionLoadingId === p.plugin_id}
                              className="text-xs h-7 px-2 text-sky-400 hover:text-sky-300 hover:bg-sky-500/10 border-slate-800 shrink-0"
                              title={p.runtime_type === "container" ? "Reconstruir imagem Docker a partir do Dockerfile do plugin" : "Recompilar WASM a partir dos códigos-fonte Go"}
                            >
                              <Hammer className="h-3 w-3 mr-1" />
                              {p.runtime_type === "container" ? "Reconstruir Imagem" : "Recompilar"}
                            </Button>
                          )}

                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => handleDeletePlugin(p)}
                            disabled={actionLoadingId === p.plugin_id}
                            className="text-xs h-7 px-2 text-rose-400 hover:text-rose-300 hover:bg-rose-500/10 shrink-0"
                            title="Desinstalar plugin"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* TAB 2: STORE / CATALOGS */}
      {activeTab === "store" && (
        <div className="space-y-4">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
            <div className="relative w-full sm:w-72">
              <Search className="absolute left-3 top-2.5 h-3.5 w-3.5 text-slate-500" />
              <input
                type="text"
                placeholder="Buscar na Loja de Plugins..."
                value={storeSearch}
                onChange={(e) => setStoreSearch(e.target.value)}
                className="w-full bg-slate-900 border border-slate-800 rounded-lg pl-8 pr-3 py-1.5 text-xs text-white placeholder-slate-500 focus:outline-none focus:ring-1 focus:ring-sky-500"
              />
            </div>

            {isAdmin && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setIsManagingCatalogs(!isManagingCatalogs)}
                className="text-xs"
              >
                <Server className="h-3.5 w-3.5 mr-1.5" />
                {isManagingCatalogs ? "Ocultar Fontes de Catálogo" : "Gerenciar URLs de Catálogo"}
              </Button>
            )}
          </div>

          {/* Catalog Sources Manager Drawer */}
          {isManagingCatalogs && isAdmin && (
            <div className="p-4 rounded-xl bg-slate-900/80 border border-slate-800 space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h4 className="text-xs font-bold text-white uppercase tracking-wider">
                    Fontes de Catálogo Online (index.json)
                  </h4>
                  <p className="text-[11px] text-slate-400">
                    Adicione URLs HTTPS de arquivos index.json com metadados de plugins externos.
                  </p>
                </div>
              </div>

              <form onSubmit={handleAddCatalog} className="flex flex-col sm:flex-row gap-2">
                <input
                  type="text"
                  placeholder="Nome do Catálogo (ex: Loja Comunitária)"
                  value={newCatalogName}
                  onChange={(e) => setNewCatalogName(e.target.value)}
                  className="bg-slate-950 border border-slate-800 rounded-lg px-3 py-1.5 text-xs text-white"
                />
                <input
                  type="url"
                  placeholder="URL do index.json (https://...)"
                  value={newCatalogURL}
                  onChange={(e) => setNewCatalogURL(e.target.value)}
                  required
                  className="flex-1 bg-slate-950 border border-slate-800 rounded-lg px-3 py-1.5 text-xs text-white"
                />
                <Button type="submit" size="sm" className="text-xs whitespace-nowrap">
                  <Plus className="h-3.5 w-3.5 mr-1" />
                  Adicionar Fonte
                </Button>
              </form>

              <div className="divide-y divide-slate-800/60 border border-slate-800/60 rounded-lg overflow-hidden">
                {catalogSources.map((cat) => (
                  <div key={cat.id} className="p-3 bg-slate-950/60 flex items-center justify-between gap-3 text-xs">
                    <div>
                      <span className="font-semibold text-white block">{cat.name}</span>
                      <span className="text-[11px] font-mono text-slate-400 block truncate max-w-md">{cat.url}</span>
                      {cat.last_synced_at && (
                        <span className="text-[10px] text-slate-500">
                          Sincronizado em: {new Date(cat.last_synced_at).toLocaleString()}
                        </span>
                      )}
                    </div>
                    <div className="flex items-center gap-1.5">
                      <Button size="sm" variant="ghost" onClick={() => handleSyncCatalog(cat.id)} title="Sincronizar">
                        <RefreshCw className="h-3.5 w-3.5 text-sky-400" />
                      </Button>
                      <Button size="sm" variant="ghost" onClick={() => handleDeleteCatalog(cat.id)} title="Remover">
                        <Trash2 className="h-3.5 w-3.5 text-rose-400" />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Store Items Grid */}
          {isLoadingStore ? (
            <div className="p-12 text-center text-slate-400 text-xs flex items-center justify-center gap-2">
              <RefreshCw className="h-4 w-4 animate-spin text-sky-400" />
              Carregando plugins da loja online...
            </div>
          ) : filteredStore.length === 0 ? (
            <div className="p-12 text-center rounded-2xl border border-slate-800 bg-slate-900/40 text-slate-400 text-xs">
              Nenhum plugin localizado nos catálogos consultados.
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {filteredStore.map((item) => (
                <div
                  key={item.id}
                  className="p-5 rounded-2xl bg-slate-900 border border-slate-800 hover:border-slate-700/80 transition flex flex-col justify-between shadow-lg"
                >
                  <div>
                    <div className="flex items-start justify-between gap-3 mb-2">
                      <div className="flex items-center gap-2.5">
                        <div className="p-2 rounded-xl bg-indigo-500/10 border border-indigo-500/20 text-indigo-400">
                          <ShoppingBag className="h-5 w-5" />
                        </div>
                        <div>
                          <h3 className="text-sm font-bold text-white tracking-tight">{item.name}</h3>
                          <span className="text-[11px] font-mono text-slate-400">
                            v{item.version} • {item.author}
                          </span>
                        </div>
                      </div>

                      {item.is_installed ? (
                        <Badge variant="running" className="text-[10px]">
                          Instalado
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="text-[10px]">
                          Disponível
                        </Badge>
                      )}
                    </div>

                    <p className="text-xs text-slate-400 line-clamp-2 mt-2 mb-3">
                      {item.description}
                    </p>

                    <div className="flex flex-wrap gap-1.5 mb-4">
                      <span className="text-[10px] font-mono uppercase px-1.5 py-0.5 rounded bg-slate-800 text-indigo-300 font-semibold">
                        {item.runtime_type === "wasm" ? "WASM Sandbox" : item.runtime_type === "container" ? "Docker Sidecar" : "UI Only"}
                      </span>
                      <span className="text-[10px] px-1.5 py-0.5 rounded bg-slate-800 text-slate-300">
                        {item.permissions?.length || 0} permissões
                      </span>
                    </div>
                  </div>

                  <div className="pt-3 border-t border-slate-800 flex flex-wrap items-center justify-between gap-2">
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => setDetailsModalPlugin(item)}
                      className="text-xs h-7 px-2 text-slate-400 hover:text-white shrink-0"
                    >
                      Ver Detalhes
                    </Button>

                    {isAdmin && (
                      <Button
                        size="sm"
                        disabled={actionLoadingId === item.id || (item.is_installed && !item.has_update)}
                        onClick={() => handleInstallFromStore(item)}
                        className="text-xs h-7 px-3 bg-indigo-600 hover:bg-indigo-500 text-white shrink-0"
                      >
                        {actionLoadingId === item.id ? (
                          <RefreshCw className="h-3 w-3 animate-spin mr-1" />
                        ) : item.is_installed && item.has_update ? (
                          <RefreshCw className="h-3 w-3 mr-1" />
                        ) : (
                          <Download className="h-3 w-3 mr-1" />
                        )}
                        {item.is_installed && item.has_update
                          ? "Atualizar"
                          : item.is_installed
                          ? "Instalado"
                          : "Instalar"}
                      </Button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* TAB 3: INSTALL BY URL / UPLOAD */}
      {activeTab === "install" && isAdmin && (
        <div className="max-w-2xl mx-auto space-y-6">
          <div className="p-6 rounded-2xl bg-slate-900 border border-slate-800 shadow-xl space-y-5">
            <div>
              <h3 className="text-base font-bold text-white flex items-center gap-2">
                <Upload className="h-5 w-5 text-sky-400" />
                Instalar Pacote de Plugin (.zip ou .tar.gz)
              </h3>
              <p className="text-xs text-slate-400 mt-1">
                Forneça a URL de download direto do arquivo compactado ou envie um arquivo do seu computador.
              </p>
            </div>

            {installSuccessMessage && (
              <div className="p-3.5 rounded-xl bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 text-xs flex items-center gap-2">
                <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400" />
                <span>{installSuccessMessage}</span>
              </div>
            )}

            {installErrorMessage && (
              <div className="p-3.5 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-center gap-2">
                <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
                <span>{installErrorMessage}</span>
              </div>
            )}

            <form onSubmit={handleInspect} className="space-y-4">
              {/* Option A: Direct URL */}
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-slate-300 flex items-center gap-1.5">
                  <LinkIcon className="h-3.5 w-3.5 text-sky-400" />
                  Opção 1: URL Direta do Pacote (.zip / .tar.gz)
                </label>
                <input
                  type="url"
                  placeholder="https://exemplo.com/releases/meu-plugin-1.0.0.tar.gz"
                  value={installURL}
                  onChange={(e) => {
                    setInstallURL(e.target.value);
                    if (e.target.value) setSelectedFile(null);
                  }}
                  disabled={Boolean(selectedFile)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-xs text-white placeholder-slate-500 focus:outline-none focus:ring-1 focus:ring-sky-500 disabled:opacity-50"
                />
              </div>

              <div className="flex items-center gap-4 text-xs text-slate-500">
                <span className="flex-1 border-t border-slate-800"></span>
                <span>OU</span>
                <span className="flex-1 border-t border-slate-800"></span>
              </div>

              {/* Option B: Local File Upload */}
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-slate-300 flex items-center gap-1.5">
                  <FileCode className="h-3.5 w-3.5 text-sky-400" />
                  Opção 2: Enviar Arquivo Local
                </label>
                <input
                  type="file"
                  ref={fileInputRef}
                  accept=".zip,.tar.gz,.tgz"
                  onChange={(e) => {
                    if (e.target.files?.[0]) {
                      setSelectedFile(e.target.files[0]);
                      setInstallURL("");
                    }
                  }}
                  className="hidden"
                />
                <div
                  onClick={() => fileInputRef.current?.click()}
                  className={`p-6 border-2 border-dashed rounded-xl text-center cursor-pointer transition ${
                    selectedFile
                      ? "border-sky-500/60 bg-sky-500/10"
                      : "border-slate-800 hover:border-slate-700 bg-slate-950/40"
                  }`}
                >
                  <Upload className="h-6 w-6 text-slate-400 mx-auto mb-2" />
                  {selectedFile ? (
                    <div>
                      <span className="text-xs font-semibold text-sky-300 block">{selectedFile.name}</span>
                      <span className="text-[11px] text-slate-400 font-mono">
                        {(selectedFile.size / 1024).toFixed(1)} KB
                      </span>
                    </div>
                  ) : (
                    <div>
                      <span className="text-xs text-slate-300 font-medium block">
                        Clique para selecionar um arquivo .zip ou .tar.gz
                      </span>
                      <span className="text-[10px] text-slate-500">Até 50 MB</span>
                    </div>
                  )}
                </div>
              </div>

              {/* Optional expected SHA-256 */}
              <div className="space-y-1.5 pt-2">
                <label className="text-xs font-medium text-slate-400">
                  Checksum SHA-256 Esperado (Opcional para validação de integridade)
                </label>
                <input
                  type="text"
                  placeholder="Ex: a1b2c3d4e5f6..."
                  value={expectedSHA}
                  onChange={(e) => setExpectedSHA(e.target.value)}
                  className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-1.5 text-xs font-mono text-white placeholder-slate-600 focus:outline-none focus:ring-1 focus:ring-sky-500"
                />
              </div>

              <div className="pt-2 flex justify-end">
                <Button type="submit" disabled={isInspecting || (!selectedFile && !installURL.trim())}>
                  {isInspecting ? (
                    <>
                      <RefreshCw className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                      Inspecionando Pacote...
                    </>
                  ) : (
                    <>
                      <ShieldCheck className="h-3.5 w-3.5 mr-1.5" />
                      Inspecionar e Avançar
                    </>
                  )}
                </Button>
              </div>
            </form>
          </div>

          {/* Inspection Modal / Confirmation Card */}
          {inspectResult && (() => {
            const manifest: any = inspectResult.manifest || (inspectResult as any).Manifest || {};
            const sha256 = inspectResult.sha256 || (inspectResult as any).SHA256 || "";
            const sizeBytes = inspectResult.size_bytes ?? (inspectResult as any).SizeBytes ?? 0;
            const archiveFormat = inspectResult.archive_format || (inspectResult as any).ArchiveFormat || (inspectResult as any).archive_type || "zip";
            const signatureVerified = inspectResult.signature_verified ?? (inspectResult as any).SignatureVerified ?? false;
            const signerName = inspectResult.signer_name || (inspectResult as any).SignerName || "";
            const permissions: string[] = manifest.permissions || manifest.Permissions || [];
            const pluginName = manifest.name || manifest.Name || "Plugin";
            const pluginVersion = manifest.version || manifest.Version || "1.0.0";
            const pluginDesc = manifest.description || manifest.Description || "";
            const pluginID = manifest.id || manifest.ID || "";
            const runtimeType = manifest.runtime?.type || manifest.Runtime?.Type || "none";

            return (
              <div className="p-6 rounded-2xl bg-slate-900 border border-sky-500/40 shadow-2xl space-y-5 animate-in fade-in">
                <div className="flex items-start justify-between">
                  <div>
                    <h4 className="text-base font-bold text-white flex items-center gap-2">
                      {pluginName}
                      <span className="text-xs font-mono text-sky-400 bg-sky-500/10 px-2 py-0.5 rounded">
                        v{pluginVersion}
                      </span>
                    </h4>
                    <p className="text-xs text-slate-400 mt-1">{pluginDesc}</p>
                  </div>

                  {signatureVerified ? (
                    <Badge variant="running" className="text-xs flex items-center gap-1">
                      <ShieldCheck className="h-3.5 w-3.5" /> Assinado por {signerName || "Chave Confiável"}
                    </Badge>
                  ) : (
                    <Badge variant="pending" className="text-xs flex items-center gap-1">
                      <AlertCircle className="h-3.5 w-3.5" /> Não Assinado
                    </Badge>
                  )}
                </div>

                <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 p-3.5 rounded-xl bg-slate-950 border border-slate-800 text-xs">
                  <div>
                    <span className="text-slate-500 block text-[10px]">ID do Plugin</span>
                    <span className="font-mono text-slate-200">{pluginID}</span>
                  </div>
                  <div>
                    <span className="text-slate-500 block text-[10px]">Runtime</span>
                    <span className="font-mono text-slate-200 uppercase">{runtimeType}</span>
                  </div>
                  <div>
                    <span className="text-slate-500 block text-[10px]">Formato</span>
                    <span className="font-mono text-slate-200 uppercase">{archiveFormat}</span>
                  </div>
                  <div>
                    <span className="text-slate-500 block text-[10px]">Tamanho</span>
                    <span className="font-mono text-slate-200">{(sizeBytes / 1024).toFixed(1)} KB</span>
                  </div>
                </div>

                {/* Auto-Build Banner */}
                {inspectResult.needs_compilation && (
                  <div className="p-3.5 rounded-xl bg-sky-950/60 border border-sky-500/40 text-xs flex items-start gap-3">
                    <Zap className="h-4 w-4 text-sky-400 mt-0.5 shrink-0" />
                    <div>
                      {manifest.runtime?.type === "container" ? (
                        <>
                          <span className="font-semibold text-sky-300 block">
                            Dockerfile detectado (Auto-Build de Imagem Docker ativo)
                          </span>
                          <p className="text-slate-300 text-[11px] mt-0.5">
                            O pacote do plugin fornece um Dockerfile e arquivos de contexto para compilação local.
                            O Kraken Admin Tool construirá a imagem do container automaticamente durante a importação/instalação, sem necessidade de publicar em registry público ou gerar imagens externas.
                          </p>
                        </>
                      ) : (
                        <>
                          <span className="font-semibold text-sky-300 block">
                            Código-fonte Go detectado (Auto-Build WASM ativo)
                          </span>
                          <p className="text-slate-300 text-[11px] mt-0.5">
                            O arquivo compactado contém fontes Go ({inspectResult.source_files?.length || 1} arquivo(s)) sem o binário final pré-compilado.
                            O Kraken Admin Tool compilará o módulo WebAssembly (WASI) automaticamente durante a instalação usando o compilador integrado.
                          </p>
                        </>
                      )}
                    </div>
                  </div>
                )}

                {/* Security & Permissions Review */}
                <div className="space-y-2">
                  <span className="text-xs font-bold text-white flex items-center gap-1.5">
                    <ShieldAlert className="h-4 w-4 text-amber-400" />
                    Permissões Solicitadas pelo Plugin:
                  </span>
                  {permissions && permissions.length > 0 ? (
                    <div className="flex flex-wrap gap-2">
                      {permissions.map((perm) => (
                        <span
                          key={perm}
                          className="text-xs font-mono px-2 py-1 rounded bg-slate-800 text-amber-300 border border-amber-500/20"
                        >
                          {perm}
                        </span>
                      ))}
                    </div>
                  ) : (
                    <p className="text-xs text-slate-400">Nenhuma permissão especial solicitada.</p>
                  )}
                </div>

                <div className="text-[11px] font-mono text-slate-500 truncate">
                  SHA-256: {sha256}
                </div>

                <div className="pt-3 border-t border-slate-800 flex items-center justify-end gap-3">
                  <Button variant="outline" size="sm" onClick={() => setInspectResult(null)}>
                    Cancelar
                  </Button>
                  <Button
                    size="sm"
                    onClick={handleConfirmInstall}
                    disabled={isInstalling}
                    className="bg-emerald-600 hover:bg-emerald-500 text-white"
                  >
                    {isInstalling ? (
                      <>
                        <RefreshCw className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                        Instalando...
                      </>
                    ) : (
                      <>
                        <CheckCircle2 className="h-3.5 w-3.5 mr-1.5" />
                        Confirmar e Ativar Plugin
                      </>
                    )}
                  </Button>
                </div>
              </div>
            );
          })()}
        </div>
      )}

      {/* TAB 4: TRUSTED KEYS */}
      {activeTab === "keys" && isAdmin && (
        <div className="max-w-2xl mx-auto space-y-6">
          <div className="p-6 rounded-2xl bg-slate-900 border border-slate-800 space-y-4">
            <div>
              <h3 className="text-base font-bold text-white flex items-center gap-2">
                <KeyRound className="h-5 w-5 text-sky-400" />
                Chaves Públicas Confiáveis (Ed25519)
              </h3>
              <p className="text-xs text-slate-400 mt-1">
                Cadastre chaves públicas de desenvolvedores confiáveis para verificar a autenticidade de pacotes assinados.
              </p>
            </div>

            <form onSubmit={handleAddKey} className="space-y-3">
              <input
                type="text"
                placeholder="Identificação da Chave (ex: Equipe de Segurança Oficial)"
                value={newKeyName}
                onChange={(e) => setNewKeyName(e.target.value)}
                required
                className="w-full bg-slate-950 border border-slate-800 rounded-lg px-3 py-2 text-xs text-white"
              />
              <textarea
                placeholder="Chave Pública Ed25519 (Base64 - 32 bytes)"
                value={newPublicKey}
                onChange={(e) => setNewPublicKey(e.target.value)}
                required
                rows={3}
                className="w-full bg-slate-950 border border-slate-800 rounded-lg p-3 text-xs font-mono text-white"
              />
              <Button type="submit" size="sm">
                <Plus className="h-3.5 w-3.5 mr-1.5" />
                Cadastrar Chave Confiável
              </Button>
            </form>
          </div>

          <div className="p-5 rounded-2xl bg-slate-900 border border-slate-800 space-y-3">
            <h4 className="text-xs font-bold text-slate-300 uppercase tracking-wider">
              Chaves Cadastradas ({trustedKeys.length})
            </h4>

            {isLoadingKeys ? (
              <div className="text-xs text-slate-500 py-4 text-center">Carregando chaves...</div>
            ) : trustedKeys.length === 0 ? (
              <div className="text-xs text-slate-500 py-4 text-center">Nenhuma chave pública cadastrada ainda.</div>
            ) : (
              <div className="divide-y divide-slate-800/60 border border-slate-800 rounded-xl overflow-hidden">
                {trustedKeys.map((k) => (
                  <div key={k.id} className="p-3 bg-slate-950/60 flex items-center justify-between gap-3 text-xs">
                    <div>
                      <span className="font-semibold text-white block">{k.name}</span>
                      <span className="text-[11px] font-mono text-sky-400 block">Fingerprint: {k.fingerprint}</span>
                    </div>
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => handleDeleteKey(k.id)}
                      className="text-rose-400 hover:text-rose-300"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* TAB 5: AI AGENT GUIDE */}
      {activeTab === "ai-guide" && (
        <div className="space-y-6 max-w-5xl mx-auto animate-in fade-in">
          {/* Header Card */}
          <div className="p-6 rounded-2xl bg-gradient-to-r from-emerald-950/40 via-slate-900 to-slate-900 border border-emerald-500/30 shadow-xl space-y-4">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
              <div>
                <div className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-[10px] font-semibold bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 mb-2">
                  <Sparkles className="h-3 w-3" />
                  Universal AI Agent Guide
                </div>
                <h3 className="text-lg font-bold text-white flex items-center gap-2">
                  <Bot className="h-5 w-5 text-emerald-400" />
                  Orientador de IAs para Criação de Plugins
                </h3>
                <p className="text-xs text-slate-300 mt-1 max-w-2xl leading-relaxed">
                  Copie este documento completo e cole em qualquer chat ou ferramenta de IA (Claude, ChatGPT, Gemini, DeepSeek, Cursor, Copilot) como instrução de sistema para que ela crie plugins de forma autônoma, padronizada e compatível em um projeto isolado.
                </p>
              </div>

              <div className="flex items-center gap-2 self-start sm:self-center shrink-0">
                <Button
                  size="sm"
                  onClick={handleCopyGuide}
                  className="bg-emerald-600 hover:bg-emerald-500 text-white shadow-lg shadow-emerald-600/20"
                >
                  {hasCopiedGuide ? (
                    <>
                      <Check className="h-3.5 w-3.5 mr-1.5" />
                      Copiado!
                    </>
                  ) : (
                    <>
                      <Copy className="h-3.5 w-3.5 mr-1.5" />
                      Copiar Prompt Completo
                    </>
                  )}
                </Button>

                <a
                  href="/docs/KRAKEN_PLUGIN_AGENT_GUIDE.md"
                  download="KRAKEN_PLUGIN_AGENT_GUIDE.md"
                  className="inline-flex"
                >
                  <Button variant="outline" size="sm" className="border-slate-700 bg-slate-900/80 hover:bg-slate-800 text-slate-200">
                    <FileDown className="h-3.5 w-3.5 mr-1.5" />
                    Baixar .md
                  </Button>
                </a>
              </div>
            </div>

            {/* Quick Architecture Callouts */}
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 pt-2 text-xs">
              <div className="p-3 rounded-xl bg-slate-950/60 border border-slate-800 space-y-1">
                <span className="font-semibold text-emerald-400 block flex items-center gap-1.5">
                  <ShieldCheck className="h-3.5 w-3.5" />
                  1. Sandbox Web Worker
                </span>
                <span className="text-[11px] text-slate-400 block">
                  Sem acesso ao DOM ou localStorage. Renderização de UI declarativa via JSON nativo.
                </span>
              </div>

              <div className="p-3 rounded-xl bg-slate-950/60 border border-slate-800 space-y-1">
                <span className="font-semibold text-sky-400 block flex items-center gap-1.5">
                  <Layers className="h-3.5 w-3.5" />
                  2. WASI Sandbox (Go / Rust)
                </span>
                <span className="text-[11px] text-slate-400 block">
                  Compilação para WebAssembly via GOOS=wasip1 GOARCH=wasm. I/O por stdin/stdout JSON.
                </span>
              </div>

              <div className="p-3 rounded-xl bg-slate-950/60 border border-slate-800 space-y-1">
                <span className="font-semibold text-amber-400 block flex items-center gap-1.5">
                  <Upload className="h-3.5 w-3.5" />
                  3. Empacotamento Limpo
                </span>
                <span className="text-[11px] text-slate-400 block">
                  Compacte a pasta em .zip ou .tar.gz contendo plugin.json, README.md, frontend/ e backend/.
                </span>
              </div>
            </div>
          </div>

          {/* Full Markdown Viewer */}
          <div className="p-6 rounded-2xl bg-slate-900/90 border border-slate-800 shadow-xl space-y-3">
            <div className="flex items-center justify-between pb-2 border-b border-slate-800">
              <span className="text-xs font-mono text-slate-400 flex items-center gap-1.5">
                <FileCode className="h-3.5 w-3.5 text-sky-400" />
                KRAKEN_PLUGIN_AGENT_GUIDE.md (Instruções Completas para a IA)
              </span>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleCopyGuide}
                className="text-xs text-slate-400 hover:text-white h-7 px-2"
              >
                {hasCopiedGuide ? <Check className="h-3 w-3 mr-1 text-emerald-400" /> : <Copy className="h-3 w-3 mr-1" />}
                {hasCopiedGuide ? "Copiado!" : "Copiar"}
              </Button>
            </div>

            {isLoadingGuide ? (
              <div className="p-12 text-center text-slate-400 text-xs flex items-center justify-center gap-2">
                <RefreshCw className="h-4 w-4 animate-spin text-sky-400" />
                Carregando guia orientador de IAs...
              </div>
            ) : (
              <div className="max-h-[600px] overflow-y-auto p-4 rounded-xl bg-slate-950 border border-slate-800/80 font-mono text-xs text-slate-300 whitespace-pre-wrap leading-relaxed select-text">
                {aiGuideMarkdown || "Guia não carregado. Clique em atualizar."}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Details Modal */}
      {detailsModalPlugin && (
        <Modal
          isOpen={Boolean(detailsModalPlugin)}
          onClose={() => setDetailsModalPlugin(null)}
          title={detailsModalPlugin.name}
          description={`ID: ${detailsModalPlugin.id || (detailsModalPlugin as any).plugin_id} • v${detailsModalPlugin.version}`}
        >
          <div className="space-y-4 max-h-[70vh] overflow-y-auto pr-1">
            <p className="text-xs text-slate-300">{detailsModalPlugin.description}</p>

            <div className="p-3.5 rounded-xl bg-slate-950 border border-slate-800 text-xs grid grid-cols-2 gap-2">
              <div>
                <span className="text-slate-500 text-[10px] block">Autor:</span>
                <span className="text-white">{detailsModalPlugin.author || "Não informado"}</span>
              </div>
              <div>
                <span className="text-slate-500 text-[10px] block">Licença:</span>
                <span className="text-white">{detailsModalPlugin.license || "Não informada"}</span>
              </div>
              {detailsModalPlugin.homepage && (
                <div className="col-span-2">
                  <span className="text-slate-500 text-[10px] block">Website / Repositório:</span>
                  <a
                    href={detailsModalPlugin.homepage}
                    target="_blank"
                    rel="noreferrer"
                    className="text-sky-400 hover:underline flex items-center gap-1 font-mono text-xs"
                  >
                    {detailsModalPlugin.homepage}
                    <ExternalLink className="h-3 w-3" />
                  </a>
                </div>
              )}
            </div>

            {"readme_markdown" in detailsModalPlugin && detailsModalPlugin.readme_markdown && (
              <div className="space-y-1.5 pt-2 border-t border-slate-800">
                <span className="text-xs font-bold text-white flex items-center gap-1.5">
                  <BookOpen className="h-3.5 w-3.5 text-sky-400" />
                  Documentação (README):
                </span>
                <div className="p-3.5 rounded-xl bg-slate-950 border border-slate-800 text-xs text-slate-300 font-mono whitespace-pre-wrap">
                  {detailsModalPlugin.readme_markdown}
                </div>
              </div>
            )}

            <div className="pt-3 border-t border-slate-800 flex items-center justify-between gap-2">
              {"plugin_id" in detailsModalPlugin && isAdmin ? (
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    const target = detailsModalPlugin as InstalledPlugin;
                    setDetailsModalPlugin(null);
                    setUpdateTargetPlugin(target);
                    setEnableAfterUpdate(target.status === "enabled");
                    setUpdateFile(null);
                    setUpdateError(null);
                    setUpdateSuccess(null);
                    setUpdateInspect(null);
                  }}
                  className="text-xs h-8 text-indigo-400 hover:text-indigo-300 hover:bg-indigo-500/10 border-indigo-500/30"
                >
                  <Upload className="h-3.5 w-3.5 mr-1.5" />
                  Atualizar Pacote (.zip)
                </Button>
              ) : <div />}
              <Button size="sm" onClick={() => setDetailsModalPlugin(null)}>
                Fechar
              </Button>
            </div>
          </div>
        </Modal>
      )}

      {/* Update Plugin Modal */}
      {updateTargetPlugin && (
        <Modal
          isOpen={Boolean(updateTargetPlugin)}
          onClose={() => {
            if (!isUpdating) {
              setUpdateTargetPlugin(null);
              setUpdateFile(null);
              setUpdateInspect(null);
              setUpdateError(null);
              setUpdateSuccess(null);
            }
          }}
          title={`Atualizar Plugin: ${updateTargetPlugin.name}`}
          description={`ID: ${updateTargetPlugin.plugin_id} • Versão instalada: v${updateTargetPlugin.version}`}
          maxWidth="lg"
        >
          <div className="space-y-4">
            <div className="p-3 rounded-xl bg-indigo-500/10 border border-indigo-500/20 text-indigo-300 text-xs flex items-start gap-2.5">
              <Info className="h-4 w-4 shrink-0 mt-0.5 text-indigo-400" />
              <div>
                <span className="font-semibold block">Atualização Direta de Pacote (.zip / .tar.gz)</span>
                <span>
                  Envie o novo arquivo compactado com as modificações do plugin. O Kraken atualizará os arquivos em disco e executará a compilação necessária (WASM ou Dockerfile), mesmo que o número da versão seja o mesmo (ideal para desenvolvimento).
                </span>
              </div>
            </div>

            {updateSuccess && (
              <div className="p-3.5 rounded-xl bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 text-xs flex items-center gap-2">
                <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400" />
                <span>{updateSuccess}</span>
              </div>
            )}

            {updateError && (
              <div className="p-3.5 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-center gap-2">
                <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
                <span>{updateError}</span>
              </div>
            )}

            <input
              type="file"
              ref={updateFileInputRef}
              accept=".zip,.tar.gz,.tgz"
              onChange={(e) => {
                if (e.target.files?.[0]) {
                  handleSelectUpdateFile(e.target.files[0]);
                }
              }}
              className="hidden"
            />

            <div
              onClick={() => !isUpdating && updateFileInputRef.current?.click()}
              className={`p-6 border-2 border-dashed rounded-xl text-center cursor-pointer transition ${
                updateFile
                  ? "border-indigo-500/60 bg-indigo-500/10"
                  : "border-slate-800 hover:border-slate-700 bg-slate-950/40"
              }`}
            >
              <Upload className="h-7 w-7 text-indigo-400 mx-auto mb-2" />
              {updateFile ? (
                <div>
                  <span className="text-xs font-semibold text-indigo-300 block">{updateFile.name}</span>
                  <span className="text-[11px] text-slate-400 font-mono">
                    {(updateFile.size / 1024).toFixed(1)} KB
                  </span>
                  <span className="text-[11px] text-slate-500 block mt-1">
                    Clique para escolher outro arquivo
                  </span>
                </div>
              ) : (
                <div>
                  <span className="text-xs text-slate-300 font-medium block">
                    Clique para selecionar o novo arquivo .zip ou .tar.gz
                  </span>
                  <span className="text-[10px] text-slate-500">Pacotes até 50 MB</span>
                </div>
              )}
            </div>

            {isInspectingUpdate && (
              <div className="flex items-center gap-2 text-xs text-slate-400 py-1">
                <RefreshCw className="h-3.5 w-3.5 animate-spin text-indigo-400" />
                <span>Inspecionando pacote selecionado...</span>
              </div>
            )}

            {updateInspect && (
              <div className="p-3 rounded-xl bg-slate-950 border border-slate-800 text-xs space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-slate-400">Versão no novo pacote:</span>
                  <span className="font-mono text-white font-semibold">
                    v{updateInspect.manifest?.version || "1.0.0"}
                    {updateInspect.manifest?.version === updateTargetPlugin.version && (
                      <span className="ml-1.5 text-[10px] text-amber-400 font-sans font-normal">
                        (mesma versão — atualização de desenvolvimento)
                      </span>
                    )}
                  </span>
                </div>

                <div className="flex items-center justify-between">
                  <span className="text-slate-400">Tipo de Execução:</span>
                  <span className="font-mono text-indigo-300 uppercase text-[11px]">
                    {updateInspect.manifest?.runtime?.type || "none"}
                  </span>
                </div>

                {updateInspect.needs_compilation && (
                  <div className="text-[11px] text-sky-400 flex items-center gap-1.5 pt-1 border-t border-slate-800/80">
                    <Hammer className="h-3 w-3 shrink-0" />
                    <span>
                      {updateInspect.manifest?.runtime?.type === "container"
                        ? "O Dockerfile será compilado e a imagem Docker será recriada automaticamente."
                        : "O código-fonte Go será recompilado em binário WASM automaticamente."}
                    </span>
                  </div>
                )}

                {updateInspect.manifest?.id && updateInspect.manifest.id !== updateTargetPlugin.plugin_id && (
                  <div className="p-2.5 rounded-lg bg-amber-500/10 border border-amber-500/20 text-amber-300 text-xs flex items-start gap-2">
                    <AlertCircle className="h-4 w-4 shrink-0 mt-0.5 text-amber-400" />
                    <span>
                      Atenção: O ID no pacote é <strong>{updateInspect.manifest.id}</strong>, diferente do plugin alvo (<strong>{updateTargetPlugin.plugin_id}</strong>).
                    </span>
                  </div>
                )}
              </div>
            )}

            <div className="pt-2">
              <label className="flex items-center gap-2 cursor-pointer select-none text-xs text-slate-300">
                <input
                  type="checkbox"
                  checked={enableAfterUpdate}
                  onChange={(e) => setEnableAfterUpdate(e.target.checked)}
                  className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-indigo-500 focus:ring-indigo-500 focus:ring-offset-slate-900 cursor-pointer"
                />
                <span>Manter plugin ativo após a atualização</span>
              </label>
            </div>

            <div className="pt-3 border-t border-slate-800 flex items-center justify-end gap-2">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setUpdateTargetPlugin(null);
                  setUpdateFile(null);
                  setUpdateInspect(null);
                  setUpdateError(null);
                  setUpdateSuccess(null);
                }}
                disabled={isUpdating}
              >
                Cancelar
              </Button>
              <Button
                variant="default"
                size="sm"
                onClick={handleConfirmUpdate}
                disabled={!updateFile || isUpdating || isInspectingUpdate}
                className="bg-indigo-600 hover:bg-indigo-500 text-white"
              >
                {isUpdating ? (
                  <>
                    <RefreshCw className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                    Atualizando Plugin...
                  </>
                ) : (
                  <>
                    <Upload className="h-3.5 w-3.5 mr-1.5" />
                    Confirmar Atualização
                  </>
                )}
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
