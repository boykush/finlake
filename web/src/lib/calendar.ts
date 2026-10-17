import { addMonths, monthsUntil } from "./month";

/** 毎月ほぼ決まった額が出る大項目。日々の使い方を見るときは既定で除く。 */
export const FIXED_CATEGORIES = ["住宅", "水道・光熱費", "通信費"];

const DAY = 24 * 60 * 60 * 1000;

function parse(date: string): Date {
  return new Date(`${date}T00:00:00Z`);
}

function format(date: Date): string {
  return date.toISOString().slice(0, 10);
}

/** from から to まで（どちらも含む）の日付を返す。 */
export function daysBetween(from: string, to: string): string[] {
  const out: string[] = [];
  for (let t = parse(from).getTime(); t <= parse(to).getTime(); t += DAY) {
    out.push(format(new Date(t)));
  }
  return out;
}

/** 月の最終日（YYYY-MM-DD）。 */
export function lastDayOf(month: string): string {
  return format(new Date(parse(`${addMonths(month, 1)}-01`).getTime() - DAY));
}

/** 月曜を 0 とする曜日。 */
export function weekday(date: string): number {
  return (parse(date).getUTCDay() + 6) % 7;
}

export type Cell = {
  date: string;
  /** その日の支出。明細の無い日（取り込む前の月、まだ来ていない日）は undefined。 */
  amount: number | undefined;
  /** 色の濃さ（0〜4）。 */
  level: number;
};

export type Year = {
  /** 週ごとの列。先頭の週は月曜より前を null で埋める。 */
  cells: (Cell | null)[];
  /** 月の1日がある列の番号（0 始まり）。 */
  monthColumns: { month: string; column: number }[];
};

/**
 * 年の日ごとの支出を、週を列・曜日を行にしたカレンダーに並べる。色の濃さは、支出のあった日の金額の
 * 四分位で決めるので、使い方が変わっても濃淡が潰れない。
 */
export function layoutYear(
  year: number,
  amounts: Map<string, number>,
  covered: { first: string; last: string },
): Year {
  const days = daysBetween(`${year}-01-01`, `${year}-12-31`);
  const inside = (d: string) => d >= covered.first && d <= covered.last;
  const past = days.filter(inside).map((d) => amounts.get(d) ?? 0);
  const spent = past.filter((a) => a > 0).sort((a, b) => a - b);
  const quartile = (q: number) => spent[Math.floor((spent.length - 1) * q)] ?? 0;
  const bounds = [quartile(0.25), quartile(0.5), quartile(0.75)];
  const level = (a: number) => (a === 0 ? 0 : 1 + bounds.filter((b) => a > b).length);

  const pad = weekday(days[0] as string);
  const cells: (Cell | null)[] = Array.from({ length: pad }, () => null);
  for (const date of days) {
    const amount = inside(date) ? (amounts.get(date) ?? 0) : undefined;
    cells.push({ date, amount, level: amount === undefined ? 0 : level(amount) });
  }
  const monthColumns = Array.from({ length: 12 }, (_, i) => {
    const month = `${year}-${String(i + 1).padStart(2, "0")}`;
    return { month, column: Math.floor(cells.findIndex((c) => c?.date === `${month}-01`) / 7) };
  });
  return { cells, monthColumns };
}

export type Stats = {
  zeroDays: number;
  peak: { date: string; amount: number } | undefined;
  /** ふだんの1日。大きな買い物の日に引っ張られないよう、平均ではなく中央値。 */
  weekdayMedian: number;
  weekendMedian: number;
};

/** 中央値。数が偶数なら真ん中の2つの平均。 */
export function median(xs: number[]): number {
  if (xs.length === 0) return 0;
  const sorted = [...xs].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2
    ? (sorted[mid] as number)
    : ((sorted[mid - 1] as number) + (sorted[mid] as number)) / 2;
}

/** 明細のある日だけで数える。 */
export function stats(cells: (Cell | null)[]): Stats {
  const past = cells.filter((c): c is Cell & { amount: number } => c?.amount !== undefined);
  const peak = past.reduce<Cell & { amount: number }>(
    (max, c) => (c.amount > max.amount ? c : max),
    past[0] ?? { date: "", amount: 0, level: 0 },
  );
  return {
    zeroDays: past.filter((c) => c.amount === 0).length,
    peak: peak.amount > 0 ? { date: peak.date, amount: peak.amount } : undefined,
    weekdayMedian: median(past.filter((c) => weekday(c.date) < 5).map((c) => c.amount)),
    weekendMedian: median(past.filter((c) => weekday(c.date) >= 5).map((c) => c.amount)),
  };
}

export type Pace = {
  /** 月初からの累計。index は日 - 1。 */
  current: number[];
  previous: number[];
  /** 前の count か月の、日ごとの累計の中央値。特別な支出のあった月に引っ張られない。 */
  median: number[];
};

function cumulative(month: string, amounts: Map<string, number>, lastDate?: string): number[] {
  let sum = 0;
  return daysBetween(`${month}-01`, lastDayOf(month))
    .filter((d) => lastDate === undefined || d <= lastDate)
    .map((d) => {
      sum += amounts.get(d) ?? 0;
      return sum;
    });
}

/** 月の使うペースを、前の月と、前の count か月の中央値に重ねる。 */
export function pace(
  month: string,
  amounts: Map<string, number>,
  lastDate: string,
  count: number,
): Pace {
  const before = monthsUntil(addMonths(month, -1), count).map((m) => cumulative(m, amounts));
  const typical = Array.from({ length: 31 }, (_, i) =>
    median(before.map((c) => c[Math.min(i, c.length - 1)] ?? 0)),
  );
  return {
    current: cumulative(month, amounts, lastDate),
    previous: before.at(-1) ?? [],
    median: typical.slice(0, daysBetween(`${month}-01`, lastDayOf(month)).length),
  };
}
