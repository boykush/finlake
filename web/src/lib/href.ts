/** フローの画面の URL。月と大項目はクエリで持つ。 */
export function flowHref(month: string, category?: string): string {
  const params = new URLSearchParams({ month });
  if (category) params.set("category", category);
  return `/?${params}`;
}

export type CalendarParams = {
  year: number;
  /** 使うペースを見る月（YYYY-MM）。 */
  month: string;
  /** 選んだ範囲（YYYY-MM-DD、どちらも含む）。 */
  from: string;
  to: string;
  /** 固定費の大項目も含めるか。 */
  fixed: boolean;
};

/** カレンダーの画面の URL。 */
export function calendarHref(p: CalendarParams): string {
  const params = new URLSearchParams({
    year: String(p.year),
    month: p.month,
    from: p.from,
    to: p.to,
  });
  if (p.fixed) params.set("fixed", "1");
  return `/calendar?${params}`;
}
