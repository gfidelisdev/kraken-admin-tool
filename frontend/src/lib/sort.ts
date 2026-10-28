export type SortDirection = "asc" | "desc";

export function sortItems<T>(
  items: T[],
  key: string | null,
  direction: SortDirection = "asc",
  customGetters?: Record<string, (item: T) => any>
): T[] {
  if (!key) return items;

  return [...items].sort((a, b) => {
    const valA = customGetters?.[key] ? customGetters[key](a) : (a as any)?.[key];
    const valB = customGetters?.[key] ? customGetters[key](b) : (b as any)?.[key];

    if (valA === valB) return 0;
    if (valA === undefined || valA === null) return 1;
    if (valB === undefined || valB === null) return -1;

    let comparison = 0;
    if (typeof valA === "number" && typeof valB === "number") {
      comparison = valA - valB;
    } else if (valA instanceof Date && valB instanceof Date) {
      comparison = valA.getTime() - valB.getTime();
    } else if (typeof valA === "boolean" && typeof valB === "boolean") {
      comparison = (valA ? 1 : 0) - (valB ? 1 : 0);
    } else {
      comparison = String(valA).localeCompare(String(valB), undefined, {
        numeric: true,
        sensitivity: "base",
      });
    }

    return direction === "asc" ? comparison : -comparison;
  });
}
