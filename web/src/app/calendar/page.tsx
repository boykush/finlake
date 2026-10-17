import Link from "next/link";
import { CalendarView } from "@/components/calendar/calendar-view";
import { Shell, Stat, Stepper } from "@/components/shell";
import {
  daysBetween,
  FIXED_CATEGORIES,
  lastDayOf,
  layoutYear,
  type Pace,
  pace,
  stats,
  weekday,
} from "@/lib/calendar";
import { yen } from "@/lib/format";
import { type CalendarParams, calendarHref } from "@/lib/href";
import { ledger } from "@/lib/ledger";
import { addMonths } from "@/lib/month";
import { cn } from "@/lib/utils";

const PACE_MONTHS = 6;
const WEEKDAYS = ["月", "火", "水", "木", "金", "土", "日"];
const MAX_ROWS = 50;

const datePattern = /^\d{4}-\d{2}-\d{2}$/;

export default async function CalendarPage({
  searchParams,
}: {
  searchParams: Promise<{
    year?: string;
    month?: string;
    from?: string;
    to?: string;
    fixed?: string;
  }>;
}) {
  const query = await searchParams;
  const { months } = await ledger().listMonths({});
  const latest = months[0]?.month;
  if (!latest) {
    return (
      <Shell current="/calendar">
        <p className="text-muted-foreground">まだ明細がありません。</p>
      </Shell>
    );
  }
  const available = new Set(months.map((m) => m.month));
  const years = [...new Set(months.map((m) => Number(m.month.slice(0, 4))))].sort();
  const year = years.includes(Number(query.year)) ? Number(query.year) : Number(latest.slice(0, 4));
  const month =
    query.month && available.has(query.month) && query.month.startsWith(String(year))
      ? query.month
      : (months.find((m) => m.month.startsWith(String(year)))?.month ?? latest);
  const [from, to] =
    query.from && query.to && datePattern.test(query.from) && datePattern.test(query.to)
      ? [query.from, query.to].sort()
      : [`${month}-01`, lastDayOf(month)];
  const params: CalendarParams = {
    year,
    month,
    from: from as string,
    to: to as string,
    fixed: query.fixed === "1",
  };
  const exclude = params.fixed ? [] : FIXED_CATEGORIES;
  // 明細があるのは、最初に取り込んだ月の初日から、最後に取り込んだ月の末日か今日（JST）の早い方まで。
  // その外の日は「使わなかった日」ではないので塗らない。
  const today = new Date(Date.now() + 9 * 60 * 60 * 1000).toISOString().slice(0, 10);
  const lastDate = [lastDayOf(latest), today].sort()[0] as string;
  const covered = { first: `${months.at(-1)?.month}-01`, last: lastDate };

  const paceFrom = `${addMonths(month, -PACE_MONTHS)}-01`;
  const [yearDays, paceDays, list] = await Promise.all([
    ledger().listDailyExpenses({
      from: `${year}-01-01`,
      to: `${year}-12-31`,
      excludeCategories: exclude,
    }),
    ledger().listDailyExpenses({
      from: paceFrom,
      to: lastDayOf(month),
      excludeCategories: exclude,
    }),
    ledger().listTransactions({ from: params.from, to: params.to, kind: "expense", limit: 1000 }),
  ]);
  const toMap = (days: typeof yearDays.days) =>
    new Map(days.map((d) => [d.date, Number(d.amount)]));
  const calendar = layoutYear(year, toMap(yearDays.days), covered);
  const s = stats(calendar.cells);

  const transactions = list.transactions
    .filter((t) => params.fixed || !FIXED_CATEGORIES.includes(t.category ?? ""))
    .map((t) => ({ ...t, amount: -Number(t.amount) }));

  const yearIndex = years.indexOf(year);
  const yearStep = (y: number | undefined, label: string) =>
    y === undefined
      ? undefined
      : { href: calendarHref({ ...params, ...defaultsFor(y, months) }), label };

  return (
    <Shell
      current="/calendar"
      actions={
        <Stepper
          label={String(year)}
          previous={yearStep(years[yearIndex - 1], "前の年")}
          next={yearStep(years[yearIndex + 1], "次の年")}
        />
      }
    >
      <CalendarView
        year={calendar}
        params={params}
        heading={
          <>
            <div className="mb-4 flex flex-wrap items-baseline gap-x-4 gap-y-1">
              <h1 className="text-lg font-semibold">日ごとの支出</h1>
              <p className="text-[13px] text-muted-foreground">
                日をドラッグして範囲を選ぶと、下の内訳が絞り込まれます
              </p>
              <Link
                href={calendarHref({ ...params, fixed: !params.fixed })}
                scroll={false}
                aria-pressed={params.fixed}
                className={cn(
                  "ml-auto flex min-h-11 items-center rounded-full border px-3.5 text-[13px] transition-colors",
                  params.fixed
                    ? "border-expense bg-expense-muted text-expense"
                    : "bg-background text-muted-foreground hover:bg-muted",
                )}
              >
                固定費（{FIXED_CATEGORIES.join("・")}）も含める
              </Link>
            </div>
            <dl className="mb-5 grid grid-cols-[repeat(auto-fit,minmax(180px,1fr))] gap-3">
              <Stat label="使わなかった日" value={`${s.zeroDays}日`} />
              <Stat
                label="いちばん使った日"
                value={s.peak ? s.peak.date.slice(5).replace("-", "/") : "—"}
                note={s.peak && yen(s.peak.amount)}
              />
              <Stat label="平日の中央値" value={yen(Math.round(s.weekdayMedian))} />
              <Stat
                label="土日の中央値"
                value={yen(Math.round(s.weekendMedian))}
                className="text-expense"
              />
            </dl>
          </>
        }
      >
        <div className="flex flex-wrap items-start gap-4">
          <PaceChart
            month={month}
            pace={pace(month, toMap(paceDays.days), lastDate, PACE_MONTHS)}
            range={params}
          />
          <RangeDetail from={params.from} to={params.to} transactions={transactions} />
        </div>
      </CalendarView>
    </Shell>
  );
}

/** 年を移ったときの月と範囲。その年の最後の取り込み済みの月にする。 */
function defaultsFor(year: number, months: { month: string }[]) {
  const month = months.find((m) => m.month.startsWith(String(year)))?.month ?? `${year}-12`;
  return { year, month, from: `${month}-01`, to: lastDayOf(month) };
}

function PaceChart({ month, pace, range }: { month: string; pace: Pace; range: CalendarParams }) {
  const W = 600;
  const H = 240;
  const days = pace.median.length;
  const top = Math.max(...pace.current, ...pace.previous, ...pace.median, 1) * 1.05;
  const x = (i: number) => (i / Math.max(days - 1, 1)) * W;
  const y = (v: number) => H - 10 - (v / top) * (H - 20);
  const line = (xs: number[]) => xs.map((v, i) => `${i ? "L" : "M"}${x(i)},${y(v)}`).join("");
  // 選んだ範囲がこの月にかかっていれば、その日々を帯で示す。
  const inMonth = daysBetween(range.from, range.to).filter((d) => d.startsWith(month));
  const band =
    inMonth.length > 0
      ? `M${x(Number(inMonth[0]?.slice(8)) - 1)},10H${x(Number(inMonth.at(-1)?.slice(8)) - 1)}V${H - 10}H${x(Number(inMonth[0]?.slice(8)) - 1)}Z`
      : "";
  const end = (xs: number[]) => yen(Math.round(xs.at(-1) ?? 0));

  return (
    <section className="min-w-0 flex-[1_1_460px] rounded-xl border bg-card p-5">
      <div className="mb-3 flex flex-wrap items-baseline gap-x-4 gap-y-1">
        <h2 className="text-base font-semibold">{Number(month.slice(5))}月の使うペース</h2>
        <p className="text-[13px] text-muted-foreground">月初からの累計</p>
      </div>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="block h-auto w-full"
        role="img"
        aria-label={`${month} の累計の支出を、前の月と前の${PACE_MONTHS}か月の中央値に重ねた折れ線`}
      >
        {band && <path d={band} className="fill-foreground/[0.07]" />}
        <path
          d={`M0,${H - 10}H${W}M0,${H / 2}H${W}M0,10H${W}`}
          fill="none"
          className="stroke-border"
        />
        <path
          d={line(pace.median)}
          fill="none"
          strokeWidth={1.5}
          strokeDasharray="4 4"
          className="stroke-income"
        />
        <path d={line(pace.previous)} fill="none" strokeWidth={1.5} className="stroke-flow" />
        <path
          d={line(pace.current)}
          fill="none"
          strokeWidth={2.5}
          strokeLinejoin="round"
          className="stroke-expense"
        />
      </svg>
      <div className="mt-1 flex justify-between font-mono text-[11px] text-muted-foreground">
        <span>1日</span>
        <span>{days}日</span>
      </div>
      <ul className="mt-3.5 flex flex-wrap gap-x-6 gap-y-2 text-[13px]">
        <li className="flex items-center gap-2">
          <span className="h-[3px] w-4 bg-expense" />
          {Number(month.slice(5))}月 <span className="font-mono">{end(pace.current)}</span>
        </li>
        <li className="flex items-center gap-2 text-muted-foreground">
          <span className="h-0.5 w-4 bg-flow" />
          {Number(addMonths(month, -1).slice(5))}月{" "}
          <span className="font-mono">{end(pace.previous)}</span>
        </li>
        <li className="flex items-center gap-2 text-muted-foreground">
          <span className="w-4 border-t-2 border-dashed border-income" />
          {PACE_MONTHS}か月の中央値 <span className="font-mono">{end(pace.median)}</span>
        </li>
      </ul>
    </section>
  );
}

function RangeDetail({
  from,
  to,
  transactions,
}: {
  from: string;
  to: string;
  transactions: {
    id: string;
    date: string;
    description?: string;
    category?: string;
    amount: number;
  }[];
}) {
  const total = transactions.reduce((s, t) => s + t.amount, 0);
  const byCategory = [
    ...transactions.reduce(
      (m, t) => m.set(t.category ?? "未分類", (m.get(t.category ?? "未分類") ?? 0) + t.amount),
      new Map<string, number>(),
    ),
  ].sort((a, b) => b[1] - a[1]);
  const label = (d: string) => `${Number(d.slice(5, 7))}月${Number(d.slice(8))}日`;

  return (
    <section className="min-w-0 flex-[2_1_560px] rounded-xl border bg-card p-5">
      <div className="mb-4 flex flex-wrap items-baseline gap-x-4 gap-y-2">
        <h2 className="text-base font-semibold">
          {from === to ? label(from) : `${label(from)} – ${label(to)}`}
        </h2>
        <div className="font-mono text-xl font-medium">{yen(total)}</div>
        <div className="text-[13px] text-muted-foreground">{transactions.length}件</div>
      </div>
      {byCategory.length > 0 && (
        <ul className="mb-5 flex flex-col gap-2">
          {byCategory.map(([category, amount], i) => (
            <li key={category} className="flex items-center gap-3">
              <span className="w-24 truncate text-[13px]">{category}</span>
              <span className="h-2.5 flex-1 rounded-full bg-background">
                <span
                  className={cn("block h-2.5 rounded-full", i === 0 ? "bg-expense" : "bg-heat-3")}
                  style={{ width: `${(amount / (byCategory[0]?.[1] ?? 1)) * 100}%` }}
                />
              </span>
              <span className="w-20 text-right font-mono text-[13px]">{yen(amount)}</span>
            </li>
          ))}
        </ul>
      )}
      <div className="overflow-x-auto">
        <table className="w-full min-w-[480px] border-collapse text-[13px]">
          <thead>
            <tr className="text-left text-muted-foreground">
              <th className="border-b py-2 pr-3 font-normal">日付</th>
              <th className="border-b py-2 pr-3 font-normal">内容</th>
              <th className="border-b py-2 pr-3 font-normal">大項目</th>
              <th className="border-b py-2 text-right font-normal">金額</th>
            </tr>
          </thead>
          <tbody>
            {transactions.slice(0, MAX_ROWS).map((t) => (
              <tr key={t.id}>
                <td className="border-b py-2.5 pr-3 font-mono text-muted-foreground">
                  {t.date.slice(5).replace("-", "/")} {WEEKDAYS[weekday(t.date)]}
                </td>
                <td className="border-b py-2.5 pr-3">{t.description ?? "（内容なし）"}</td>
                <td className="border-b py-2.5 pr-3 text-muted-foreground">{t.category}</td>
                <td className="border-b py-2.5 text-right font-mono">{yen(t.amount)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {transactions.length > MAX_ROWS && (
        <p className="mt-2 text-xs text-muted-foreground">
          ほか {transactions.length - MAX_ROWS} 件。範囲を狭めると全部見られます
        </p>
      )}
    </section>
  );
}
