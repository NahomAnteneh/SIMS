import { SearchFilter } from "@/lib/api/types";

export interface ParsedToken {
  key: string;
  value: string;
  raw: string;
}

export interface ParseResult {
  tokens: ParsedToken[];
  freeText: string;
  isStructured: boolean;
  filter: SearchFilter | null;
  rawQuery: string;
}

export interface SyntaxHelperItem {
  prefix: string;
  label: string;
  example: string;
  description: string;
}

export const SYNTAX_TOKENS: SyntaxHelperItem[] = [
  { prefix: "vm:", label: "VM Name", example: "vm:worker", description: "Filter by instance name substring" },
  { prefix: "ip:", label: "IP / CIDR", example: "ip:10.0.0.0/24", description: "Match IP address or CIDR block" },
  { prefix: "power:", label: "Power State", example: "power:on", description: "poweredOn, poweredOff, or suspended" },
  { prefix: "cluster:", label: "Cluster", example: "cluster:Compute-01", description: "Filter by compute cluster" },
  { prefix: "ds:", label: "Datastore", example: "ds:vsan-prod", description: "Filter by attached datastore" },
  { prefix: "os:", label: "Guest OS", example: "os:linux", description: "Filter by operating system" },
  { prefix: "tag:", label: "vSphere Tag", example: "tag:Tier:Web", description: "Filter by tag category or tag:category:value" },
];

const KNOWN_PREFIXES = new Set([
  "vm",
  "name",
  "ip",
  "power",
  "cluster",
  "ds",
  "storage",
  "os",
  "tag",
]);

/**
 * Normalizes power state string to backend enum value.
 */
function normalizePowerState(val: string): string {
  const lower = val.toLowerCase();
  if (lower === "on" || lower === "poweredon") return "poweredOn";
  if (lower === "off" || lower === "poweredoff") return "poweredOff";
  if (lower === "suspended" || lower === "suspend") return "suspended";
  return val;
}

/**
 * Check if a string looks like a standard UUID.
 */
function isUUID(str: string): boolean {
  return /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(str);
}

/**
 * Parses user input search query into structured SearchFilter AST or free-text query.
 */
export function parseSearchQuery(raw: string): ParseResult {
  const trimmed = raw.trim();
  if (!trimmed) {
    return {
      tokens: [],
      freeText: "",
      isStructured: false,
      filter: null,
      rawQuery: raw,
    };
  }

  // Regex to extract prefix:value pairs (supports quoted strings)
  const tokenRegex = /(?:([a-zA-Z0-9_-]+):(?:"([^"]+)"|'([^']+)'|(\S+)))/g;
  const tokens: ParsedToken[] = [];
  let match: RegExpExecArray | null;
  let textWithoutTokens = trimmed;

  while ((match = tokenRegex.exec(trimmed)) !== null) {
    const key = match[1].toLowerCase();
    const value = match[2] ?? match[3] ?? match[4] ?? "";
    const rawMatch = match[0];

    if (KNOWN_PREFIXES.has(key)) {
      tokens.push({ key, value, raw: rawMatch });
      textWithoutTokens = textWithoutTokens.replace(rawMatch, " ");
    }
  }

  const freeText = textWithoutTokens.replace(/\s+/g, " ").trim();
  const isStructured = tokens.length > 0;

  if (!isStructured) {
    return {
      tokens: [],
      freeText,
      isStructured: false,
      filter: null,
      rawQuery: raw,
    };
  }

  const operands: SearchFilter[] = [];

  for (const token of tokens) {
    switch (token.key) {
      case "vm":
      case "name":
        operands.push({
          op: "CONTAINS",
          field: "name",
          value: token.value,
        });
        break;

      case "ip":
        if (token.value.includes("/")) {
          operands.push({
            op: "CIDR_CONTAINS",
            field: "guest_ip_address",
            value: token.value,
          });
        } else {
          operands.push({
            op: "EQ",
            field: "guest_ip_address",
            value: token.value,
          });
        }
        break;

      case "power":
        operands.push({
          op: "EQ",
          field: "power_state",
          value: normalizePowerState(token.value),
        });
        break;

      case "os":
        operands.push({
          op: "CONTAINS",
          field: "guest_os",
          value: token.value,
        });
        break;

      case "cluster":
        if (isUUID(token.value)) {
          operands.push({
            op: "EQ",
            field: "cluster_id",
            value: token.value,
          });
        } else {
          // Name fallback search
          operands.push({
            op: "CONTAINS",
            field: "name",
            value: token.value,
          });
        }
        break;

      case "ds":
      case "storage":
        if (isUUID(token.value)) {
          operands.push({
            op: "EQ",
            field: "datastore_id",
            value: token.value,
          });
        } else {
          operands.push({
            op: "CONTAINS",
            field: "name",
            value: token.value,
          });
        }
        break;

      case "tag": {
        const parts = token.value.split(":");
        if (parts.length >= 2) {
          operands.push({
            op: "HAS_TAG",
            category: parts[0],
            tag: parts.slice(1).join(":"),
          });
        } else {
          operands.push({
            op: "HAS_TAG",
            category: parts[0],
          });
        }
        break;
      }
    }
  }

  // If there was also free text alongside structured tokens, add it as a name constraint
  if (freeText) {
    operands.push({
      op: "CONTAINS",
      field: "name",
      value: freeText,
    });
  }

  let filter: SearchFilter | null = null;
  if (operands.length === 1) {
    filter = operands[0];
  } else if (operands.length > 1) {
    filter = {
      op: "AND",
      operands,
    };
  }

  return {
    tokens,
    freeText,
    isStructured: true,
    filter,
    rawQuery: raw,
  };
}
