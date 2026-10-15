/** フローの画面の URL。月と大項目はクエリで持つ。 */
export function flowHref(month: string, category?: string): string {
  const params = new URLSearchParams({ month });
  if (category) params.set("category", category);
  return `/?${params}`;
}
