import React from "react";

export default function HomePage() {
  return (
    <main style={{ maxWidth: "800px", margin: "0 auto", background: "#ffffff", padding: "2rem", borderRadius: "8px", boxShadow: "0 1px 3px rgba(0,0,0,0.1)" }}>
      <header style={{ borderBottom: "1px solid #e2e8f0", paddingBottom: "1rem", marginBottom: "1.5rem" }}>
        <h1 style={{ fontSize: "1.75rem", margin: "0 0 0.5rem 0" }}>Distributed vSphere Inventory & Search Platform</h1>
        <p style={{ color: "#64748b", margin: 0 }}>Sprint 0 — Foundations &amp; Architecture Validation</p>
      </header>
      <section>
        <p style={{ lineHeight: 1.6 }}>
          Platform foundations initialized. System architecture is decoupled across collector fleet, PostgreSQL storage with sqlc, and operational query API.
        </p>
        <div style={{ marginTop: "1.5rem", padding: "1rem", backgroundColor: "#f1f5f9", borderRadius: "6px" }}>
          <strong>Platform Status:</strong> Foundations Operational (Sprint 0 Baseline)
        </div>
      </section>
    </main>
  );
}
