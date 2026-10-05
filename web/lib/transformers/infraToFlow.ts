import { Node, Edge, Position } from "@xyflow/react";

export interface BuildFlowParams {
  datacenters?: any[];
  clusters?: any[];
  hosts?: any[];
  vms?: any[];
}

export function transformInfraToFlow(
  params: BuildFlowParams = {}
): { nodes: Node[]; edges: Edge[] } {
  // 1. Safe default fallbacks to prevent crash on initial API load
  const { datacenters = [], clusters = [], hosts = [] } = params;

  const nodes: Node[] = [];
  const edges: Edge[] = [];

  // Expanded Spacing Configuration to prevent subtree collisions
  const DC_X_OFFSET = 900; // Expanded horizontal distance between East/West DCs
  const CLUSTER_Y_OFFSET = 240; // Vertical gap to Clusters
  const HOST_Y_OFFSET = 180; // Vertical gap between stacked Hosts
  const X_BRANCH_GAP = 380; // Horizontal gap between sibling clusters

  datacenters.forEach((dc, dcIndex) => {
    const dcNodeId = `dc-${dc.id}`;

    // Alternate Datacenters East (+) and West (-) from center origin (X = 0)
    const direction = dcIndex % 2 === 0 ? 1 : -1;
    const pairGroup = Math.floor(dcIndex / 2);
    const dcXPosition = direction * (pairGroup + 0.5) * DC_X_OFFSET;
    const dcYPosition = 50;

    // 1. Add Datacenter Node
    nodes.push({
      id: dcNodeId,
      type: "infraNode",
      position: { x: dcXPosition, y: dcYPosition },
      data: {
        label: dc.displayName || dc.name || "Datacenter",
        sublabel: dc.location || "Datacenter Site",
        type: "datacenter",
        status: dc.status || "healthy",
        metrics: {
          hosts: dc.hostCount ?? 0,
          vms: dc.vmCount ?? 0,
          cpu: `${dc.usedCpuGHz ?? 0} / ${dc.totalCpuGHz ?? 0} GHz`,
        },
      },
      sourcePosition: Position.Bottom,
      targetPosition: Position.Top,
    });

    // 2. Filter Clusters belonging STRICTLY to this Datacenter
    const childClusters = clusters.filter(
      (c) => c.datacenterId === dc.id || c.datacenterId === dc.siteCode
    );

    childClusters.forEach((cluster, clusterIndex) => {
      const clusterNodeId = `cluster-${cluster.id}`;

      // Spread clusters horizontally under parent DC
      const clusterX =
        dcXPosition +
        (clusterIndex - (childClusters.length - 1) / 2) * X_BRANCH_GAP;
      const clusterY = dcYPosition + CLUSTER_Y_OFFSET;

      nodes.push({
        id: clusterNodeId,
        type: "infraNode",
        position: { x: clusterX, y: clusterY },
        data: {
          label: cluster.name,
          sublabel: `HA: ${cluster.haEnabled ? "ON" : "OFF"} | DRS: ${
            cluster.drsEnabled ? "ON" : "OFF"
          }`,
          type: "cluster",
          status: cluster.status || "healthy",
          metrics: {
            hosts: cluster.hostCount ?? 0,
            vms: cluster.vmCount ?? 0,
          },
        },
        sourcePosition: Position.Bottom,
        targetPosition: Position.Top,
      });

      // Edge: Datacenter -> Cluster
      edges.push({
        id: `edge-${dcNodeId}-${clusterNodeId}`,
        source: dcNodeId,
        target: clusterNodeId,
        animated: true,
        style: { stroke: "#52525b", strokeWidth: 2 },
      });

      // 3. Filter Hosts belonging STRICTLY to this Cluster
      // FIX: Removed `|| h.datacenterId === dc.id` to prevent host duplication!
      const clusterHosts = hosts.filter((h) => h.clusterId === cluster.id);

      // Stack Hosts VERTICALLY beneath their parent cluster
      clusterHosts.forEach((host, hostIndex) => {
        const hostNodeId = `host-${host.id}`;
        const hostX = clusterX; // Aligned directly beneath parent cluster
        const hostY = clusterY + CLUSTER_Y_OFFSET + hostIndex * HOST_Y_OFFSET;

        nodes.push({
          id: hostNodeId,
          type: "infraNode",
          position: { x: hostX, y: hostY },
          data: {
            label: host.name,
            sublabel: host.ipAddress || "ESXi Host",
            type: "host",
            status: host.status || "healthy",
            metrics: {
              vms: host.vmCount ?? 0,
              cpu: `${host.usedCpuGHz ?? 0} GHz`,
            },
          },
          sourcePosition: Position.Bottom,
          targetPosition: Position.Top,
        });

        // Vertical chain: Cluster -> Host 1 -> Host 2 -> Host 3
        const sourceId =
          hostIndex === 0 ? clusterNodeId : `host-${clusterHosts[hostIndex - 1].id}`;

        edges.push({
          id: `edge-${sourceId}-${hostNodeId}`,
          source: sourceId,
          target: hostNodeId,
          style: { stroke: "#3f3f46", strokeWidth: 2 },
        });
      });
    });

    // 4. Standalone Hosts (Hosts in this DC without a cluster)
    const standaloneHosts = hosts.filter(
      (h) =>
        (!h.clusterId || h.clusterId === "") &&
        (h.datacenterId === dc.id || h.datacenterId === dc.siteCode)
    );

    if (standaloneHosts.length > 0) {
      const standaloneX =
        dcXPosition +
        (childClusters.length > 0
          ? ((childClusters.length + 1) / 2) * X_BRANCH_GAP
          : 0);

      standaloneHosts.forEach((host, hostIndex) => {
        const hostNodeId = `host-${host.id}`;
        const hostY =
          dcYPosition + CLUSTER_Y_OFFSET + hostIndex * HOST_Y_OFFSET;

        nodes.push({
          id: hostNodeId,
          type: "infraNode",
          position: { x: standaloneX, y: hostY },
          data: {
            label: host.name,
            sublabel: host.ipAddress || "Standalone Host",
            type: "host",
            status: host.status || "healthy",
            metrics: {
              vms: host.vmCount ?? 0,
              cpu: `${host.usedCpuGHz ?? 0} GHz`,
            },
          },
          sourcePosition: Position.Bottom,
          targetPosition: Position.Top,
        });

        const sourceId =
          hostIndex === 0
            ? dcNodeId
            : `host-${standaloneHosts[hostIndex - 1].id}`;

        edges.push({
          id: `edge-${sourceId}-${hostNodeId}`,
          source: sourceId,
          target: hostNodeId,
          style: { stroke: "#3f3f46", strokeWidth: 2, strokeDasharray: "4 4" },
        });
      });
    }
  });

  return { nodes, edges };
}