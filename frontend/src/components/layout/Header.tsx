"use client";

import React, { useState } from "react";
import {
  LogOut,
  User,
  Shield,
  BookOpen,
  Menu,
  X,
  ExternalLink,
  KeyRound,
  UserPlus,
} from "lucide-react";
import { useAuth } from "@/context/AuthContext";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { ServerSelector } from "@/components/layout/ServerSelector";
import { ChangePasswordModal } from "@/components/auth/ChangePasswordModal";
import { AddUserModal } from "@/components/auth/AddUserModal";
import Link from "next/link";

export function Header({
  onToggleMobileSidebar,
}: {
  onToggleMobileSidebar?: () => void;
}) {
  const { user, logout } = useAuth();
  const [showProfileMenu, setShowProfileMenu] = useState(false);
  const [showChangePasswordModal, setShowChangePasswordModal] = useState(false);
  const [showAddUserModal, setShowAddUserModal] = useState(false);

  return (
    <>
      <header className="sticky top-0 z-30 flex h-16 w-full items-center justify-between border-b border-slate-800 bg-[#090d16]/80 px-4 md:px-8 backdrop-blur-md">
      {/* Left section: mobile hamburger & breadcrumb info */}
      <div className="flex items-center space-x-3">
        <button
          type="button"
          onClick={onToggleMobileSidebar}
          className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-800 hover:text-white md:hidden"
        >
          <Menu className="h-5 w-5" />
        </button>

        <div className="hidden sm:flex items-center space-x-2 text-xs text-slate-400">
          <span className="font-semibold text-slate-200">Kraken Admin</span>
          <span>/</span>
          <span className="text-sky-400 font-mono">Infrastructure Control Plane</span>
        </div>
      </div>

      {/* Right section: Server Selector, System Docs, Quick links & User profile dropdown */}
      <div className="flex items-center space-x-3">
        {/* Docker Server Selector */}
        <ServerSelector />

        {/* API Docs link */}
        <Link
          href="/api/swagger/index.html"
          target="_blank"
          className="hidden sm:flex items-center space-x-1.5 rounded-lg border border-slate-800 bg-slate-900/80 px-2.5 py-1.5 text-xs text-slate-300 hover:border-slate-700 hover:text-white transition-colors"
        >
          <BookOpen className="h-3.5 w-3.5 text-sky-400" />
          <span>API Docs</span>
          <ExternalLink className="h-3 w-3 text-slate-500" />
        </Link>

        {/* User Info & Menu */}
        <div className="relative">
          <button
            onClick={() => setShowProfileMenu((v) => !v)}
            className="flex items-center space-x-2.5 rounded-xl border border-slate-800 bg-slate-900/90 px-3 py-1.5 text-left transition-colors hover:border-slate-700 hover:bg-slate-800/80"
          >
            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-sky-500/20 text-sky-400 font-bold text-xs border border-sky-500/30">
              {user?.username ? user.username.charAt(0).toUpperCase() : "U"}
            </div>
            <div className="hidden sm:block">
              <p className="text-xs font-semibold text-slate-200 leading-none">
                {user?.username || "Admin"}
              </p>
              <p className="text-[10px] text-slate-400 leading-none mt-1 font-mono">
                {user?.role || "superadmin"}
              </p>
            </div>
          </button>

          {/* Profile Dropdown */}
          {showProfileMenu && (
            <>
              <div
                className="fixed inset-0 z-40"
                onClick={() => setShowProfileMenu(false)}
              />
              <div className="absolute right-0 mt-2 w-56 rounded-xl border border-slate-800 bg-slate-900 p-2 shadow-xl z-50 animate-in fade-in zoom-in-95">
                <div className="px-3 py-2 border-b border-slate-800">
                  <p className="text-xs font-semibold text-white">{user?.username}</p>
                  <p className="text-[11px] text-slate-400 truncate">{user?.email}</p>
                  <div className="mt-1.5 flex items-center space-x-1">
                    <Badge variant="outline" className="text-[10px] py-0 px-1.5">
                      <Shield className="h-2.5 w-2.5 mr-1 text-sky-400" />
                      {user?.role || "admin"}
                    </Badge>
                  </div>
                </div>

                <div className="pt-1.5 space-y-1">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setShowProfileMenu(false);
                      setShowChangePasswordModal(true);
                    }}
                    className="w-full justify-start text-xs text-slate-300 hover:bg-slate-800 hover:text-white"
                  >
                    <KeyRound className="mr-2 h-3.5 w-3.5 text-sky-400" />
                    Change Password
                  </Button>

                  {user?.role === "admin" && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setShowProfileMenu(false);
                        setShowAddUserModal(true);
                      }}
                      className="w-full justify-start text-xs text-slate-300 hover:bg-slate-800 hover:text-white"
                    >
                      <UserPlus className="mr-2 h-3.5 w-3.5 text-emerald-400" />
                      Add New User
                    </Button>
                  )}

                  <div className="pt-1 border-t border-slate-800/80">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setShowProfileMenu(false);
                        logout();
                      }}
                      className="w-full justify-start text-xs text-rose-400 hover:bg-rose-500/10 hover:text-rose-300"
                    >
                      <LogOut className="mr-2 h-3.5 w-3.5" />
                      Sign Out
                    </Button>
                  </div>
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </header>

    {/* Account Modals */}
    <ChangePasswordModal
      isOpen={showChangePasswordModal}
      onClose={() => setShowChangePasswordModal(false)}
    />

    <AddUserModal
      isOpen={showAddUserModal}
      onClose={() => setShowAddUserModal(false)}
    />
  </>
  );
}
