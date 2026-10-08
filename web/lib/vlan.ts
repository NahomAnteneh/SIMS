/**
 * Formats a VLAN specification or PostgreSQL multirange string (e.g. "{[10,21),[100,201)}")
 * into a clean, human-readable format like "10-20, 100-200" or "100".
 */
export function formatVlanDisplay(vlanSpec?: string | null): string {
  if (!vlanSpec || vlanSpec === "{}" || vlanSpec.trim() === "") {
    return "None";
  }

  let s = vlanSpec.trim();
  if (s.startsWith("{") && s.endsWith("}")) {
    s = s.slice(1, -1).trim();
  }

  if (!s) return "None";

  // Check if formatted with brackets [start,end) or [start,end]
  if (s.includes("[") || s.includes("(")) {
    const regex = /([\[\(])\s*(\d+)\s*,\s*(\d+)\s*([\]\)])/g;
    const ranges: string[] = [];
    let match: RegExpExecArray | null;

    while ((match = regex.exec(s)) !== null) {
      const incStart = match[1] === "[";
      let start = parseInt(match[2], 10);
      let end = parseInt(match[3], 10);
      const incEnd = match[4] === "]";

      if (!incStart) start += 1;
      if (!incEnd) end -= 1;

      if (start === end) {
        ranges.push(`${start}`);
      } else if (start < end) {
        ranges.push(`${start}-${end}`);
      }
    }

    if (ranges.length > 0) {
      return ranges.join(", ");
    }
  }

  // Already a clean human string like "100" or "100-200"
  return s;
}

/**
 * Returns a URL-safe, clean query parameter value for a VLAN specification.
 */
export function getVlanFilterParam(vlanSpec?: string | null): string | null {
  const display = formatVlanDisplay(vlanSpec);
  if (display === "None" || !display) return null;
  return display;
}
