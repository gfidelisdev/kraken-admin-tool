"use client";

import React, { createContext, useContext, useEffect, useState, useCallback } from "react";
import { User, AuthResponse } from "@/types";
import { api, getToken, setToken, removeToken } from "@/lib/api";
import { SetupOTPModal } from "@/components/auth/SetupOTPModal";

interface AuthContextType {
  user: User | null;
  token: string | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  login: (username: string, password: string) => Promise<void>;
  register: (username: string, email: string, password: string) => Promise<any>;
  logout: () => void;
  refreshUser: () => Promise<void>;
  openOTPSetup: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [token, setTokenState] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(true);

  const fetchCurrentUser = useCallback(async () => {
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
    } finally {
      setIsLoading(false);
    }
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
      }
      return res;
    } finally {
      setIsLoading(false);
    }
  };

  const logout = () => {
    removeToken();
    setUser(null);
    setTokenState(null);
    if (typeof window !== "undefined") {
      sessionStorage.removeItem("kraken_otp_dismissed");
      window.location.href = "/login";
    }
  };

  const [isOTPModalOpen, setIsOTPModalOpen] = useState(false);
  const [isInitialOTPPrompt, setIsInitialOTPPrompt] = useState(false);

  useEffect(() => {
    if (user && user.status === "active" && !user.otp_enabled) {
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
        login,
        register,
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
