import { describe, expect, it } from "vitest";
import { daysBetween, lastDayOf, layoutYear, pace, stats, weekday } from "./calendar";

describe("日付", () => {
  it("月の最終日と曜日", () => {
    expect(lastDayOf("2026-02")).toBe("2026-02-28");
    expect(lastDayOf("2024-02")).toBe("2024-02-29");
    expect(weekday("2026-01-01")).toBe(3); // 木曜
    expect(daysBetween("2026-09-29", "2026-10-02")).toEqual([
      "2026-09-29",
      "2026-09-30",
      "2026-10-01",
      "2026-10-02",
    ]);
  });
});

describe("layoutYear", () => {
  const amounts = new Map([
    ["2026-01-01", 1000],
    ["2026-01-03", 9000],
    ["2026-01-04", 3000],
  ]);
  const year = layoutYear(2026, amounts, { first: "2026-01-01", last: "2026-01-05" });

  it("取り込む前の日は金額を持たない", () => {
    const later = layoutYear(2026, amounts, { first: "2026-01-03", last: "2026-01-05" });
    expect(later.cells.find((c) => c?.date === "2026-01-01")?.amount).toBeUndefined();
    expect(stats(later.cells).zeroDays).toBe(1);
  });

  it("月曜から始まるように前を埋め、来ていない日は金額を持たない", () => {
    expect(year.cells.slice(0, 4).map((c) => c?.date ?? null)).toEqual([
      null,
      null,
      null,
      "2026-01-01",
    ]);
    expect(year.cells.find((c) => c?.date === "2026-01-06")?.amount).toBeUndefined();
    expect(year.monthColumns[0]).toEqual({ month: "2026-01", column: 0 });
  });

  it("支出の無い日は 0、ある日は四分位で 1〜4", () => {
    const level = (d: string) => year.cells.find((c) => c?.date === d)?.level;
    expect(level("2026-01-02")).toBe(0);
    expect(level("2026-01-01")).toBe(1);
    expect(level("2026-01-03")).toBe(4);
  });

  it("集計は来ている日だけ", () => {
    const s = stats(year.cells);
    expect(s.zeroDays).toBe(2);
    expect(s.peak).toEqual({ date: "2026-01-03", amount: 9000 });
    expect(s.weekendAverage).toBe(6000);
  });
});

describe("pace", () => {
  it("累計を、前の月と平均に重ねる", () => {
    const amounts = new Map([
      ["2026-08-01", 100],
      ["2026-07-01", 300],
      ["2026-09-01", 50],
      ["2026-09-02", 50],
    ]);
    const p = pace("2026-09", amounts, "2026-09-02", 2);
    expect(p.current).toEqual([50, 100]);
    expect(p.previous[30]).toBe(100);
    expect(p.average[0]).toBe(200);
    expect(p.average).toHaveLength(30);
  });
});
