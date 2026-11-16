"use client";

import React, { createContext, useContext, useEffect, useState, useCallback } from "react";
import { User, AuthResponse, SetupStatusResponse, InitialSetupRequest } from "@/types";
import { api, getToken, setToken, removeToken, getSetupStatus, completeInitialSetup } from "@/lib/api";
import { SetupOTPModal } from "@/components/auth/SetupOTPModal";

interface AuthContextType {
  user: User | null;
  token: string | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  needsSetup: boolean;
  setupStatus: SetupStatusResponse | null;
  login: (username: string, password: string) => Promise<void>;
  completeMicrosoftLogin: (token: string, user: User) => void;
  register: (username: string, email: string, password: string) => Promise<any>;
  completeSetup: (payload: InitialSetupRequest) => Promise<void>;
  logout: () => void;
  refreshUser: () => Promise<void>;
  openOTPSetup: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [token, setTokenState] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(true);
  const [needsSetup, setNeedsSetup] = useState<boolean>(false);
  const [setupStatus, setSetupStatus] = useState<SetupStatusResponse | null>(null);

  const fetchCurrentUser = useCallback(async () => {
    try {
      const status = await getSetupStatus();
      setSetupStatus(status);
      if (status?.needs_setup) {
        setNeedsSetup(true);
        removeToken();
        setUser(null);
        setTokenState(null);
        setIsLoading(false);
        return;
      }
      setNeedsSetup(false);
    } catch {
      // Ignore setup status check failure and fall back to token verification
    }

    const currentToken = getToken();
    if (!currentToken) {
      setUser(null);
      setTokenState(null);
      setIsLoading(false);
      return;
    }

    try {
      const data = await api.get<any>("/api/auth/me");
      const currentUser = data?.user || data;
      setUser(currentUser);
      setTokenState(currentToken);
    } catch {
      // If verification fails, clear token
      removeToken();
      setUser(null);
      setTokenState(null);
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchCurrentUser();
  }, [fetchCurrentUser]);

  const login = async (username: string, password: string) => {
    setIsLoading(true);
    try {
      const res = await api.post<AuthResponse>("/api/auth/login", { username, password });
      setToken(res.token);
      setTokenState(res.token);
      setUser(res.user);
      setNeedsSetup(false);
    } finally {
      setIsLoading(false);
    }
  };

  const completeMicrosoftLogin = (newToken: string, newUser: User) => {
    setToken(newToken);
    setTokenState(newToken);
    setUser(newUser);
    setNeedsSetup(false);
    setIsLoading(false);
  };

  const register = async (username: string, email: string, password: string) => {
    setIsLoading(true);
    try {
      const res = await api.post<any>("/api/auth/register", {
        username,
        email,
        password,
      });
      if (res?.token) {
        setToken(res.token);
        setTokenState(res.token);
        setUser(res.user);
        setNeedsSetup(false);
      }
      return res;
    } finally {
      setIsLoading(false);
    }
  };

  const completeSetup = async (payload: InitialSetupRequest) => {
    setIsLoading(true);
    try {
      const res = await completeInitialSetup(payload);
      setToken(res.token);
      setTokenState(res.token);
      setUser(res.user);
      setNeedsSetup(false);
      setSetupStatus((prev) =>
        prev
          ? {
              ...prev,
              needs_setup: false,
              allow_registration: payload.allow_registration ?? prev.allow_registration,
            }
          : {
              needs_setup: false,
              allow_registration: !!payload.allow_registration,
              runtime_mode: "docker",
            }
      );
    } finally {
      setIsLoading(false);
    }
  };

  const logout = () => {
    api.post("/api/auth/logout", {}).catch(() => {});
    removeToken();
    setUser(null);
    setTokenState(null);
    if (typeof window !== "undefined") {
      sessionStorage.removeItem("kraken_otp_dismissed");
      try {
        if (window.indexedDB) {
          window.indexedDB.deleteDatabase("kraken_azure_kv_plugin_db");
        }
      } catch (_) {}
      window.location.href = "/login";
    }
  };

  const [isOTPModalOpen, setIsOTPModalOpen] = useState(false);
  const [isInitialOTPPrompt, setIsInitialOTPPrompt] = useState(false);

  useEffect(() => {
    if (user && user.status === "active" && !user.otp_enabled && user.auth_provider !== "microsoft") {
      if (typeof window !== "undefined") {
        const dismissed = sessionStorage.getItem("kraken_otp_dismissed");
        if (!dismissed) {
          setIsInitialOTPPrompt(true);
          setIsOTPModalOpen(true);
        }
      }
    }
  }, [user]);

  const openOTPSetup = () => {
    setIsInitialOTPPrompt(false);
    setIsOTPModalOpen(true);
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        token,
        isLoading,
        isAuthenticated: !!user,
        needsSetup,
        setupStatus,
        login,
        completeMicrosoftLogin,
        register,
        completeSetup,
        logout,
        refreshUser: fetchCurrentUser,
        openOTPSetup,
      }}
    >
      {children}
      {user && (
        <SetupOTPModal
          isOpen={isOTPModalOpen}
          onClose={() => setIsOTPModalOpen(false)}
          isInitialPrompt={isInitialOTPPrompt}
        />
      )}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}
