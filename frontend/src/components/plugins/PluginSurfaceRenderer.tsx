"use client";

import React, { useState, useEffect, useRef, useCallback } from "react";
import DOMPurify from "dompurify";
import { getPluginWorkerJS, callPlugin, getPlugins, api } from "@/lib/api";
import { useAuth } from "@/context/AuthContext";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/Card";
import { CodeEditor } from "./CodeEditor";
import {
  RefreshCw,
  AlertCircle,
  Terminal,
  CheckCircle2,
  Info,
  Shield,
  Layers,
  Copy,
  Check,
} from "lucide-react";

interface PluginSurfaceRendererProps {
  pluginId: string;
  surface?: "page" | "widget" | "tab";
  contextParams?: Record<string, any>;
  className?: string;
}

interface UINode {
  type: string;
  id?: string;
  title?: string;
  description?: string;
  value?: any;
  defaultValue?: any;
  label?: string;
  placeholder?: string;
  inputType?: "text" | "url" | "number" | "password" | "email";
  disabled?: boolean;
  required?: boolean;
  checked?: boolean;
  rows?: number | Array<Record<string, any>>;
  options?: Array<{ value: string | number; label: string } | string>;
  variant?: string;
  children?: UINode[];
  columns?: Array<{ key: string; label: string }>;
  actionId?: string;
  html?: string;
  code?: string;
  language?: string;
  theme?: string;
  readOnly?: boolean;
  lineNumbers?: boolean;
  minimap?: boolean;
  wordWrap?: "off" | "on" | "bounded" | string;
  tabSize?: number;
  insertSpaces?: boolean;
  automaticLayout?: boolean;
  height?: number;
  saveActionId?: string;
  props?: Record<string, any>;
  helperText?: string;
  copyText?: string;
  open?: boolean;
  onCloseId?: string;
}

export function PluginSurfaceRenderer({
  pluginId,
  surface = "page",
  contextParams,
  className,
}: PluginSurfaceRendererProps) {
  const { user } = useAuth();
  const [uiTree, setUiTree] = useState<UINode | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [formValues, setFormValues] = useState<Record<string, any>>({});
  const formValuesRef = useRef<Record<string, any>>({});
  const prevNodeValuesRef = useRef<Record<string, any>>({});
  const workerRef = useRef<Worker | null>(null);
  const blobUrlRef = useRef<string | null>(null);
  const pendingRequestsRef = useRef<Map<string, { resolve: (val: any) => void; reject: (err: any) => void }>>(new Map());
  const contextParamsStr = JSON.stringify(contextParams || {});
  const sanitizedUserStr = JSON.stringify({
    id: user?.id,
    username: user?.username,
    email: user?.email,
    role: user?.role,
    auth_provider: user?.auth_provider || "local",
    microsoft_account: user?.microsoft_account || null,
  });

  const updateFormField = useCallback((fieldId: string, value: any) => {
    formValuesRef.current = {
      ...formValuesRef.current,
      [fieldId]: value,
    };
    setFormValues((prev) => ({ ...prev, [fieldId]: value }));
  }, []);

  // Sync incoming tree values when specified or modified by the plugin
  useEffect(() => {
    if (!uiTree) return;
    const updates: Record<string, any> = {};
    function scanTree(node: UINode) {
      if (node.id) {
        if (node.value !== undefined || node.defaultValue !== undefined) {
          const incoming = node.value !== undefined ? node.value : node.defaultValue;
          if (prevNodeValuesRef.current[node.id] !== incoming) {
            prevNodeValuesRef.current[node.id] = incoming;
            updates[node.id] = incoming;
          }
        } else if (node.type === "CodeEditor" && updates[node.id] === undefined && formValuesRef.current[node.id] === undefined) {
          updates[node.id] = "";
          prevNodeValuesRef.current[node.id] = "";
        }
      }
      if (node.children) {
        node.children.forEach(scanTree);
      }
    }
    scanTree(uiTree);
    if (Object.keys(updates).length > 0) {
      setFormValues((prev) => {
        const next = { ...prev, ...updates };
        formValuesRef.current = next;
        return next;
      });
    }
  }, [uiTree]);

  // Dispatch event back to Web Worker
  const sendWorkerEvent = useCallback((targetId: string, eventType: string, payload?: any) => {
    if (workerRef.current) {
      workerRef.current.postMessage({
        type: "KRAKEN_EVENT",
        targetId,
        eventType,
        payload,
      });
    }
  }, []);

  // Initialize isolated Web Worker
  useEffect(() => {
    if (!user?.id) {
      return;
    }
    let isMounted = true;
    setIsLoading(true);
    setError(null);
    setUiTree(null);

    const initWorker = async () => {
      try {
        const workerJS = await getPluginWorkerJS(pluginId);
        if (!isMounted) return;

        // Injected Kraken Worker SDK Prelude
        const sdkPrelude = `
// Kraken Worker Isolation SDK
const _pendingRPC = new Map();
let _eventHandlers = new Map();
let _formData = {};

self.kraken = {
  pluginId: "${pluginId}",
  surface: "${surface}",
  params: ${contextParamsStr},
  formData: _formData,
  user: ${sanitizedUserStr},
  session: {
    user: ${sanitizedUserStr}
  },

  render: function(tree) {
    self.postMessage({ type: 'KRAKEN_RENDER_UI', tree: tree });
  },

  log: function(...args) {
    console.log('[PLUGIN:${pluginId}]', ...args);
  },

  ui: {
    render: function(tree) {
      self.postMessage({ type: 'KRAKEN_RENDER_UI', tree: tree });
    }
  },

  backend: {
    call: function(action, payload) {
      return new Promise((resolve, reject) => {
        const id = 'rpc_' + Math.random().toString(36).substr(2, 9);
        _pendingRPC.set(id, { resolve, reject });
        self.postMessage({ type: 'KRAKEN_CALL_BACKEND', id, action, payload });
      });
    }
  },

  call: function(action, payload) {
    return self.kraken.backend.call(action, payload);
  },

  kv: {
    get: function(key) {
      return self.kraken.backend.call('kv_get', key);
    },
    set: function(key, value) {
      return self.kraken.backend.call('kv_set', { key: key, value: value });
    }
  },

  storage: {
    get: function(key) {
      return self.kraken.backend.call('kv_get', key);
    },
    set: function(key, value) {
      return self.kraken.backend.call('kv_set', { key: key, value: value });
    }
  },

  on: function(actionId, handler) {
    _eventHandlers.set(actionId, handler);
  }
};

self.onmessage = function(e) {
  const msg = e.data;
  if (!msg) return;

  if (msg.type === 'KRAKEN_RPC_REPLY') {
    const p = _pendingRPC.get(msg.id);
    if (p) {
      _pendingRPC.delete(msg.id);
      if (msg.error) p.reject(new Error(msg.error));
      else p.resolve(msg.result);
    }
  } else if (msg.type === 'KRAKEN_EVENT') {
    if (msg.payload && typeof msg.payload === 'object') {
      if (msg.payload.formData) {
        Object.assign(_formData, msg.payload.formData);
      }
      if (msg.payload.id && msg.payload.value !== undefined) {
        _formData[msg.payload.id] = msg.payload.value;
      }
      self.kraken.formData = _formData;
    }
    const handler = _eventHandlers.get(msg.targetId);
    if (handler) {
      try {
        handler(msg.payload, msg.eventType);
      } catch (err) {
        console.error('[PLUGIN WORKER ERROR]', err);
      }
    }
  }
};
`;

        const fullScript = sdkPrelude + "\n\n// --- Plugin Worker Code ---\n" + workerJS;
        const blob = new Blob([fullScript], { type: "application/javascript" });
        const blobUrl = URL.createObjectURL(blob);
        blobUrlRef.current = blobUrl;

        const worker = new Worker(blobUrl);
        workerRef.current = worker;

        // Send KRAKEN_INIT event for backward compatibility with workers waiting for init
        worker.postMessage({ type: "KRAKEN_INIT" });

        worker.onmessage = async (e) => {
          const msg = e.data;
          if (!msg) return;

          if (msg.type === "KRAKEN_RENDER_UI") {
            setUiTree(msg.tree);
            setIsLoading(false);
          } else if (msg.type === "KRAKEN_COPY_CLIPBOARD") {
            try {
              if (typeof navigator !== "undefined" && navigator?.clipboard?.writeText) {
                navigator.clipboard.writeText(msg.text || "").catch(console.error);
                if (msg.autoClearAfterMs) {
                  setTimeout(() => {
                    navigator.clipboard.writeText("").catch(() => {});
                  }, msg.autoClearAfterMs);
                }
              }
            } catch (err) {
              console.error("[PLUGIN CLIPBOARD ERROR]", err);
            }
          } else if (msg.type === "KRAKEN_CALL_BACKEND") {
            try {
              const res = await callPlugin(pluginId, msg.action, msg.payload);
              if (res.success) {
                worker.postMessage({ type: "KRAKEN_RPC_REPLY", id: msg.id, result: res.result });
              } else {
                worker.postMessage({ type: "KRAKEN_RPC_REPLY", id: msg.id, error: res.error });
              }
            } catch (err: any) {
              worker.postMessage({ type: "KRAKEN_RPC_REPLY", id: msg.id, error: err?.message || "RPC failed" });
            }
          } else if (msg.type === "KRAKEN_CREATE_SERVER") {
            try {
              // Verify plugin has the mandatory 'hosts:create' permission
              let hasHostsCreate = false;
              try {
                const { plugins } = await getPlugins();
                const p = plugins.find((item) => item.plugin_id === pluginId);
                if (p) {
                  let perms: string[] = [];
                  if (p.granted_permissions) {
                    try {
                      perms = JSON.parse(p.granted_permissions);
                    } catch {}
                  } else if (p.manifest_json) {
                    try {
                      const m = JSON.parse(p.manifest_json);
                      perms = m.permissions || [];
                    } catch {}
                  }
                  hasHostsCreate = perms.includes("hosts:create");
                }
              } catch (permErr) {
                console.warn("[PLUGIN HOSTS:CREATE CHECK FAILED]", permErr);
              }

              if (!hasHostsCreate) {
                throw new Error(`Permissão negada: o plugin '${pluginId}' não possui a permissão 'hosts:create' necessária.`);
              }

              // Sanitize and allowlist fields to prevent prototype pollution or privilege escalation
              const raw = msg.payload || {};
              const sanitizedPayload = {
                name: String(raw.name || "").trim(),
                host_url: String(raw.host_url || "").trim(),
                server_host: String(raw.server_host || "").trim(),
                server_port: String(raw.server_port || "22").trim(),
                ssh_user: String(raw.ssh_user || "").trim(),
                ssh_auth_method: String(raw.ssh_auth_method || "").trim(),
                credential_source: "plugin",
                credential_plugin_id: pluginId, // Force to caller's plugin ID
                secret_reference: String(raw.secret_reference || "").trim(),
                secret_version: raw.secret_version ? String(raw.secret_version).trim() : undefined,
                vault_config: typeof raw.vault_config === "object" ? JSON.stringify(raw.vault_config) : (raw.vault_config ? String(raw.vault_config).trim() : undefined),
                credential_policy: "save_securely",
              };

              const server = await api.post("/api/docker/servers", sanitizedPayload);
              if (typeof window !== "undefined") {
                window.dispatchEvent(new CustomEvent("kraken:refresh-servers"));
              }
              worker.postMessage({ type: "KRAKEN_CREATE_SERVER_REPLY", id: msg.id, success: true, server });
            } catch (err: any) {
              worker.postMessage({ type: "KRAKEN_CREATE_SERVER_REPLY", id: msg.id, success: false, error: err?.message || "Falha ao cadastrar servidor no Kraken" });
            }
          }
        };

        worker.onerror = (err) => {
          console.error(`[Plugin:${pluginId}] Worker error:`, err);
          setError(`Erro na execução do Worker do plugin: ${err.message}`);
          setIsLoading(false);
        };
      } catch (err: any) {
        if (!isMounted) return;
        setError(err?.message || "Falha ao carregar script do plugin");
        setIsLoading(false);
      }
    };

    initWorker();

    return () => {
      isMounted = false;
      if (workerRef.current) {
        workerRef.current.terminate();
        workerRef.current = null;
      }
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current);
        blobUrlRef.current = null;
      }
    };
  }, [pluginId, surface, contextParamsStr, sanitizedUserStr, user?.id]);

  if (isLoading) {
    return (
      <div className="flex flex-col items-center justify-center p-12 text-slate-400 space-y-3">
        <RefreshCw className="h-6 w-6 animate-spin text-sky-400" />
        <span className="text-xs font-mono">Inicializando sandbox do plugin...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-4 rounded-xl bg-rose-500/10 border border-rose-500/30 text-rose-300 text-xs flex items-center gap-3">
        <AlertCircle className="h-5 w-5 text-rose-400 shrink-0" />
        <div>
          <span className="font-semibold block">Erro ao executar plugin</span>
          <span>{error}</span>
        </div>
      </div>
    );
  }

  if (!uiTree) {
    return (
      <div className="p-6 rounded-xl bg-slate-900 border border-slate-800 text-center text-xs text-slate-400">
        Nenhum conteúdo de interface retornado pelo plugin.
      </div>
    );
  }

  let renderedContent: React.ReactNode = null;
  try {
    renderedContent = renderNode(uiTree, sendWorkerEvent, formValues, updateFormField, formValuesRef, "root");
  } catch (err: any) {
    renderedContent = (
      <div className="p-3 text-xs text-rose-400 bg-rose-500/10 rounded-lg border border-rose-500/30">
        Falha ao renderizar componente do plugin: {err?.message || String(err)}
      </div>
    );
  }

  return (
    <div className={`space-y-4 ${className || ""}`}>
      {renderedContent}
    </div>
  );
}

// Dedicated controlled input field to maintain stable DOM identity, focus, and caret position
function PluginTextInputField({
  node,
  fieldVal,
  updateFormField,
  onEvent,
  formValuesRef,
}: {
  node: UINode;
  fieldVal: any;
  updateFormField: (id: string, val: any) => void;
  onEvent: (id: string, type: string, payload?: any) => void;
  formValuesRef: React.MutableRefObject<Record<string, any>>;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [localVal, setLocalVal] = useState<string>(String(fieldVal ?? ""));

  useEffect(() => {
    // Only sync from external fieldVal if user is not actively typing in this element
    if (document.activeElement !== inputRef.current) {
      setLocalVal(String(fieldVal ?? ""));
    }
  }, [fieldVal]);

  const handleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value;
    setLocalVal(val);
    if (node.id) {
      updateFormField(node.id, val);
      onEvent(node.actionId || node.id, "change", {
        id: node.id,
        value: val,
        formData: { ...formValuesRef.current, [node.id]: val },
        [node.id]: val,
      });
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape") {
      inputRef.current?.blur();
    } else if (e.key === "Enter" && node.id) {
      onEvent(node.actionId || node.id, "enter", {
        id: node.id,
        value: localVal,
        formData: { ...formValuesRef.current },
        ...formValuesRef.current,
      });
    }
  };

  return (
    <div className="space-y-1.5 w-full">
      {node.label && (
        <label className="text-xs font-medium text-slate-300 block">
          {node.label}
          {node.required && <span className="text-rose-400 ml-1">*</span>}
        </label>
      )}
      <input
        ref={inputRef}
        type={node.inputType || "text"}
        placeholder={node.placeholder}
        disabled={node.disabled}
        value={localVal}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
        className="flex h-9 w-full rounded-lg border border-slate-700 bg-slate-900/90 px-3 py-1.5 text-xs text-slate-100 placeholder:text-slate-500 shadow-sm transition-colors focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 disabled:cursor-not-allowed disabled:opacity-50"
      />
      {(node.description || node.helperText) && (
        <p className="text-[11px] text-slate-400">{node.description || node.helperText}</p>
      )}
    </div>
  );
}

// Dedicated controlled textarea field to maintain stable focus and caret position
function PluginTextareaField({
  node,
  fieldVal,
  updateFormField,
  onEvent,
  formValuesRef,
}: {
  node: UINode;
  fieldVal: any;
  updateFormField: (id: string, val: any) => void;
  onEvent: (id: string, type: string, payload?: any) => void;
  formValuesRef: React.MutableRefObject<Record<string, any>>;
}) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [localVal, setLocalVal] = useState<string>(String(fieldVal ?? ""));

  useEffect(() => {
    if (document.activeElement !== textareaRef.current) {
      setLocalVal(String(fieldVal ?? ""));
    }
  }, [fieldVal]);

  const handleChange = (e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const val = e.target.value;
    setLocalVal(val);
    if (node.id) {
      updateFormField(node.id, val);
      onEvent(node.actionId || node.id, "change", {
        id: node.id,
        value: val,
        formData: { ...formValuesRef.current, [node.id]: val },
        [node.id]: val,
      });
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Escape") {
      textareaRef.current?.blur();
    }
  };

  return (
    <div className="space-y-1.5 w-full">
      {node.label && (
        <label className="text-xs font-medium text-slate-300 block">
          {node.label}
          {node.required && <span className="text-rose-400 ml-1">*</span>}
        </label>
      )}
      <textarea
        ref={textareaRef}
        placeholder={node.placeholder}
        disabled={node.disabled}
        rows={typeof node.rows === "number" ? node.rows : 3}
        value={localVal}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
        className="flex w-full rounded-lg border border-slate-700 bg-slate-900/90 p-3 text-xs text-slate-100 placeholder:text-slate-500 shadow-sm transition-colors focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 disabled:cursor-not-allowed disabled:opacity-50 font-mono resize-y"
      />
      {(node.description || node.helperText) && (
        <p className="text-[11px] text-slate-400">{node.description || node.helperText}</p>
      )}
    </div>
  );
}

// Recursive Native Declarative Renderer with deterministic React keys
function renderNode(
  node: UINode,
  onEvent: (id: string, type: string, payload?: any) => void,
  formValues: Record<string, any>,
  updateFormField: (id: string, val: any) => void,
  formValuesRef: React.MutableRefObject<Record<string, any>>,
  path: string = "root"
): React.ReactNode {
  if (!node) return null;

  const nodeKey = node.id ? `id-${node.id}` : `node-${path}`;

  switch (node.type) {
    case "Modal":
    case "Dialog": {
      if (node.open === false) return null;
      return (
        <div key={nodeKey} className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm p-4 overflow-y-auto">
          <div className="bg-slate-900 border border-slate-700 rounded-xl shadow-2xl max-w-lg w-full p-6 space-y-4 my-8 relative">
            <div className="flex items-start justify-between border-b border-slate-800 pb-3">
              <div>
                <h3 className="text-base font-bold text-white tracking-tight">{node.title || "Diálogo"}</h3>
                {node.description && <p className="text-xs text-slate-400 mt-0.5">{node.description}</p>}
              </div>
              {node.onCloseId && (
                <button
                  type="button"
                  onClick={() => onEvent(node.onCloseId!, "click", {})}
                  className="text-slate-400 hover:text-white text-base font-bold p-1 rounded-md hover:bg-slate-800 transition-colors ml-4 shrink-0"
                >
                  ✕
                </button>
              )}
            </div>
            <div className="space-y-4">
              {node.children && node.children.map((child, idx) =>
                renderNode(child, onEvent, formValues, updateFormField, formValuesRef, `${path}.modal.${child.id || child.type || 'n'}.${idx}`)
              )}
            </div>
          </div>
        </div>
      );
    }

    case "Page":
      return (
        <div key={nodeKey} className="space-y-6">
          {(node.title || node.description) && (
            <div className="flex flex-col gap-1">
              {node.title && <h2 className="text-xl font-bold text-white tracking-tight">{node.title}</h2>}
              {node.description && <p className="text-xs text-slate-400">{node.description}</p>}
            </div>
          )}
          {node.children && node.children.map((child, idx) => renderNode(child, onEvent, formValues, updateFormField, formValuesRef, `${path}.page.${child.id || child.type || 'n'}.${idx}`))}
        </div>
      );

    case "Grid":
      return (
        <div key={nodeKey} className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {node.children && node.children.map((child, idx) => renderNode(child, onEvent, formValues, updateFormField, formValuesRef, `${path}.grid.${child.id || child.type || 'n'}.${idx}`))}
        </div>
      );

    case "Section":
    case "Card":
      return (
        <Card key={nodeKey} className="border-slate-800 bg-slate-900/60 shadow-lg">
          {(node.title || node.description) && (
            <CardHeader className="pb-3 border-b border-slate-800/60">
              {node.title && <CardTitle className="text-sm font-semibold text-white">{node.title}</CardTitle>}
              {node.description && <CardDescription className="text-xs text-slate-400">{node.description}</CardDescription>}
            </CardHeader>
          )}
          <CardContent className="pt-4 space-y-4">
            {node.children && node.children.map((child, idx) => renderNode(child, onEvent, formValues, updateFormField, formValuesRef, `${path}.card.${child.id || child.type || 'n'}.${idx}`))}
          </CardContent>
        </Card>
      );

    case "StatCard":
      return (
        <div
          key={nodeKey}
          className="p-4 rounded-xl bg-slate-900 border border-slate-800 flex items-center justify-between"
        >
          <div>
            <span className="text-xs text-slate-400 block font-medium">{node.title}</span>
            <span className="text-2xl font-bold text-white mt-1 block">{node.value}</span>
            {node.description && <span className="text-[11px] text-slate-500 mt-0.5 block">{node.description}</span>}
          </div>
          {node.variant && (
            <Badge variant={node.variant as any} className="text-xs">
              {node.label || node.variant}
            </Badge>
          )}
        </div>
      );

    case "Table":
      return (
        <div key={nodeKey} className="overflow-x-auto rounded-xl border border-slate-800 bg-slate-950/40">
          <table className="w-full text-left text-xs text-slate-300">
            <thead className="bg-slate-900 border-b border-slate-800 text-[11px] uppercase tracking-wider text-slate-400 font-semibold">
              <tr>
                {node.columns?.map((col: any) => (
                  <th
                    key={col.key}
                    style={col.width ? { width: col.width } : undefined}
                    className={`px-4 py-3 ${col.className || ""}`}
                  >
                    {col.label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/60">
              {Array.isArray(node.rows) && node.rows.length > 0 ? (
                node.rows.map((row: any, idx: number) => (
                  <tr key={idx} className="hover:bg-slate-900/40 transition">
                    {node.columns?.map((col: any) => {
                      const cellVal = row[col.key];
                      const isNode = typeof cellVal === "object" && cellVal !== null;
                      return (
                        <td
                          key={col.key}
                          style={col.width ? { width: col.width } : undefined}
                          className={`px-4 py-2 text-xs align-middle ${col.cellClassName || ""}`}
                        >
                          {isNode ? (
                            Array.isArray(cellVal)
                              ? cellVal.map((c: any, cIdx: number) => renderNode(c, onEvent, formValues, updateFormField, formValuesRef, `${path}.tbl.${col.key}.${idx}.${cIdx}`))
                              : renderNode(cellVal, onEvent, formValues, updateFormField, formValuesRef, `${path}.tbl.${col.key}.${idx}`)
                          ) : (
                            cellVal !== undefined ? String(cellVal) : "-"
                          )}
                        </td>
                      );
                    })}
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={node.columns?.length || 1} className="px-4 py-6 text-center text-slate-500">
                    Nenhum registro encontrado
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      );

    case "Form":
      return (
        <form
          key={nodeKey}
          onSubmit={(e) => {
            e.preventDefault();
            const payload = {
              formData: { ...formValuesRef.current },
              ...formValuesRef.current,
            };
            onEvent(node.actionId || node.id || "submit", "submit", payload);
          }}
          className="space-y-4 w-full"
        >
          {node.children && node.children.map((child, idx) => renderNode(child, onEvent, formValues, updateFormField, formValuesRef, `${path}.form.${child.id || child.type || 'n'}.${idx}`))}
        </form>
      );

    case "FieldGroup":
    case "FormRow":
      return (
        <div
          key={nodeKey}
          className="flex flex-col sm:flex-row items-stretch sm:items-end gap-3 w-full"
        >
          {node.children && node.children.map((child, idx) => renderNode(child, onEvent, formValues, updateFormField, formValuesRef, `${path}.row.${child.id || child.type || 'n'}.${idx}`))}
        </div>
      );

    case "Input":
    case "TextInput": {
      const fieldVal = node.id && formValues[node.id] !== undefined
        ? formValues[node.id]
        : (node.value ?? node.defaultValue ?? "");

      return (
        <PluginTextInputField
          key={nodeKey}
          node={node}
          fieldVal={fieldVal}
          updateFormField={updateFormField}
          onEvent={onEvent}
          formValuesRef={formValuesRef}
        />
      );
    }

    case "Textarea": {
      const fieldVal = node.id && formValues[node.id] !== undefined
        ? formValues[node.id]
        : (node.value ?? node.defaultValue ?? "");

      return (
        <PluginTextareaField
          key={nodeKey}
          node={node}
          fieldVal={fieldVal}
          updateFormField={updateFormField}
          onEvent={onEvent}
          formValuesRef={formValuesRef}
        />
      );
    }

    case "CodeEditor": {
      const fieldVal = node.id && formValues[node.id] !== undefined
        ? formValues[node.id]
        : (node.value ?? node.defaultValue ?? "");

      return (
        <CodeEditor
          key={nodeKey}
          node={node as any}
          fieldVal={fieldVal}
          updateFormField={updateFormField}
          onEvent={onEvent}
          formValuesRef={formValuesRef}
        />
      );
    }

    case "Select": {
      const options = Array.isArray(node.options)
        ? node.options.map((opt) =>
            typeof opt === "object" && opt !== null ? opt : { value: String(opt), label: String(opt) }
          )
        : [];
      const fieldVal = node.id && formValues[node.id] !== undefined
        ? formValues[node.id]
        : (node.value ?? node.defaultValue ?? (options[0]?.value || ""));

      return (
        <div key={nodeKey} className="space-y-1.5 w-full">
          {node.label && (
            <label className="text-xs font-medium text-slate-300 block">
              {node.label}
              {node.required && <span className="text-rose-400 ml-1">*</span>}
            </label>
          )}
          <select
            disabled={node.disabled}
            value={fieldVal}
            onChange={(e) => {
              const val = e.target.value;
              if (node.id) {
                updateFormField(node.id, val);
                onEvent(node.actionId || node.id, "change", {
                  id: node.id,
                  value: val,
                  formData: { ...formValuesRef.current, [node.id]: val },
                  [node.id]: val,
                });
              }
            }}
            className="flex h-9 w-full rounded-lg border border-slate-700 bg-slate-900/90 px-3 py-1.5 text-xs text-slate-100 shadow-sm transition-colors focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {options.map((opt) => (
              <option key={opt.value} value={opt.value} className="bg-slate-900 text-slate-100">
                {opt.label}
              </option>
            ))}
          </select>
          {(node.description || node.helperText) && (
            <p className="text-[11px] text-slate-400">{node.description || node.helperText}</p>
          )}
        </div>
      );
    }

    case "Checkbox": {
      const checked = Boolean(
        node.id && formValues[node.id] !== undefined
          ? formValues[node.id]
          : (node.checked ?? node.value ?? false)
      );

      return (
        <div key={nodeKey} className="space-y-1">
          <label className="flex items-center gap-2 cursor-pointer select-none text-xs text-slate-200">
            <input
              type="checkbox"
              disabled={node.disabled}
              checked={checked}
              onChange={(e) => {
                const val = e.target.checked;
                if (node.id) {
                  updateFormField(node.id, val);
                  onEvent(node.actionId || node.id, "change", {
                    id: node.id,
                    value: val,
                    checked: val,
                    formData: { ...formValuesRef.current, [node.id]: val },
                    [node.id]: val,
                  });
                }
              }}
              className="h-4 w-4 rounded border-slate-700 bg-slate-900 text-sky-500 focus:ring-sky-500 focus:ring-offset-slate-900 cursor-pointer"
            />
            <span>{node.label || node.title}</span>
          </label>
          {(node.description || node.helperText) && (
            <p className="text-[11px] text-slate-400 pl-6">{node.description || node.helperText}</p>
          )}
        </div>
      );
    }

    case "Button":
      return (
        <Button
          key={nodeKey}
          variant={(node.variant as any) || "default"}
          size="sm"
          disabled={node.disabled}
          onClick={() => {
            if (node.copyText !== undefined && node.copyText !== null) {
              try {
                if (typeof navigator !== "undefined" && navigator?.clipboard?.writeText) {
                  navigator.clipboard.writeText(String(node.copyText)).catch(console.error);
                }
              } catch (_) {}
            }
            const payload = {
              value: node.value,
              formData: { ...formValuesRef.current },
              ...formValuesRef.current,
            };
            onEvent(node.actionId || node.id || "click", "click", payload);
          }}
          className="text-xs shrink-0"
        >
          {node.title || node.label || "Ação"}
        </Button>
      );

    case "Badge":
      return (
        <Badge key={nodeKey} variant={(node.variant as any) || "outline"}>
          {node.value || node.label || "Info"}
        </Badge>
      );

    case "Alert":
      return (
        <div
          key={nodeKey}
          className={`p-3.5 rounded-xl border text-xs flex items-start gap-2.5 ${
            node.variant === "destructive"
              ? "bg-rose-500/10 border-rose-500/30 text-rose-300"
              : node.variant === "success"
              ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-300"
              : "bg-sky-500/10 border-sky-500/30 text-sky-300"
          }`}
        >
          <Info className="h-4 w-4 shrink-0 mt-0.5" />
          <div>
            {node.title && <span className="font-bold block">{node.title}</span>}
            <span>{node.description || node.value}</span>
          </div>
        </div>
      );

    case "Terminal":
    case "CodeBlock":
      return (
        <div key={nodeKey} className="rounded-xl border border-slate-800 bg-slate-950 p-4 font-mono text-xs text-slate-200 overflow-x-auto relative group">
          <pre>{node.code || node.value || ""}</pre>
        </div>
      );

    case "CustomShadowHtml":
      return <ShadowHost key={nodeKey} html={node.html || ""} />;

    default:
      return (
        <div key={nodeKey} className="text-xs text-slate-400">
          {node.value ? String(node.value) : null}
        </div>
      );
  }
}

// Shadow DOM container component for custom HTML without iframes
function ShadowHost({ html }: { html: string }) {
  const containerRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!containerRef.current) return;
    let shadow = containerRef.current.shadowRoot;
    if (!shadow) {
      shadow = containerRef.current.attachShadow({ mode: "open" });
    }

    const sanitized = DOMPurify.sanitize(html, {
      ADD_TAGS: ["style"],
      FORCE_BODY: true,
      FORBID_TAGS: ["script", "iframe", "object", "embed", "form"],
      FORBID_ATTR: ["onerror", "onload", "onclick", "onmouseover", "onfocus", "onblur"],
    });

    shadow.innerHTML = `
      <style>
        :host { display: block; font-family: inherit; color: inherit; }
      </style>
      <div>${sanitized}</div>
    `;
  }, [html]);

  return <div ref={containerRef} className="w-full overflow-hidden" />;
}
