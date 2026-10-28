import type { Metadata } from "next";
import "./globals.css";
import { AuthProvider } from "@/context/AuthContext";
import { ThemeProvider } from "@/context/ThemeContext";

export const metadata: Metadata = {
  title: "Kraken Admin Tool - Unified Docker & Kubernetes Management",
  description: "Modern production-grade management dashboard for Docker and Kubernetes clusters.",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" className="dark" suppressHydrationWarning>
      <head>
        <script
          dangerouslySetInnerHTML={{
            __html: `
              (function() {
                try {
                  var saved = localStorage.getItem('kraken_theme') || 'dark';
                  document.documentElement.setAttribute('data-theme', saved);
                  document.documentElement.classList.remove('theme-dark', 'theme-light', 'theme-high-contrast', 'theme-colorblind', 'theme-sepia', 'dark', 'light');
                  document.documentElement.classList.add('theme-' + saved);
                  if (saved === 'light') {
                    document.documentElement.classList.add('light');
                  } else {
                    document.documentElement.classList.add('dark');
                  }
                  var meta = document.querySelector('meta[name="color-scheme"]');
                  if (!meta) {
                    meta = document.createElement('meta');
                    meta.setAttribute('name', 'color-scheme');
                    document.head.appendChild(meta);
                  }
                  meta.setAttribute('content', saved === 'light' ? 'light' : 'dark');
                } catch (e) {}
              })();
            `,
          }}
        />
      </head>
      <body className="min-h-screen bg-[#0b0f19] text-slate-100 antialiased selection:bg-sky-500/30 selection:text-white">
        <ThemeProvider>
          <AuthProvider>{children}</AuthProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
