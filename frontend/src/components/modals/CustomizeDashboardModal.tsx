"use client";

import React from "react";
import { Modal } from "@/components/ui/Modal";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import {
  Eye,
  EyeOff,
  Maximize2,
  Minimize2,
  ArrowUp,
  ArrowDown,
  RotateCcw,
  GripVertical,
  SlidersHorizontal,
} from "lucide-react";

export interface DashboardItemDefinition {
  id: string;
  title: string;
  description: string;
  icon: React.ReactNode;
}

export interface CustomizeDashboardModalProps {
  isOpen: boolean;
  onClose: () => void;
  definitions: DashboardItemDefinition[];
  order: string[];
  visibility: Record<string, boolean>;
  collapsed: Record<string, boolean>;
  onReorder: (newOrder: string[]) => void;
  onToggleVisibility: (id: string) => void;
  onToggleCollapse: (id: string) => void;
  onSetAllVisibility: (visible: boolean) => void;
  onSetAllCollapse: (collapsed: boolean) => void;
  onReset: () => void;
}

export function CustomizeDashboardModal({
  isOpen,
  onClose,
  definitions,
  order,
  visibility,
  collapsed,
  onReorder,
  onToggleVisibility,
  onToggleCollapse,
  onSetAllVisibility,
  onSetAllCollapse,
  onReset,
}: CustomizeDashboardModalProps) {
  const defMap = React.useMemo(() => {
    const map = new Map<string, DashboardItemDefinition>();
    definitions.forEach((d) => map.set(d.id, d));
    return map;
  }, [definitions]);

  const handleMove = (index: number, direction: "up" | "down") => {
    const targetIndex = direction === "up" ? index - 1 : index + 1;
    if (targetIndex < 0 || targetIndex >= order.length) return;

    const newOrder = [...order];
    const [moved] = newOrder.splice(index, 1);
    newOrder.splice(targetIndex, 0, moved);
    onReorder(newOrder);
  };

  const visibleCount = order.filter((id) => visibility[id] !== false).length;

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title="Personalizar Dashboard"
      description="Organize a ordem dos blocos, alterne entre visualização expandida ou compacta e escolha quais itens exibir na página inicial."
      maxWidth="xl"
    >
      <div className="space-y-6">
        {/* Quick Bulk Actions */}
        <div className="flex flex-wrap items-center justify-between gap-2 p-3 rounded-lg bg-slate-900/80 border border-slate-800 text-xs">
          <div className="flex items-center gap-2 text-slate-300">
            <SlidersHorizontal className="h-4 w-4 text-sky-400" />
            <span>
              Visíveis: <strong className="text-white">{visibleCount}</strong> de {order.length} blocos
            </span>
          </div>
          <div className="flex items-center gap-1.5 flex-wrap">
            <Button
              variant="outline"
              size="sm"
              onClick={() => onSetAllVisibility(true)}
              className="text-[11px] h-7 px-2"
            >
              Exibir Todos
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => onSetAllCollapse(false)}
              className="text-[11px] h-7 px-2"
            >
              Expandir Todos
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => onSetAllCollapse(true)}
              className="text-[11px] h-7 px-2"
            >
              Compactar Todos
            </Button>
          </div>
        </div>

        {/* List of Dashboard Items */}
        <div className="space-y-2.5 max-h-[50vh] overflow-y-auto pr-1">
          {order.map((id, index) => {
            const def = defMap.get(id);
            if (!def) return null;

            const isVisible = visibility[id] !== false;
            const isCollapsed = Boolean(collapsed[id]);

            return (
              <div
                key={id}
                className={`flex items-center justify-between p-3.5 rounded-xl border transition-all ${
                  isVisible
                    ? "bg-slate-900/70 border-slate-800 text-slate-200 hover:border-slate-700"
                    : "bg-slate-950/40 border-slate-900/80 text-slate-500 opacity-60"
                }`}
              >
                {/* Left: Drag indicator, Order badge, Icon & Info */}
                <div className="flex items-center gap-3 min-w-0 pr-3">
                  <span className="text-xs font-mono font-bold text-slate-500 w-5 text-center">
                    #{index + 1}
                  </span>
                  <div
                    className={`p-2 rounded-lg shrink-0 ${
                      isVisible ? "bg-slate-800 text-sky-400" : "bg-slate-900 text-slate-600"
                    }`}
                  >
                    {def.icon}
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <p className="text-xs font-semibold text-white truncate">{def.title}</p>
                      {isVisible && (
                        <Badge
                          variant={isCollapsed ? "outline" : "running"}
                          className="text-[9px] px-1.5 py-0 uppercase"
                        >
                          {isCollapsed ? "Compacto" : "Expandido"}
                        </Badge>
                      )}
                    </div>
                    <p className="text-[11px] text-slate-400 truncate mt-0.5">{def.description}</p>
                  </div>
                </div>

                {/* Right: Actions */}
                <div className="flex items-center gap-1.5 shrink-0">
                  {/* Move Up */}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleMove(index, "up")}
                    disabled={index === 0}
                    title="Mover para cima"
                    className="h-8 w-8 p-0 text-slate-400 hover:text-white disabled:opacity-30"
                  >
                    <ArrowUp className="h-4 w-4" />
                  </Button>

                  {/* Move Down */}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleMove(index, "down")}
                    disabled={index === order.length - 1}
                    title="Mover para baixo"
                    className="h-8 w-8 p-0 text-slate-400 hover:text-white disabled:opacity-30"
                  >
                    <ArrowDown className="h-4 w-4" />
                  </Button>

                  {/* Toggle Compact / Expand */}
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => onToggleCollapse(id)}
                    title={isCollapsed ? "Mudar para Expandido" : "Mudar para Compacto"}
                    disabled={!isVisible}
                    className="h-8 w-8 p-0 text-slate-400 hover:text-sky-400 disabled:opacity-30"
                  >
                    {isCollapsed ? (
                      <Maximize2 className="h-4 w-4" />
                    ) : (
                      <Minimize2 className="h-4 w-4" />
                    )}
                  </Button>

                  {/* Toggle Visibility */}
                  <Button
                    variant={isVisible ? "ghost" : "secondary"}
                    size="sm"
                    onClick={() => onToggleVisibility(id)}
                    title={isVisible ? "Ocultar da dashboard" : "Exibir na dashboard"}
                    className={`h-8 w-8 p-0 ${
                      isVisible
                        ? "text-slate-400 hover:text-rose-400"
                        : "text-emerald-400 hover:text-emerald-300"
                    }`}
                  >
                    {isVisible ? <Eye className="h-4 w-4" /> : <EyeOff className="h-4 w-4" />}
                  </Button>
                </div>
              </div>
            );
          })}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between pt-4 border-t border-slate-800">
          <Button
            variant="outline"
            size="sm"
            onClick={onReset}
            className="text-xs text-slate-400 hover:text-slate-200"
          >
            <RotateCcw className="mr-1.5 h-3.5 w-3.5" />
            Restaurar Padrão
          </Button>

          <Button variant="default" size="sm" onClick={onClose} className="text-xs">
            Concluir
          </Button>
        </div>
      </div>
    </Modal>
  );
}
