"use client";

import React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
  {
    variants: {
      variant: {
        default: "border-transparent bg-slate-800 text-slate-200",
        running: "border-emerald-500/30 bg-emerald-500/10 text-emerald-400 border",
        stopped: "border-rose-500/30 bg-rose-500/10 text-rose-400 border",
        error: "border-red-500/30 bg-red-500/10 text-red-400 border",
        pending: "border-amber-500/30 bg-amber-500/10 text-amber-400 border",
        docker: "border-sky-500/30 bg-sky-500/10 text-sky-400 border",
        k8s: "border-indigo-500/30 bg-indigo-500/10 text-indigo-400 border",
        outline: "text-slate-300 border border-slate-700",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <div className={cn(badgeVariants({ variant }), className)} {...props} />;
}

export function StatusBadge({ status }: { status: string }) {
  const s = status.toLowerCase();
  let variant: VariantProps<typeof badgeVariants>["variant"] = "default";

  if (s.includes("running") || s.includes("up") || s === "success" || s === "ready") {
    variant = "running";
  } else if (
    s.includes("stop") ||
    s.includes("exit") ||
    s.includes("failed") ||
    s.includes("crash") ||
    s === "error"
  ) {
    variant = "stopped";
  } else if (s.includes("pending") || s.includes("wait") || s.includes("restart") || s.includes("containercreating")) {
    variant = "pending";
  }

  return (
    <Badge variant={variant} className="capitalize flex items-center gap-1.5 w-fit">
      <span
        className={cn("h-1.5 w-1.5 rounded-full", {
          "bg-emerald-400 animate-pulse": variant === "running",
          "bg-rose-400": variant === "stopped",
          "bg-amber-400 animate-bounce": variant === "pending",
          "bg-slate-400": variant === "default",
        })}
      />
      {status}
    </Badge>
  );
}
