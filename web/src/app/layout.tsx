import type { Metadata, Viewport } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import type { ReactNode } from "react";
import { Providers } from "@/components/providers";
import "./globals.css";

const sans = Geist({ subsets: ["latin"], variable: "--font-sans" });
const mono = Geist_Mono({ subsets: ["latin"], variable: "--font-mono" });

export const metadata: Metadata = {
  title: "coder",
  description: "Your own Linux machines, with VS Code and a terminal in the browser.",
};

export const viewport: Viewport = {
  themeColor: "#000000",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    // There is one theme, and it is dark: the class turns on the components'
    // dark: styles.
    <html lang="en" className={`dark ${sans.variable} ${mono.variable} h-full antialiased`}>
      <body className="flex min-h-full flex-col">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
