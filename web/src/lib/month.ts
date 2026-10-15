/** YYYY-MM を n か月ずらす。 */
export function addMonths(month: string, n: number): string {
  const [year, m] = month.split("-").map(Number) as [number, number];
  const index = year * 12 + (m - 1) + n;
  return `${Math.floor(index / 12)}-${String((index % 12) + 1).padStart(2, "0")}`;
}

/** to で終わる count か月を古い順に返す。 */
export function monthsUntil(to: string, count: number): string[] {
  return Array.from({ length: count }, (_, i) => addMonths(to, i - count + 1));
}
