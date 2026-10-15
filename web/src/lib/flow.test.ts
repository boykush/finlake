import { describe, expect, it } from "vitest";
import { layoutFlow } from "./flow";

const size = { width: 800, height: 400 };

describe("layoutFlow", () => {
  it("収入が上回る月は、残りを支出側に置く", () => {
    const { nodes, links } = layoutFlow(
      [
        { kind: "income", category: "給与", amount: 300000 },
        { kind: "expense", category: "食費", amount: 60000 },
        { kind: "expense", category: "住宅", amount: 90000 },
      ],
      size,
    );
    expect(nodes.map((n) => n.id)).toEqual([
      "income:給与",
      "hub",
      "expense:食費",
      "expense:住宅",
      "rest",
    ]);
    expect(nodes.find((n) => n.id === "rest")?.amount).toBe(150000);
    expect(links.map((l) => l.id)).toEqual(["income:給与", "expense:食費", "expense:住宅", "rest"]);
    // 帯の太さは金額に比例する
    const [food, housing] = [links[1], links[2]];
    expect((housing?.width ?? 0) / (food?.width ?? 1)).toBeCloseTo(1.5);
  });

  it("支出が上回る月は、不足を収入側に置く", () => {
    const { nodes } = layoutFlow(
      [
        { kind: "income", category: "給与", amount: 100000 },
        { kind: "expense", category: "特別な支出", amount: 130000 },
      ],
      size,
    );
    expect(nodes.find((n) => n.id === "shortfall")?.amount).toBe(30000);
    expect(nodes.some((n) => n.id === "rest")).toBe(false);
  });

  it("収支に入る明細が無ければ空を返す", () => {
    expect(layoutFlow([], size)).toEqual({ nodes: [], links: [] });
  });
});
