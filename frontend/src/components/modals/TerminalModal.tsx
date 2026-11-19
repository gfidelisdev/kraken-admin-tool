"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { RotateCw, TerminalSquare } from "lucide-react";
import "@xterm/xterm/css/xterm.css";
import type { Terminal } from "@xterm/xterm";
import type { FitAddon } from "@xterm/addon-fit";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { buildWsUrl } from "@/lib/api";

export type TerminalTarget =
  | { kind: "docker"; containerId: string; label: string }
  | { kind: "k8s"; namespace: string; pod: string; container?: string; containers?: string[]; label: string };

interface TerminalModalProps {
  isOpen: boolean;
  onClose: () => void;
  target: TerminalTarget | null;
  serverId?: number;
}

type Status = "idle" | "connecting" | "connected" | "closed" | "error";

const SHELLS = ["auto", "bash", "sh", "ash", "zsh"];

export function TerminalModal({ isOpen, onClose, target, serverId }: TerminalModalProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [status, setStatus] = useState<Status>("idle");
  const [message, setMessage] = useState<string>("");
  const [shell, setShell] = useState("auto");
  const [k8sContainer, setK8sContainer] = useState<string>("");
  const [session, setSession] = useState(0);

  useEffect(() => {
    if (target?.kind === "k8s") {
      setK8sContainer(target.container || target.containers?.[0] || "");
    }
  }, [target]);

  const disconnect = useCallback(() => {
    if (wsRef.current) {
      wsRef.current.onclose = null;
      wsRef.current.close();
      wsRef.current = null;
    }
  }, []);

  useEffect(() => {
    if (!isOpen || !target) return;
    let disposed = false;
    let resizeObserver: ResizeObserver | null = null;
    const disposables: Array<{ dispose: () => void }> = [];

    const sendResize = () => {
      const term = termRef.current;
      const ws = wsRef.current;
      if (term && ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      }
    };

    (async () => {
      const [{ Terminal }, { FitAddon }] = await Promise.all([
        import("@xterm/xterm"),
        import("@xterm/addon-fit"),
      ]);
      if (disposed || !containerRef.current) return;

      const term = new Terminal({
        cursorBlink: true,
        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
        fontSize: 13,
        theme: { background: "#020617", foreground: "#e2e8f0" },
        scrollback: 5000,
      });
      const fit = new FitAddon();
      term.loadAddon(fit);
      term.open(containerRef.current);
      fit.fit();
      termRef.current = term;
      fitRef.current = fit;

      resizeObserver = new ResizeObserver(() => {
        try {
          fit.fit();
        } catch {
          /* container not visible yet */
        }
      });
      resizeObserver.observe(containerRef.current);

      const params = new URLSearchParams({
        shell,
        cols: String(term.cols),
        rows: String(term.rows),
      });
      if (serverId) params.set("serverId", String(serverId));
      let path: string;
      if (target.kind === "docker") {
        params.set("containerId", target.containerId);
        path = `/api/ws/docker/exec?${params.toString()}`;
      } else {
        params.set("namespace", target.namespace);
        params.set("pod", target.pod);
        if (k8sContainer) params.set("container", k8sContainer);
        path = `/api/ws/k8s/exec?${params.toString()}`;
      }

      setStatus("connecting");
      setMessage("");
      term.writeln(`\x1b[90mConnecting to ${target.label}...\x1b[0m`);

      const url = await buildWsUrl(path);
      if (disposed) return;
      const ws = new WebSocket(url);
      ws.binaryType = "arraybuffer";
      wsRef.current = ws;

      ws.onopen = () => {
        setStatus("connected");
        sendResize();
        term.focus();
      };
      ws.onmessage = (ev) => {
        if (typeof ev.data !== "string") {
          term.write(new Uint8Array(ev.data as ArrayBuffer));
          return;
        }
        try {
          const msg = JSON.parse(ev.data) as { type: string; code?: number; message?: string };
          if (msg.type === "exit") {
            term.writeln(`\r\n\x1b[90m[process exited with code ${msg.code ?? 0}]\x1b[0m`);
            setStatus("closed");
          } else if (msg.type === "error") {
            term.writeln(`\r\n\x1b[31m${msg.message ?? "terminal error"}\x1b[0m`);
            setMessage(msg.message ?? "terminal error");
            setStatus("error");
          }
        } catch {
          term.write(ev.data);
        }
      };
      ws.onerror = () => {
        setStatus("error");
        setMessage("WebSocket connection failed");
      };
      ws.onclose = () => {
        setStatus((s) => (s === "error" ? s : "closed"));
      };

      disposables.push(
        term.onData((data) => {
          if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "input", data }));
        }),
        term.onResize(() => sendResize())
      );
    })().catch((err: unknown) => {
      setStatus("error");
      setMessage(err instanceof Error ? err.message : "Failed to start terminal");
    });

    return () => {
      disposed = true;
      resizeObserver?.disconnect();
      disposables.forEach((d) => d.dispose());
      disconnect();
      termRef.current?.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
    // `session` forces a fresh connection when the user clicks reconnect.
  }, [isOpen, target, serverId, shell, k8sContainer, session, disconnect]);

  const statusColor = {
    idle: "text-slate-400",
    connecting: "text-amber-400",
    connected: "text-emerald-400",
    closed: "text-slate-400",
    error: "text-rose-400",
  }[status];

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={target ? `Terminal: ${target.label}` : "Terminal"}
      description="Interactive shell session. Every session is recorded in the audit log."
      maxWidth="6xl"
    >
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-3 text-xs">
          <label className="flex items-center gap-2 text-slate-400">
            Shell
            <select
              value={shell}
              onChange={(e) => setShell(e.target.value)}
              className="rounded-md border border-slate-700 bg-slate-950 px-2 py-1 text-slate-200"
            >
              {SHELLS.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
          {target?.kind === "k8s" && (target.containers?.length ?? 0) > 1 && (
            <label className="flex items-center gap-2 text-slate-400">
              Container
              <select
                value={k8sContainer}
                onChange={(e) => setK8sContainer(e.target.value)}
                className="rounded-md border border-slate-700 bg-slate-950 px-2 py-1 text-slate-200"
              >
                {target.containers!.map((c) => (
                  <option key={c} value={c}>
                    {c}
                  </option>
                ))}
              </select>
            </label>
          )}
          <span className={`flex items-center gap-1 font-medium ${statusColor}`}>
            <TerminalSquare className="h-3.5 w-3.5" />
            {status}
            {message && status === "error" ? ` — ${message}` : ""}
          </span>
          <div className="ml-auto">
            <Button size="sm" variant="secondary" onClick={() => setSession((n) => n + 1)}>
              <RotateCw className="mr-1.5 h-3.5 w-3.5" />
              Reconnect
            </Button>
          </div>
        </div>
        <div
          ref={containerRef}
          className="h-[60vh] w-full overflow-hidden rounded-lg border border-slate-800 bg-slate-950 p-2"
        />
      </div>
    </Modal>
  );
}
