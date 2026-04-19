import type { Metadata } from "next";
import { IBM_Plex_Sans, Space_Grotesk } from "next/font/google";
import Script from "next/script";
import { OrcaWorkspaceProvider } from "../components/orca-workspace-provider";
import { QueryProvider } from "../components/query-provider";
import { ThemeProvider } from "../components/theme-provider";
import "./globals.css";

const themeBootstrap = `(function(){try{var storageKey="go-orca-ui.theme";var stored=window.localStorage.getItem(storageKey);var theme=stored==="dark"||stored==="light"?stored:(window.matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light");var root=document.documentElement;root.dataset.theme=theme;root.classList.toggle("dark",theme==="dark");root.style.colorScheme=theme;}catch(error){}})();`;

const displayFont = Space_Grotesk({
  subsets: ["latin"],
  variable: "--font-display",
});

const bodyFont = IBM_Plex_Sans({
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
  variable: "--font-body",
});

export const metadata: Metadata = {
  title: "go-orca Control Center",
  description: "Authenticated control surface for go-orca workflows, tenants, providers, and scope customizations.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`${displayFont.variable} ${bodyFont.variable}`} suppressHydrationWarning>
      <body className="bg-canvas font-body text-ink antialiased">
        <Script id="theme-bootstrap" strategy="beforeInteractive">
          {themeBootstrap}
        </Script>
        <ThemeProvider>
          <QueryProvider>
            <OrcaWorkspaceProvider>{children}</OrcaWorkspaceProvider>
          </QueryProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}