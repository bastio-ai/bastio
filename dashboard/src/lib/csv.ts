// Minimal CSV exporter for the dashboard. Serializes an array of records
// and triggers a browser download. Scope is intentionally narrow: client
// side only, no streaming, no pagination — the caller is responsible for
// fetching all rows it wants to include before calling this.

function csvEscape(v: unknown): string {
  if (v === null || v === undefined) return "";
  const s = typeof v === "string" ? v : JSON.stringify(v);
  if (/[",\n\r]/.test(s)) {
    return `"${s.replace(/"/g, '""')}"`;
  }
  return s;
}

export function toCSV<T extends Record<string, unknown>>(rows: T[], columns?: (keyof T)[]): string {
  const first = rows[0];
  if (!first) return "";
  const cols = columns ?? (Object.keys(first) as (keyof T)[]);
  const header = cols.map((c) => csvEscape(String(c))).join(",");
  const body = rows
    .map((r) => cols.map((c) => csvEscape(r[c])).join(","))
    .join("\n");
  return `${header}\n${body}`;
}

export function downloadCSV<T extends Record<string, unknown>>(
  filename: string,
  rows: T[],
  columns?: (keyof T)[],
): void {
  const csv = toCSV(rows, columns);
  const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
