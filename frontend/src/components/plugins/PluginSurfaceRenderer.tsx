"use client";

import React, { useState, useEffect, useRef, useCallback } from "react";
import { getPluginWorkerJS, callPlugin } from "@/lib/api";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/Card";
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
  label?: string;
  variant?: string;
  children?: UINode[];
  columns?: Array<{ key: string; label: string }>;
  rows?: Array<Record<string, any>>;
  actionId?: string;
  html?: string;
  code?: string;
  language?: string;
  props?: Record<string, any>;
}

export function PluginSurfaceRenderer({
  pluginId,
  surface = "page",
  contextParams,
  className,
}: PluginSurfaceRendererProps) {
  const [uiTree, setUiTree] = useState<UINode | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const workerRef = useRef<Worker | null>(null);
  const blobUrlRef = useRef<string | null>(null);
  const pendingRequestsRef = useRef<Map<string, { resolve: (val: any) => void; reject: (err: any) => void }>>(new Map());
  const contextParamsStr = JSON.stringify(contextParams || {});

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

self.kraken = {
  pluginId: "${pluginId}",
  surface: "${surface}",
  params: ${contextParamsStr},

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

        worker.onmessage = async (e) => {
          const msg = e.data;
          if (!msg) return;

          if (msg.type === "KRAKEN_RENDER_UI") {
            setUiTree(msg.tree);
            setIsLoading(false);
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
  }, [pluginId, surface, contextParamsStr]);

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
    renderedContent = renderNode(uiTree, sendWorkerEvent);
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

// Recursive Native Declarative Renderer
function renderNode(node: UINode, onEvent: (id: string, type: string, payload?: any) => void): React.ReactNode {
  if (!node) return null;

  switch (node.type) {
    case "Page":
      return (
        <div key={node.id || Math.random()} className="space-y-6">
          {(node.title || node.description) && (
            <div className="flex flex-col gap-1">
              {node.title && <h2 className="text-xl font-bold text-white tracking-tight">{node.title}</h2>}
              {node.description && <p className="text-xs text-slate-400">{node.description}</p>}
            </div>
          )}
          {node.children && node.children.map((child) => renderNode(child, onEvent))}
        </div>
      );

    case "Grid":
      return (
        <div key={node.id || Math.random()} className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {node.children && node.children.map((child) => renderNode(child, onEvent))}
        </div>
      );

    case "Section":
    case "Card":
      return (
        <Card key={node.id || Math.random()} className="border-slate-800 bg-slate-900/60 shadow-lg">
          {(node.title || node.description) && (
            <CardHeader className="pb-3 border-b border-slate-800/60">
              {node.title && <CardTitle className="text-sm font-semibold text-white">{node.title}</CardTitle>}
              {node.description && <CardDescription className="text-xs text-slate-400">{node.description}</CardDescription>}
            </CardHeader>
          )}
          <CardContent className="pt-4 space-y-4">
            {node.children && node.children.map((child) => renderNode(child, onEvent))}
          </CardContent>
        </Card>
      );

    case "StatCard":
      return (
        <div
          key={node.id || Math.random()}
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
        <div key={node.id || Math.random()} className="overflow-x-auto rounded-xl border border-slate-800 bg-slate-950/40">
          <table className="w-full text-left text-xs text-slate-300">
            <thead className="bg-slate-900 border-b border-slate-800 text-[11px] uppercase tracking-wider text-slate-400 font-semibold">
              <tr>
                {node.columns?.map((col) => (
                  <th key={col.key} className="px-4 py-3">
                    {col.label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/60">
              {node.rows && node.rows.length > 0 ? (
                node.rows.map((row, idx) => (
                  <tr key={idx} className="hover:bg-slate-900/40 transition">
                    {node.columns?.map((col) => (
                      <td key={col.key} className="px-4 py-2.5 font-mono text-xs">
                        {row[col.key] !== undefined ? String(row[col.key]) : "-"}
                      </td>
                    ))}
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

    case "Button":
      return (
        <Button
          key={node.id || Math.random()}
          variant={(node.variant as any) || "default"}
          size="sm"
          onClick={() => onEvent(node.actionId || node.id || "click", "click", node.value)}
          className="text-xs"
        >
          {node.title || node.label || "Ação"}
        </Button>
      );

    case "Badge":
      return (
        <Badge key={node.id || Math.random()} variant={(node.variant as any) || "outline"}>
          {node.value || node.label || "Info"}
        </Badge>
      );

    case "Alert":
      return (
        <div
          key={node.id || Math.random()}
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
        <div key={node.id || Math.random()} className="rounded-xl border border-slate-800 bg-slate-950 p-4 font-mono text-xs text-slate-200 overflow-x-auto relative group">
          <pre>{node.code || node.value || ""}</pre>
        </div>
      );

    case "CustomShadowHtml":
      return <ShadowHost key={node.id || Math.random()} html={node.html || ""} />;

    default:
      return (
        <div key={node.id || Math.random()} className="text-xs text-slate-400">
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

    // Basic sanitization: strip script tags
    const sanitized = html.replace(/<script\b[^<]*(?:(?!<\/script>)<[^<]*)*<\/script>/gi, "");

    shadow.innerHTML = `
      <style>
        :host { display: block; font-family: inherit; color: inherit; }
      </style>
      <div>${sanitized}</div>
    `;
  }, [html]);

  return <div ref={containerRef} className="w-full overflow-hidden" />;
}
