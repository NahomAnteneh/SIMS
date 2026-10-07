import type { Metadata } from 'next';
import { Inter, Geist } from 'next/font/google';
import './globals.css';
import { AppShell } from "@/components/layout/AppShell";
import { cn } from "@/lib/utils";

const geist = Geist({ subsets: ["latin"], variable: "--font-sans" });
const inter = Inter({ subsets: ["latin"] });

export const metadata: Metadata = {
  title: "Depot - Inventory Management",
  description: "Analytics and insights for your inventory",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className={cn("dark", "font-sans", geist.variable)}>
      <body className={`${inter.className} bg-black text-zinc-100 antialiased`}>
        <AppShell>{children}</AppShell>
      </body>
    </html>
  );
}