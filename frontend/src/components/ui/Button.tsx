"use client";

import React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center whitespace-nowrap rounded-lg text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 ring-offset-background",
  {
    variants: {
      variant: {
        default: "bg-sky-500 text-white hover:bg-sky-600 shadow-sm active:scale-[0.98]",
        docker: "bg-docker text-white hover:bg-docker-600 shadow-sm shadow-sky-500/20 active:scale-[0.98]",
        k8s: "bg-k8s text-white hover:bg-k8s-600 shadow-sm shadow-blue-500/20 active:scale-[0.98]",
        destructive: "bg-rose-600 text-white hover:bg-rose-700 shadow-sm shadow-rose-500/20 active:scale-[0.98]",
        success: "bg-emerald-600 text-white hover:bg-emerald-700 shadow-sm shadow-emerald-500/20 active:scale-[0.98]",
        warning: "bg-amber-500 text-slate-950 hover:bg-amber-600 shadow-sm shadow-amber-500/20 active:scale-[0.98]",
        outline: "border border-slate-700 bg-slate-900/60 hover:bg-slate-800 text-slate-200 hover:text-white",
        secondary: "bg-slate-800 text-slate-200 hover:bg-slate-700 hover:text-white",
        ghost: "hover:bg-slate-800 text-slate-300 hover:text-white",
        link: "text-sky-400 underline-offset-4 hover:underline",
      },
      size: {
        default: "h-9 px-4 py-2",
        sm: "h-8 rounded-md px-3 text-xs",
        lg: "h-11 rounded-lg px-6 text-base",
        icon: "h-9 w-9",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  isLoading?: boolean;
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, isLoading, children, disabled, ...props }, ref) => {
    return (
      <button
        ref={ref}
        className={cn(buttonVariants({ variant, size, className }))}
        disabled={disabled || isLoading}
        {...props}
      >
        {isLoading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
        {children}
      </button>
    );
  }
);

Button.displayName = "Button";
