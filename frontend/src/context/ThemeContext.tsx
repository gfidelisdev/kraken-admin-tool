"use client";

import React, { createContext, useContext, useEffect, useState } from "react";

export type Theme = "dark" | "light" | "high-contrast" | "colorblind" | "sepia";

export interface ThemeOption {
  id: Theme;
  name: string;
  description: string;
  icon: "Moon" | "Sun" | "Contrast" | "Palette" | "Sunset";
}

export const THEME_OPTIONS: ThemeOption[] = [
  {
    id: "dark",
    name: "Dark",
    description: "Default deep slate control plane",
    icon: "Moon",
  },
  {
    id: "light",
    name: "Light",
    description: "High-visibility clean daytime theme",
    icon: "Sun",
  },
  {
    id: "high-contrast",
    name: "High Contrast",
    description: "WCAG AAA 7:1 maximum contrast",
    icon: "Contrast",
  },
  {
    id: "colorblind",
    name: "Colorblind Safe",
    description: "Accessible Okabe-Ito & Tol palette",
    icon: "Palette",
  },
  {
    id: "sepia",
    name: "Sepia / Nighttime",
    description: "Warm amber & low-blue-light soothing night palette",
    icon: "Sunset",
  },
];

interface ThemeContextType {
  theme: Theme;
  setTheme: (theme: Theme) => void;
  themes: ThemeOption[];
}

const THEME_STORAGE_KEY = "kraken_theme";

const ThemeContext = createContext<ThemeContextType | undefined>(undefined);

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setThemeState] = useState<Theme>("dark");

  useEffect(() => {
    try {
      const saved = localStorage.getItem(THEME_STORAGE_KEY) as Theme | null;
      if (
        saved &&
        (saved === "dark" ||
          saved === "light" ||
          saved === "high-contrast" ||
          saved === "colorblind" ||
          saved === "sepia")
      ) {
        setThemeState(saved);
        applyTheme(saved);
      } else {
        applyTheme("dark");
      }
    } catch {
      applyTheme("dark");
    }
  }, []);

  const setTheme = (newTheme: Theme) => {
    setThemeState(newTheme);
    try {
      localStorage.setItem(THEME_STORAGE_KEY, newTheme);
    } catch {}
    applyTheme(newTheme);
  };

  const applyTheme = (t: Theme) => {
    if (typeof document === "undefined") return;
    const root = document.documentElement;
    root.setAttribute("data-theme", t);

    // Clean up classes
    root.classList.remove(
      "theme-dark",
      "theme-light",
      "theme-high-contrast",
      "theme-colorblind",
      "theme-sepia",
      "dark",
      "light"
    );

    root.classList.add(`theme-${t}`);
    if (t === "light") {
      root.classList.add("light");
    } else {
      root.classList.add("dark");
    }

    // Keep <meta name="color-scheme"> in sync with active theme
    let meta = document.querySelector('meta[name="color-scheme"]');
    if (!meta) {
      meta = document.createElement("meta");
      meta.setAttribute("name", "color-scheme");
      document.head.appendChild(meta);
    }
    meta.setAttribute("content", t === "light" ? "light" : "dark");
  };

  return (
    <ThemeContext.Provider value={{ theme, setTheme, themes: THEME_OPTIONS }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error("useTheme must be used within a ThemeProvider");
  }
  return context;
}
