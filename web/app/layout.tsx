import type { Metadata } from "next";
import React from "react";

export const metadata: Metadata = {
  title: "SIMS — vSphere Inventory & Search",
  description: "Distributed vSphere Inventory & Search Platform",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body style={{ fontFamily: "system-ui, sans-serif", margin: 0, padding: "2rem", backgroundColor: "#f8fafc", color: "#0f172a" }}>
        {children}
      </body>
    </html>
  );
}
