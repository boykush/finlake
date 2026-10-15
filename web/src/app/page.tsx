import { Code, ConnectError } from "@connectrpc/connect";
import { ChevronLeft, ChevronRight } from "lucide-react";
import Link from "next/link";
import { SANKEY_SIZE, Sankey } from "@/components/flow/sankey";
import { buttonVariants } from "@/components/ui/button";
import type { GetMonthlySummaryResponse } from "@/gen/finlake/v1/ledger_pb";
import { type FlowCategory, layoutFlow } from "@/lib/flow";
import { percent, signedYen, yen } from "@/lib/format";
import { ledger } from "@/lib/ledger";
import { monthsUntil } from "@/lib/month";
import { cn } from "@/lib/utils";

const TREND_MONTHS = 6;
const TOP_TRANSACTIONS = 5;

function href(month: string, category?: string): string {
  const params = new URLSearchParams({ month });
  if (category) params.set("category", category);
  return `/?${params}`;
}

async function summaryOf(month: string): Promise<GetMonthlySummaryResponse | undefined> {
  try {
    return await ledger().getMonthlySummary({ month });
  } catch (err) {
    // 振替だけの月など、収支に入る明細が無い月。
    if (err instanceof ConnectError && err.code === Code.NotFound) return undefined;
    throw err;
  }
}

// マネーフォワードの収入は大項目が「収入」の1つなので、収入側は中項目で分ける。
async function incomesOf(month: string): Promise<FlowCategory[]> {
  const { transactions } = await ledger().listTransactions({ month, kind: "income", limit: 1000 });
  const totals = new Map<string, number>();
  for (const t of transactions) {
    const name = t.subcategory ?? t.category ?? "収入";
    totals.set(name, (totals.get(name) ?? 0) + Number(t.amount));
  }
  return [...totals]
    .map(([category, amount]) => ({ kind: "income", category, amount }))
    .sort((a, b) => b.amount - a.amount);
}

export default async function FlowPage({
  searchParams,
}: {
  searchParams: Promise<{ month?: string; category?: string }>;
}) {
  const params = await searchParams;
  const { months } = await ledger().listMonths({});
  const available = months.map((m) => m.month);
  const month = available.find((m) => m === params.month) ?? available[0];
  if (!month) {
    return (
      <Shell>
        <p className="text-muted-foreground">まだ明細がありません。</p>
      </Shell>
    );
  }
  const index = available.indexOf(month);
  const [summary, incomes] = await Promise.all([summaryOf(month), incomesOf(month)]);

  const expenses: FlowCategory[] = (summary?.categories ?? [])
    .filter((c) => c.kind === "expense")
    .map((c) => ({ kind: c.kind, category: c.category, amount: Number(c.amount) }));
  const categories = [...incomes, ...expenses];
  const selected = expenses.find((c) => c.category === params.category) ?? expenses[0];

  return (
    <Shell
      nav={
        <div className="ml-auto flex items-center gap-2">
          <MonthLink month={available[index + 1]} label="前の月">
            <ChevronLeft />
          </MonthLink>
          <div className="min-w-20 text-center font-mono text-[15px]">{month}</div>
          <MonthLink month={available[index - 1]} label="次の月">
            <ChevronRight />
          </MonthLink>
        </div>
      }
    >
      {summary ? (
        <>
          <dl className="mb-5 grid grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-3">
            <Stat label="収入" value={yen(Number(summary.income))} className="text-income" />
            <Stat label="支出" value={yen(Number(summary.expense))} />
            <Stat label="収支" value={signedYen(Number(summary.balance))} />
            <Stat
              label="残した割合"
              value={
                summary.income > 0n
                  ? percent(Number(summary.balance) / Number(summary.income))
                  : "—"
              }
            />
          </dl>
          <div className="flex flex-wrap items-start gap-4">
            <section className="min-w-0 flex-[999_1_620px] rounded-xl border bg-card p-5">
              <div className="mb-3 flex flex-wrap items-baseline gap-x-4 gap-y-1">
                <h1 className="text-lg font-semibold">お金の流れ</h1>
                <p className="text-[13px] text-muted-foreground">
                  大項目を選ぶと、帯と右の内訳が一緒に切り替わります
                </p>
              </div>
              <Sankey
                layout={layoutFlow(categories, SANKEY_SIZE)}
                selected={selected?.category}
                hrefFor={(category) => href(month, category)}
              />
              <nav aria-label="支出の大項目" className="mt-4 flex flex-wrap gap-2">
                {expenses.map((c) => {
                  const current = c.category === selected?.category;
                  return (
                    <Link
                      key={c.category}
                      href={href(month, c.category)}
                      scroll={false}
                      aria-current={current ? "true" : undefined}
                      className={cn(
                        "flex min-h-11 items-center rounded-full border px-3.5 text-sm transition-colors",
                        current
                          ? "border-expense bg-expense-muted text-expense"
                          : "bg-background hover:bg-muted",
                      )}
                    >
                      {c.category}
                    </Link>
                  );
                })}
              </nav>
            </section>
            {selected && (
              <CategoryDetail month={month} category={selected} expense={Number(summary.expense)} />
            )}
          </div>
        </>
      ) : (
        <p className="text-muted-foreground">この月は収支に入る明細がありません。</p>
      )}
    </Shell>
  );
}

function Shell({ nav, children }: { nav?: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="mx-auto max-w-[1360px] px-6 pt-7 pb-12 text-sm">
      <header className="mb-7 flex flex-wrap items-center gap-x-7 gap-y-3">
        <div className="font-mono text-lg font-medium">finlake</div>
        {nav}
      </header>
      <main>{children}</main>
    </div>
  );
}

function MonthLink({
  month,
  label,
  children,
}: {
  month: string | undefined;
  label: string;
  children: React.ReactNode;
}) {
  const className = cn(buttonVariants({ variant: "outline", size: "icon" }), "size-11 bg-card");
  // 取り込み済みの端では、押せない見た目だけを残す。
  if (!month) {
    return (
      <span aria-hidden="true" className={cn(className, "opacity-40")}>
        {children}
      </span>
    );
  }
  return (
    <Link href={href(month)} aria-label={label} className={className}>
      {children}
    </Link>
  );
}

function Stat({ label, value, className }: { label: string; value: string; className?: string }) {
  return (
    <div className="rounded-xl border bg-card px-4.5 py-4">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={cn("font-mono text-[26px] font-medium", className)}>{value}</dd>
    </div>
  );
}

async function CategoryDetail({
  month,
  category,
  expense,
}: {
  month: string;
  category: FlowCategory;
  expense: number;
}) {
  const range = monthsUntil(month, TREND_MONTHS);
  const [trend, list] = await Promise.all([
    ledger().getCategoryTrend({ category: category.category, from: range[0], to: month }),
    ledger().listTransactions({
      month,
      kind: "expense",
      category: category.category,
      limit: 1000,
    }),
  ]);
  const byMonth = new Map(trend.points.map((p) => [p.month, Number(p.amount)]));
  const bars = range.map((m) => ({ month: m, amount: byMonth.get(m) ?? 0 }));
  const max = Math.max(...bars.map((b) => b.amount), 1);
  const previous = bars.at(-2)?.amount ?? 0;
  const top = list.transactions
    .map((t) => ({ ...t, amount: Number(t.amount) }))
    .sort((a, b) => a.amount - b.amount)
    .slice(0, TOP_TRANSACTIONS);

  return (
    <aside className="flex min-w-0 flex-[1_1_340px] flex-col gap-5 rounded-xl border bg-card p-5">
      <div>
        <div className="text-xs text-muted-foreground">選択中の大項目</div>
        <h2 className="mt-0.5 text-[22px] font-semibold text-expense">{category.category}</h2>
        <div className="mt-1 font-mono text-3xl font-medium">{yen(category.amount)}</div>
        <dl className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-[13px] text-muted-foreground">
          <div className="flex gap-1.5">
            <dt>支出の</dt>
            <dd className="font-mono text-foreground">{percent(category.amount / expense)}</dd>
          </div>
          <div className="flex gap-1.5">
            <dt>前月比</dt>
            <dd className="font-mono text-foreground">{signedYen(category.amount - previous)}</dd>
          </div>
        </dl>
      </div>

      <div>
        <div className="mb-2 text-xs text-muted-foreground">直近{TREND_MONTHS}か月</div>
        <ol className="flex gap-2">
          {bars.map((bar) => (
            <li key={bar.month} className="flex flex-1 flex-col gap-1.5">
              <div className="flex h-21 items-end">
                <div
                  className={cn(
                    "w-full rounded-t transition-[height] duration-300",
                    bar.month === month ? "bg-expense" : "bg-input",
                  )}
                  style={{ height: `${Math.max((bar.amount / max) * 100, 2)}%` }}
                  title={yen(bar.amount)}
                />
              </div>
              <div
                className={cn(
                  "text-center font-mono text-[11px]",
                  bar.month === month ? "text-foreground" : "text-muted-foreground",
                )}
              >
                {bar.month.slice(5)}
              </div>
            </li>
          ))}
        </ol>
      </div>

      <div>
        <div className="mb-1 text-xs text-muted-foreground">
          金額の大きい明細（{list.transactions.length}件のうち）
        </div>
        <ul>
          {top.map((t) => (
            <li key={t.id} className="flex items-baseline gap-3 border-b py-2.5 last:border-b-0">
              <span className="w-10 font-mono text-xs text-muted-foreground">
                {Number(t.date.slice(8))}日
              </span>
              <span className="min-w-0 flex-1 truncate">{t.description ?? "（内容なし）"}</span>
              <span className="font-mono">{yen(t.amount)}</span>
            </li>
          ))}
        </ul>
      </div>
    </aside>
  );
}
