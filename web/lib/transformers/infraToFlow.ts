import { Node, Edge, Position } from "@xyflow/react";

export interface BuildFlowParams {
  datacenters?: any[];
  clusters?: any[];
  hosts?: any[];
  vms?: any[];
}

/** Layout and spacing configuration */
const LAYOUT = {
  CARD_WIDTH: 220,
  HOST_GAP: 24,
  CLUSTER_GAP: 48,
  LEVEL_Y_GAP: 120,
  DC_VERTICAL_SPACING: 140,
  START_X: 50,
  START_Y: 50,
} as const;

/** Helper to construct consistent React Flow nodes */
function createInfraNode(params: {
  id: string;
  label: string;
  type: "datacenter" | "cluster" | "host";
  status?: string;
  position: { x: number; y: number };
}): Node {
  return {
    id: params.id,
    type: "infraNode",
    position: params.position,
    data: {
      label: params.label,
      type: params.type,
      status: params.status || "healthy",
    },
    sourcePosition: Position.Bottom,
    targetPosition: Position.Top,
  };
}

/** Helper to construct consistent React Flow edges */
function createInfraEdge(params: {
  source: string;
  target: string;
  animated?: boolean;
  dashed?: boolean;
}): Edge {
  return {
    id: `edge-${params.source}-${params.target}`,
    source: params.source,
    target: params.target,
    animated: params.animated,
    style: {
      stroke: params.animated ? "#52525b" : "#3f3f46",
      strokeWidth: 2,
      ...(params.dashed ? { strokeDasharray: "4 4" } : {}),
    },
  };
}

/** Calculates total horizontal width for a cluster based on host count */
function getClusterWidth(hostCount: number): number {
  if (hostCount === 0) return LAYOUT.CARD_WIDTH;
  return hostCount * LAYOUT.CARD_WIDTH + (hostCount - 1) * LAYOUT.HOST_GAP;
}

interface SubtreeResult {
  nodes: Node[];
  edges: Edge[];
  nextY: number;
}

/** Lays out a single Datacenter and its clusters and hosts */
function layoutDatacenterSubtree(
  dc: any,
  allClusters: any[],
  allHosts: any[],
  startY: number
): SubtreeResult {
  const nodes: Node[] = [];
  const edges: Edge[] = [];
  const dcNodeId = `dc-${dc.id}`;

  // 1. Filter child clusters and standalone hosts
  const childClusters = allClusters.filter(
    (c) =>
      c.datacenterId === dc.id ||
      c.datacenter_id === dc.id ||
      (dc.siteCode && c.datacenterId === dc.siteCode)
  );
  const standaloneHosts = allHosts.filter(
    (h) =>
      (!h.clusterId && !h.cluster_id) &&
      (h.datacenterId === dc.id ||
        h.datacenter_id === dc.id ||
        (dc.siteCode && h.datacenterId === dc.siteCode))
  );

  // 2. Measure cluster subtrees
  const clusterLayouts = childClusters.map((cluster) => {
    const clusterHosts = allHosts.filter(
      (h) => h.clusterId === cluster.id || h.cluster_id === cluster.id
    );
    return {
      cluster,
      hosts: clusterHosts,
      width: getClusterWidth(clusterHosts.length),
    };
  });

  const standaloneWidth = getClusterWidth(standaloneHosts.length);
  const totalClustersWidth =
    clusterLayouts.reduce((acc, c) => acc + c.width, 0) +
    Math.max(0, clusterLayouts.length - 1) * LAYOUT.CLUSTER_GAP;

  const totalDcWidth = Math.max(
    LAYOUT.CARD_WIDTH,
    totalClustersWidth +
      (standaloneWidth > 0 && totalClustersWidth > 0 ? LAYOUT.CLUSTER_GAP : 0) +
      standaloneWidth
  );

  // 3. Datacenter Node (centered above clusters)
  const dcX = LAYOUT.START_X + (totalDcWidth - LAYOUT.CARD_WIDTH) / 2;
  const dcY = startY;

  nodes.push(
    createInfraNode({
      id: dcNodeId,
      label: dc.displayName || dc.name || "Datacenter",
      type: "datacenter",
      status: dc.status,
      position: { x: dcX, y: dcY },
    })
  );

  const clusterY = dcY + LAYOUT.LEVEL_Y_GAP;
  const hostY = clusterY + LAYOUT.LEVEL_Y_GAP;
  let currentX = LAYOUT.START_X;
  let hasHosts = standaloneHosts.length > 0;

  // 4. Place Clusters and their Hosts
  clusterLayouts.forEach(({ cluster, hosts: clusterHosts, width }) => {
    const clusterNodeId = `cluster-${cluster.id}`;
    const clusterX = currentX + (width - LAYOUT.CARD_WIDTH) / 2;

    nodes.push(
      createInfraNode({
        id: clusterNodeId,
        label: cluster.name,
        type: "cluster",
        status: cluster.status,
        position: { x: clusterX, y: clusterY },
      })
    );

    edges.push(
      createInfraEdge({
        source: dcNodeId,
        target: clusterNodeId,
        animated: true,
      })
    );

    if (clusterHosts.length > 0) {
      hasHosts = true;
    }

    clusterHosts.forEach((host, hostIndex) => {
      const hostNodeId = `host-${host.id}`;
      const hostX = currentX + hostIndex * (LAYOUT.CARD_WIDTH + LAYOUT.HOST_GAP);

      nodes.push(
        createInfraNode({
          id: hostNodeId,
          label: host.name,
          type: "host",
          status: host.status,
          position: { x: hostX, y: hostY },
        })
      );

      edges.push(
        createInfraEdge({
          source: clusterNodeId,
          target: hostNodeId,
        })
      );
    });

    currentX += width + LAYOUT.CLUSTER_GAP;
  });

  // 5. Place Standalone Hosts (if any)
  if (standaloneHosts.length > 0) {
    standaloneHosts.forEach((host, hostIndex) => {
      const hostNodeId = `host-${host.id}`;
      const hostX = currentX + hostIndex * (LAYOUT.CARD_WIDTH + LAYOUT.HOST_GAP);

      nodes.push(
        createInfraNode({
          id: hostNodeId,
          label: host.name,
          type: "host",
          status: host.status,
          position: { x: hostX, y: clusterY },
        })
      );

      edges.push(
        createInfraEdge({
          source: dcNodeId,
          target: hostNodeId,
          dashed: true,
        })
      );
    });
  }

  // 6. Compute vertical spacing for the next DC
  const dcBottomY = hasHosts
    ? hostY
    : childClusters.length > 0
    ? clusterY
    : dcY;

  return {
    nodes,
    edges,
    nextY: dcBottomY + LAYOUT.DC_VERTICAL_SPACING,
  };
}

/** Orchestrates infrastructure records into React Flow Nodes and Edges */
export function transformInfraToFlow(
  params: BuildFlowParams = {}
): { nodes: Node[]; edges: Edge[] } {
  const { datacenters = [], clusters = [], hosts = [] } = params;

  const allNodes: Node[] = [];
  const allEdges: Edge[] = [];
  let currentY: number = LAYOUT.START_Y;

  for (const dc of datacenters) {
    const { nodes, edges, nextY } = layoutDatacenterSubtree(
      dc,
      clusters,
      hosts,
      currentY
    );

    allNodes.push(...nodes);
    allEdges.push(...edges);
    currentY = nextY;
  }

  return { nodes: allNodes, edges: allEdges };
}