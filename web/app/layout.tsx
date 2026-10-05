import type { Metadata } from 'next';
import { Inter, Geist } from 'next/font/google';
import './globals.css';
import { Sidebar } from '@/components/layout/Sidebar';
import { Topbar } from '@/components/layout/Topbar';
import { cn } from "@/lib/utils";

const geist = Geist({subsets:['latin'],variable:'--font-sans'});

const inter = Inter({ subsets: ['latin'] });

export const metadata: Metadata = {
  title: 'Depot - Inventory Management',
  description: 'Analytics and insights for your inventory',
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className={cn("dark", "font-sans", geist.variable)}>
      <body className={`${inter.className} flex h-screen w-full bg-black text-zinc-100 overflow-hidden antialiased`}>
        {/* Left Sidebar */}
        <Sidebar />
        
        {/* Right Content Area */}
        <div className="flex flex-1 flex-col min-w-0 overflow-hidden">
          <Topbar />
          <main className="flex-1 overflow-y-auto bg-[#0a0a0a] p-6">
            {children}
          </main>
        </div>
      </body>
    </html>
  );
}