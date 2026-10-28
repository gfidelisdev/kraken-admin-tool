"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import {
  Users,
  UserCheck,
  KeyRound,
  Shield,
  Clock,
  CheckCircle2,
  XCircle,
  Search,
  Plus,
  RefreshCw,
  AlertCircle,
  MoreVertical,
  Trash2,
  ShieldAlert,
} from "lucide-react";
import { api } from "@/lib/api";
import { User, PasswordResetRequest } from "@/types";
import { useAuth } from "@/context/AuthContext";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { SortControl } from "@/components/ui/SortControl";
import { SortableTableHead } from "@/components/ui/Table";
import { sortItems, SortDirection } from "@/lib/sort";
import { ApproveUserModal } from "@/components/auth/ApproveUserModal";
import { ResetPasswordModal } from "@/components/auth/ResetPasswordModal";
import { ChangeRoleModal } from "@/components/auth/ChangeRoleModal";
import { AddUserModal } from "@/components/auth/AddUserModal";
import { ConfirmActionModal } from "@/components/modals/ConfirmActionModal";

export default function UsersPage() {
  const { user: currentUser } = useAuth();
  const isAdmin = currentUser?.role === "admin";

  const [activeTab, setActiveTab] = useState<"all" | "pending" | "recovery">("all");
  const [users, setUsers] = useState<User[]>([]);
  const [recoveryRequests, setRecoveryRequests] = useState<PasswordResetRequest[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [searchTerm, setSearchTerm] = useState("");
  const [sortKey, setSortKey] = useState<string>("user");
  const [sortDirection, setSortDirection] = useState<SortDirection>("asc");

  const handleSort = (key: string) => {
    if (sortKey === key) {
      setSortDirection((prev) => (prev === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDirection("asc");
    }
  };
  const [error, setError] = useState<string | null>(null);

  // Modals state
  const [isAddUserOpen, setIsAddUserOpen] = useState(false);
  const [approvingUser, setApprovingUser] = useState<User | null>(null);
  const [resettingUser, setResettingUser] = useState<User | { id: number; username: string; email?: string } | null>(null);
  const [changingRoleUser, setChangingRoleUser] = useState<User | null>(null);
  const [deletingUser, setDeletingUser] = useState<User | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);

  const fetchData = useCallback(async () => {
    if (!isAdmin) return;
    setIsLoading(true);
    setError(null);
    try {
      const [usersData, recoveryData] = await Promise.all([
        api.get<User[]>("/api/auth/users"),
        api.get<PasswordResetRequest[]>("/api/auth/password-recovery-requests"),
      ]);
      setUsers(usersData || []);
      setRecoveryRequests(recoveryData || []);
    } catch (err: any) {
      setError(err?.message || "Failed to load users data");
    } finally {
      setIsLoading(false);
    }
  }, [isAdmin]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const pendingUsers = users.filter((u) => u.status === "pending");
  const pendingRecoveries = recoveryRequests.filter((r) => r.status === "pending");

  const handleDeleteUser = async () => {
    if (!deletingUser) return;
    setIsDeleting(true);
    try {
      await api.delete(`/api/auth/users/${deletingUser.id}`);
      setDeletingUser(null);
      fetchData();
    } catch (err: any) {
      alert(err?.message || "Failed to delete user");
    } finally {
      setIsDeleting(false);
    }
  };

  const handleRejectUser = async (user: User) => {
    if (!confirm(`Are you sure you want to reject the registration request for "${user.username}"?`)) {
      return;
    }
    try {
      await api.post(`/api/auth/users/${user.id}/reject`, {});
      fetchData();
    } catch (err: any) {
      alert(err?.message || "Failed to reject user");
    }
  };

  const handleRejectRecovery = async (request: PasswordResetRequest) => {
    if (!confirm(`Dismiss password recovery request from "${request.username}"?`)) {
      return;
    }
    try {
      await api.post(`/api/auth/password-recovery-requests/${request.id}/reject`, {});
      fetchData();
    } catch (err: any) {
      alert(err?.message || "Failed to dismiss request");
    }
  };

  const filteredUsers = useMemo(() => {
    let list = users;
    if (searchTerm.trim()) {
      const term = searchTerm.toLowerCase();
      list = users.filter(
        (u) =>
          u.username.toLowerCase().includes(term) ||
          u.email.toLowerCase().includes(term) ||
          u.role.toLowerCase().includes(term) ||
          u.status.toLowerCase().includes(term)
      );
    }

    const customGetters = {
      user: (u: User) => u.username,
      role: (u: User) => u.role,
      status: (u: User) => u.status,
      created: (u: User) => u.created_at || "",
    };

    return sortItems(list, sortKey, sortDirection, customGetters);
  }, [users, searchTerm, sortKey, sortDirection]);

  if (!isAdmin) {
    return (
      <div className="flex h-96 flex-col items-center justify-center p-6 text-center">
        <ShieldAlert className="h-12 w-12 text-rose-500 mb-3" />
        <h2 className="text-lg font-semibold text-white">Administrator Access Required</h2>
        <p className="mt-1 text-xs text-slate-400 max-w-sm">
          User management and access approval are restricted to administrative accounts.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-white flex items-center gap-2">
            <Users className="h-6 w-6 text-sky-400" />
            Users & Access Control
          </h1>
          <p className="text-xs text-slate-400">
            Approve registrations, attribute operational roles, and manage password recoveries
          </p>
        </div>

        <div className="flex items-center space-x-2">
          <Button
            variant="outline"
            size="sm"
            onClick={fetchData}
            isLoading={isLoading}
            className="text-xs"
          >
            <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
            Refresh
          </Button>
          <Button
            size="sm"
            onClick={() => setIsAddUserOpen(true)}
            className="text-xs"
          >
            <Plus className="mr-1.5 h-3.5 w-3.5" />
            Add User
          </Button>
        </div>
      </div>

      {error && (
        <div className="flex items-center space-x-2 rounded-xl border border-rose-500/20 bg-rose-500/10 p-3 text-xs text-rose-400">
          <AlertCircle className="h-4 w-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* Stats Cards */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Card className="p-4 bg-slate-900/60 border-slate-800">
          <div className="flex items-center justify-between">
            <p className="text-xs font-medium text-slate-400">Total Users</p>
            <Users className="h-4 w-4 text-sky-400" />
          </div>
          <p className="mt-2 text-2xl font-bold text-white">{users.length}</p>
        </Card>

        <Card className="p-4 bg-slate-900/60 border-slate-800">
          <div className="flex items-center justify-between">
            <p className="text-xs font-medium text-slate-400">Active Users</p>
            <CheckCircle2 className="h-4 w-4 text-emerald-400" />
          </div>
          <p className="mt-2 text-2xl font-bold text-emerald-400">
            {users.filter((u) => u.status === "active").length}
          </p>
        </Card>

        <Card
          className={`p-4 cursor-pointer transition-colors ${
            pendingUsers.length > 0
              ? "bg-amber-500/10 border-amber-500/30 hover:border-amber-500/50"
              : "bg-slate-900/60 border-slate-800"
          }`}
          onClick={() => setActiveTab("pending")}
        >
          <div className="flex items-center justify-between">
            <p className="text-xs font-medium text-slate-400">Pending Approvals</p>
            <Clock className={`h-4 w-4 ${pendingUsers.length > 0 ? "text-amber-400" : "text-slate-500"}`} />
          </div>
          <div className="mt-2 flex items-baseline space-x-2">
            <span className={`text-2xl font-bold ${pendingUsers.length > 0 ? "text-amber-400" : "text-white"}`}>
              {pendingUsers.length}
            </span>
            {pendingUsers.length > 0 && (
              <span className="text-[10px] text-amber-300 font-medium">Needs Review</span>
            )}
          </div>
        </Card>

        <Card
          className={`p-4 cursor-pointer transition-colors ${
            pendingRecoveries.length > 0
              ? "bg-indigo-500/10 border-indigo-500/30 hover:border-indigo-500/50"
              : "bg-slate-900/60 border-slate-800"
          }`}
          onClick={() => setActiveTab("recovery")}
        >
          <div className="flex items-center justify-between">
            <p className="text-xs font-medium text-slate-400">Recovery Requests</p>
            <KeyRound className={`h-4 w-4 ${pendingRecoveries.length > 0 ? "text-indigo-400" : "text-slate-500"}`} />
          </div>
          <div className="mt-2 flex items-baseline space-x-2">
            <span className={`text-2xl font-bold ${pendingRecoveries.length > 0 ? "text-indigo-400" : "text-white"}`}>
              {pendingRecoveries.length}
            </span>
            {pendingRecoveries.length > 0 && (
              <span className="text-[10px] text-indigo-300 font-medium">Action Needed</span>
            )}
          </div>
        </Card>
      </div>

      {/* Tab Navigation */}
      <div className="flex border-b border-slate-800">
        <button
          onClick={() => setActiveTab("all")}
          className={`px-4 py-2.5 text-xs font-semibold border-b-2 transition-colors flex items-center space-x-2 ${
            activeTab === "all"
              ? "border-sky-500 text-sky-400"
              : "border-transparent text-slate-400 hover:text-slate-200"
          }`}
        >
          <span>All Registered Users</span>
          <Badge variant="outline" className="text-[10px] py-0 px-1.5 ml-1.5">
            {users.length}
          </Badge>
        </button>

        <button
          onClick={() => setActiveTab("pending")}
          className={`px-4 py-2.5 text-xs font-semibold border-b-2 transition-colors flex items-center space-x-2 ${
            activeTab === "pending"
              ? "border-amber-500 text-amber-400"
              : "border-transparent text-slate-400 hover:text-slate-200"
          }`}
        >
          <Clock className="h-3.5 w-3.5" />
          <span>Pending Approvals</span>
          {pendingUsers.length > 0 && (
            <span className="rounded-full bg-amber-500/20 px-1.5 py-0.2 text-[10px] font-bold text-amber-300 border border-amber-500/40">
              {pendingUsers.length}
            </span>
          )}
        </button>

        <button
          onClick={() => setActiveTab("recovery")}
          className={`px-4 py-2.5 text-xs font-semibold border-b-2 transition-colors flex items-center space-x-2 ${
            activeTab === "recovery"
              ? "border-indigo-500 text-indigo-400"
              : "border-transparent text-slate-400 hover:text-slate-200"
          }`}
        >
          <KeyRound className="h-3.5 w-3.5" />
          <span>Password Recovery Requests</span>
          {pendingRecoveries.length > 0 && (
            <span className="rounded-full bg-indigo-500/20 px-1.5 py-0.2 text-[10px] font-bold text-indigo-300 border border-indigo-500/40">
              {pendingRecoveries.length}
            </span>
          )}
        </button>
      </div>

      {/* Tab: All Users */}
      {activeTab === "all" && (
        <div className="space-y-4">
          <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3">
            <div className="w-full max-w-xs">
              <Input
                placeholder="Search users by name, email, role..."
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                icon={<Search className="h-4 w-4" />}
              />
            </div>

            <SortControl
              options={[
                { value: "user", label: "User / Email" },
                { value: "role", label: "Role" },
                { value: "status", label: "Status" },
                { value: "created", label: "Created Date" },
              ]}
              currentKey={sortKey}
              currentDirection={sortDirection}
              onKeyChange={(k) => setSortKey(k)}
              onDirectionToggle={() =>
                setSortDirection((prev) => (prev === "asc" ? "desc" : "asc"))
              }
            />
          </div>

          <div
            tabIndex={0}
            role="region"
            aria-label="Users and access control table"
            className="rounded-xl border border-slate-800 bg-slate-900/60 overflow-hidden focus:outline-none focus:ring-1 focus:ring-sky-500/50"
          >
            <table className="w-full text-left text-xs">
              <thead className="bg-slate-950/60 text-slate-400 border-b border-slate-800 uppercase font-mono text-[10px]">
                <tr>
                  <SortableTableHead
                    sortKey="user"
                    currentSortKey={sortKey}
                    currentDirection={sortDirection}
                    onSort={handleSort}
                    className="px-4 py-3"
                  >
                    User
                  </SortableTableHead>
                  <SortableTableHead
                    sortKey="role"
                    currentSortKey={sortKey}
                    currentDirection={sortDirection}
                    onSort={handleSort}
                    className="px-4 py-3"
                  >
                    Role
                  </SortableTableHead>
                  <SortableTableHead
                    sortKey="status"
                    currentSortKey={sortKey}
                    currentDirection={sortDirection}
                    onSort={handleSort}
                    className="px-4 py-3"
                  >
                    Status
                  </SortableTableHead>
                  <SortableTableHead
                    sortKey="created"
                    currentSortKey={sortKey}
                    currentDirection={sortDirection}
                    onSort={handleSort}
                    className="px-4 py-3"
                  >
                    Created
                  </SortableTableHead>
                  <th className="px-4 py-3 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/80">
                {filteredUsers.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="p-8 text-center text-slate-500">
                      No users matching search criteria.
                    </td>
                  </tr>
                ) : (
                  filteredUsers.map((u) => (
                    <tr key={u.id} className="hover:bg-slate-800/30 transition-colors">
                      <td className="px-4 py-3">
                        <div className="flex items-center space-x-2.5">
                          <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-sky-500/20 text-sky-400 font-bold text-xs border border-sky-500/30">
                            {u.username.charAt(0).toUpperCase()}
                          </div>
                          <div>
                            <p className="font-semibold text-white">{u.username}</p>
                            <p className="text-[11px] text-slate-400">{u.email}</p>
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <Badge
                          variant="outline"
                          className={
                            u.role === "admin"
                              ? "bg-sky-500/10 text-sky-400 border-sky-500/30"
                              : u.role === "operator"
                              ? "bg-indigo-500/10 text-indigo-400 border-indigo-500/30"
                              : "bg-slate-800 text-slate-300 border-slate-700"
                          }
                        >
                          <Shield className="h-2.5 w-2.5 mr-1" />
                          {u.role || "pending"}
                        </Badge>
                      </td>
                      <td className="px-4 py-3">
                        {u.status === "active" ? (
                          <Badge variant="outline" className="bg-emerald-500/10 text-emerald-400 border-emerald-500/30">
                            <CheckCircle2 className="h-2.5 w-2.5 mr-1" />
                            Active
                          </Badge>
                        ) : u.status === "pending" ? (
                          <Badge variant="outline" className="bg-amber-500/10 text-amber-400 border-amber-500/30">
                            <Clock className="h-2.5 w-2.5 mr-1" />
                            Pending Approval
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="bg-rose-500/10 text-rose-400 border-rose-500/30">
                            <XCircle className="h-2.5 w-2.5 mr-1" />
                            Rejected
                          </Badge>
                        )}
                      </td>
                      <td className="px-4 py-3 text-slate-400 font-mono text-[11px]">
                        {u.created_at ? new Date(u.created_at).toLocaleDateString() : "-"}
                      </td>
                      <td className="px-4 py-3 text-right">
                        <div className="flex items-center justify-end space-x-1.5">
                          {u.status === "pending" ? (
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => setApprovingUser(u)}
                              className="text-[11px] h-7 px-2 border-emerald-500/30 text-emerald-400 hover:bg-emerald-500/10"
                            >
                              <UserCheck className="h-3 w-3 mr-1" />
                              Approve
                            </Button>
                          ) : (
                            <Button
                              size="sm"
                              variant="ghost"
                              onClick={() => setChangingRoleUser(u)}
                              className="text-[11px] h-7 px-2 text-slate-300 hover:text-white"
                            >
                              <Shield className="h-3 w-3 mr-1 text-sky-400" />
                              Change Role
                            </Button>
                          )}

                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => setResettingUser(u)}
                            className="text-[11px] h-7 px-2 text-slate-300 hover:text-white"
                            title="Reset Password for this user"
                          >
                            <KeyRound className="h-3 w-3 mr-1 text-amber-400" />
                            Reset Password
                          </Button>

                          {currentUser?.id !== u.id && (
                            <Button
                              size="sm"
                              variant="ghost"
                              onClick={() => setDeletingUser(u)}
                              className="text-[11px] h-7 px-2 text-rose-400 hover:bg-rose-500/10 hover:text-rose-300"
                              title="Delete User"
                            >
                              <Trash2 className="h-3 w-3" />
                            </Button>
                          )}
                        </div>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Tab: Pending Approvals */}
      {activeTab === "pending" && (
        <div className="space-y-4">
          <div className="rounded-xl border border-amber-500/20 bg-amber-500/5 p-3.5 text-xs text-amber-300 flex items-start space-x-2.5">
            <Clock className="h-4 w-4 shrink-0 text-amber-400 mt-0.5" />
            <div>
              <p className="font-semibold text-amber-200">Account Approval Required</p>
              <p className="text-slate-300 mt-0.5 leading-relaxed">
                When users attempt to create an account, their access remains disabled until an
                administrator approves the request and attributes an operational role (e.g. Operator,
                Viewer, or Admin).
              </p>
            </div>
          </div>

          <div className="rounded-xl border border-slate-800 bg-slate-900/60 overflow-hidden">
            <table className="w-full text-left text-xs">
              <thead className="bg-slate-950/60 text-slate-400 border-b border-slate-800 uppercase font-mono text-[10px]">
                <tr>
                  <th className="px-4 py-3">User Requested</th>
                  <th className="px-4 py-3">Registration Date</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3 text-right">Approval Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/80">
                {pendingUsers.length === 0 ? (
                  <tr>
                    <td colSpan={4} className="p-8 text-center text-slate-500">
                      <div className="flex flex-col items-center justify-center">
                        <CheckCircle2 className="h-8 w-8 text-emerald-500/40 mb-2" />
                        <p className="text-slate-300 font-medium">All caught up!</p>
                        <p className="text-[11px] text-slate-500 mt-0.5">
                          There are currently no new account registration requests awaiting approval.
                        </p>
                      </div>
                    </td>
                  </tr>
                ) : (
                  pendingUsers.map((u) => (
                    <tr key={u.id} className="hover:bg-slate-800/30 transition-colors">
                      <td className="px-4 py-3">
                        <div className="flex items-center space-x-2.5">
                          <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-amber-500/20 text-amber-400 font-bold text-xs border border-amber-500/30">
                            {u.username.charAt(0).toUpperCase()}
                          </div>
                          <div>
                            <p className="font-semibold text-white">{u.username}</p>
                            <p className="text-[11px] text-slate-400">{u.email}</p>
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-3 text-slate-400 font-mono text-[11px]">
                        {u.created_at ? new Date(u.created_at).toLocaleString() : "-"}
                      </td>
                      <td className="px-4 py-3">
                        <Badge variant="outline" className="bg-amber-500/10 text-amber-400 border-amber-500/30">
                          <Clock className="h-2.5 w-2.5 mr-1" />
                          Awaiting Admin Role
                        </Badge>
                      </td>
                      <td className="px-4 py-3 text-right">
                        <div className="flex items-center justify-end space-x-2">
                          <Button
                            size="sm"
                            onClick={() => setApprovingUser(u)}
                            className="bg-emerald-600 hover:bg-emerald-500 text-white text-xs h-8"
                          >
                            <UserCheck className="mr-1.5 h-3.5 w-3.5" />
                            Approve & Assign Role
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleRejectUser(u)}
                            className="text-xs h-8 border-rose-500/30 text-rose-400 hover:bg-rose-500/10"
                          >
                            <XCircle className="mr-1.5 h-3.5 w-3.5" />
                            Reject
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Tab: Password Recovery Requests */}
      {activeTab === "recovery" && (
        <div className="space-y-4">
          <div className="rounded-xl border border-indigo-500/20 bg-indigo-500/5 p-3.5 text-xs text-indigo-300 flex items-start space-x-2.5">
            <KeyRound className="h-4 w-4 shrink-0 text-indigo-400 mt-0.5" />
            <div>
              <p className="font-semibold text-indigo-200">Password Recovery via Administrator</p>
              <p className="text-slate-300 mt-0.5 leading-relaxed">
                Users who forgot their credentials submit requests here. Administrators can reset
                their passwords, generate a secure temporary credential, and copy it to communicate to
                the user.
              </p>
            </div>
          </div>

          <div className="rounded-xl border border-slate-800 bg-slate-900/60 overflow-hidden">
            <table className="w-full text-left text-xs">
              <thead className="bg-slate-950/60 text-slate-400 border-b border-slate-800 uppercase font-mono text-[10px]">
                <tr>
                  <th className="px-4 py-3">Account</th>
                  <th className="px-4 py-3">User Note / Reason</th>
                  <th className="px-4 py-3">Submitted</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/80">
                {recoveryRequests.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="p-8 text-center text-slate-500">
                      <div className="flex flex-col items-center justify-center">
                        <CheckCircle2 className="h-8 w-8 text-emerald-500/40 mb-2" />
                        <p className="text-slate-300 font-medium">No recovery requests</p>
                        <p className="text-[11px] text-slate-500 mt-0.5">
                          No users have requested password recovery.
                        </p>
                      </div>
                    </td>
                  </tr>
                ) : (
                  recoveryRequests.map((req) => (
                    <tr key={req.id} className="hover:bg-slate-800/30 transition-colors">
                      <td className="px-4 py-3">
                        <p className="font-semibold text-white">{req.username}</p>
                        <p className="text-[11px] text-slate-400">{req.email}</p>
                      </td>
                      <td className="px-4 py-3 text-slate-300 max-w-xs truncate">
                        {req.note || <span className="text-slate-500 italic">None provided</span>}
                      </td>
                      <td className="px-4 py-3 text-slate-400 font-mono text-[11px]">
                        {new Date(req.created_at).toLocaleString()}
                      </td>
                      <td className="px-4 py-3">
                        {req.status === "pending" ? (
                          <Badge variant="outline" className="bg-indigo-500/10 text-indigo-400 border-indigo-500/30">
                            <Clock className="h-2.5 w-2.5 mr-1" />
                            Pending Recovery
                          </Badge>
                        ) : req.status === "resolved" ? (
                          <div className="space-y-0.5">
                            <Badge variant="outline" className="bg-emerald-500/10 text-emerald-400 border-emerald-500/30">
                              <CheckCircle2 className="h-2.5 w-2.5 mr-1" />
                              Resolved
                            </Badge>
                            {req.resolved_by && (
                              <p className="text-[10px] text-slate-500">by {req.resolved_by}</p>
                            )}
                          </div>
                        ) : (
                          <Badge variant="outline" className="bg-rose-500/10 text-rose-400 border-rose-500/30">
                            <XCircle className="h-2.5 w-2.5 mr-1" />
                            Dismissed
                          </Badge>
                        )}
                      </td>
                      <td className="px-4 py-3 text-right">
                        {req.status === "pending" ? (
                          <div className="flex items-center justify-end space-x-2">
                            <Button
                              size="sm"
                              onClick={() =>
                                setResettingUser({
                                  id: req.user_id,
                                  username: req.username,
                                  email: req.email,
                                })
                              }
                              className="text-xs h-8"
                            >
                              <KeyRound className="mr-1.5 h-3.5 w-3.5" />
                              Reset Password
                            </Button>
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => handleRejectRecovery(req)}
                              className="text-xs h-8 border-slate-700 text-slate-400 hover:text-white"
                            >
                              Dismiss
                            </Button>
                          </div>
                        ) : (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() =>
                              setResettingUser({
                                id: req.user_id,
                                username: req.username,
                                email: req.email,
                              })
                            }
                            className="text-xs h-7 text-slate-400 hover:text-white"
                          >
                            Reset Again
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Modals */}
      <AddUserModal
        isOpen={isAddUserOpen}
        onClose={() => setIsAddUserOpen(false)}
        onUserAdded={fetchData}
      />

      <ApproveUserModal
        isOpen={!!approvingUser}
        user={approvingUser}
        onClose={() => setApprovingUser(null)}
        onUserApproved={fetchData}
      />

      <ResetPasswordModal
        isOpen={!!resettingUser}
        user={resettingUser}
        onClose={() => setResettingUser(null)}
        onPasswordReset={fetchData}
      />

      <ChangeRoleModal
        isOpen={!!changingRoleUser}
        user={changingRoleUser}
        onClose={() => setChangingRoleUser(null)}
        onRoleUpdated={fetchData}
      />

      <ConfirmActionModal
        isOpen={!!deletingUser}
        onClose={() => setDeletingUser(null)}
        onConfirm={handleDeleteUser}
        title="Delete User Account"
        description={`Are you sure you want to permanently delete user "${deletingUser?.username}"? This action cannot be undone.`}
        confirmLabel="Delete Account"
        confirmVariant="destructive"
      />
    </div>
  );
}
