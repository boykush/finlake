import { sankey, sankeyLinkHorizontal } from "d3-sankey";

export type FlowCategory = { kind: string; category: string; amount: number };

export type FlowRole = "income" | "hub" | "expense" | "rest" | "shortfall";

export type FlowNode = {
  id: string;
  role: FlowRole;
  label: string;
  amount: number;
  x: number;
  y: number;
  width: number;
  height: number;
};

export type FlowLink = {
  id: string;
  /** 帯が指す、収入側か支出側の端のノード。 */
  node: FlowNode;
  path: string;
  width: number;
};

export type FlowLayout = { nodes: FlowNode[]; links: FlowLink[] };

type Datum = { id: string; role: FlowRole; label: string; amount: number };

const HUB = "hub";

/**
 * 月の収支を「収入の大項目 → 収入 → 支出の大項目 / 残り」の Sankey に配置する。
 * 支出が収入を上回る月は、足りない分を収入側に置いて両側の高さを揃える。
 */
export function layoutFlow(
  categories: FlowCategory[],
  size: { width: number; height: number },
): FlowLayout {
  const positive = categories.filter((c) => c.amount > 0);
  const incomes = positive.filter((c) => c.kind === "income");
  const expenses = positive.filter((c) => c.kind === "expense");
  const income = incomes.reduce((sum, c) => sum + c.amount, 0);
  const expense = expenses.reduce((sum, c) => sum + c.amount, 0);
  if (income === 0 && expense === 0) return { nodes: [], links: [] };

  const sources: Datum[] = incomes.map((c) => ({
    id: `income:${c.category}`,
    role: "income",
    label: c.category,
    amount: c.amount,
  }));
  if (expense > income) {
    sources.push({ id: "shortfall", role: "shortfall", label: "不足", amount: expense - income });
  }
  const targets: Datum[] = expenses.map((c) => ({
    id: `expense:${c.category}`,
    role: "expense",
    label: c.category,
    amount: c.amount,
  }));
  if (income > expense) {
    targets.push({ id: "rest", role: "rest", label: "残り", amount: income - expense });
  }
  const hub: Datum = { id: HUB, role: "hub", label: "収入", amount: income };

  const graph = sankey<Datum, { source: string; target: string; value: number }>()
    .nodeId((d) => d.id)
    .nodeWidth(14)
    .nodePadding(16)
    // 並びは API が返した順（金額の大きい順）のまま。
    .nodeSort(null)
    .linkSort(null)
    .extent([
      [0, 0],
      [size.width, size.height],
    ])({
    nodes: [...sources, hub, ...targets].map((d) => ({ ...d })),
    links: [
      ...sources.map((d) => ({ source: d.id, target: HUB, value: d.amount })),
      ...targets.map((d) => ({ source: HUB, target: d.id, value: d.amount })),
    ],
  });

  const nodes = new Map<string, FlowNode>();
  for (const n of graph.nodes) {
    const [x0, x1, y0, y1] = [n.x0 ?? 0, n.x1 ?? 0, n.y0 ?? 0, n.y1 ?? 0];
    nodes.set(n.id, {
      id: n.id,
      role: n.role,
      label: n.label,
      amount: n.amount,
      x: x0,
      y: y0,
      width: x1 - x0,
      height: Math.max(y1 - y0, 1),
    });
  }
  const path = sankeyLinkHorizontal();
  const links = graph.links.map((l) => {
    const source = l.source as Datum;
    const target = l.target as Datum;
    const end = source.id === HUB ? target : source;
    return {
      id: end.id,
      node: nodes.get(end.id) as FlowNode,
      path: path(l) ?? "",
      width: Math.max(l.width ?? 0, 1),
    };
  });
  return { nodes: [...nodes.values()], links };
}
