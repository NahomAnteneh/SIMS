"use client";

import React, { useEffect, useState, useMemo } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  BackgroundVariant,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import { InfraService } from "@/lib/services/infra";
import { transformInfraToFlow } from "@/lib/transformers/infraToFlow";
import { CustomInfraNode } from "@/components/ui/nodes/DatacenterNode";

export default function InfraCanvas() {
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [loading, setLoading] = useState(true);

  const nodeTypes = useMemo(() => ({ infraNode: CustomInfraNode }), []);

  useEffect(() => {
    async function loadCanvasData() {
      try {
        setLoading(true);

        // Fetch datasets in parallel
        const [datacenters, clusters, hosts] = await Promise.all([
          InfraService.getDatacenters(),
          InfraService.getClusters(),
          InfraService.getHosts(),
        ]);

        // Transform REST records into calculated Node/Edge trees
        const { nodes: flowNodes, edges: flowEdges } = transformInfraToFlow({
          datacenters,
          clusters,
          hosts,
        });

        setNodes(flowNodes);
        setEdges(flowEdges);
      } catch (error) {
        console.error("Error loading infrastructure canvas data:", error);
      } finally {
        setLoading(false);
      }
    }

    loadCanvasData();
  }, [setNodes, setEdges]);

  if (loading) {
    return (
      <div className="w-full h-[650px] bg-[#121110] rounded-2xl flex items-center justify-center text-zinc-500 text-sm">
        Building Infrastructure Diagram...
      </div>
    );
  }

  return (
    <div className="relative w-full h-[650px] bg-[#121110] rounded-2xl overflow-hidden border border-zinc-800/80 shadow-2xl">
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_at_center,_var(--tw-gradient-stops))] from-zinc-900/40 via-[#121110] to-[#0a0a09] pointer-events-none z-10" />

      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        nodeTypes={nodeTypes}
        
        proOptions={{ hideAttribution: true }}
      >
        <Background
          variant={BackgroundVariant.Dots}
          gap={20}
          size={1.5}
          color="#33312e"
        />
        <Controls className="!bg-[#1c1b1a] !border-zinc-800 [&>button]:!bg-[#1c1b1a] [&>button]:!border-zinc-800 [&>button]:!text-zinc-300" />
        <MiniMap
          nodeColor="#27272a"
          maskColor="rgba(18, 17, 16, 0.7)"
          className="!bg-[#1c1b1a] !border-zinc-800 rounded-lg overflow-hidden"
        />
      </ReactFlow>
    </div>
  );
}