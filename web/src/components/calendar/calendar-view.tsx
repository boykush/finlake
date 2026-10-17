"use client";

import { useRouter } from "next/navigation";
import { useOptimistic, useRef, useState, useTransition } from "react";
import type { Year } from "@/lib/calendar";
import { yen } from "@/lib/format";
import { type CalendarParams, calendarHref } from "@/lib/href";
import { cn } from "@/lib/utils";

const HEAT = ["bg-heat-0", "bg-heat-1", "bg-heat-2", "bg-heat-3", "bg-heat-4"];

type Range = { from: string; to: string };

function ordered(a: string, b: string): Range {
  return a <= b ? { from: a, to: b } : { from: b, to: a };
}

/**
 * 年のカレンダーと、その下に置く内訳（children）。日をドラッグして範囲を選ぶと、選んだ範囲は手を離す前から
 * 塗り分け、サーバーが内訳を描き終えるまで children を薄くしておく。
 */
export function CalendarView({
  year,
  params,
  heading,
  children,
}: {
  year: Year;
  params: CalendarParams;
  /** カレンダーの枠の上に置くもの。 */
  heading: React.ReactNode;
  children: React.ReactNode;
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [committed, setCommitted] = useOptimistic<Range>({ from: params.from, to: params.to });
  const [dragging, setDragging] = useState<Range>();
  const anchor = useRef<string>(undefined);
  const shown = dragging ?? committed;

  const dateAt = (x: number, y: number) =>
    (document.elementFromPoint(x, y) as HTMLElement | null)?.dataset.date;

  const commit = (range: Range) => {
    startTransition(() => {
      setCommitted(range);
      router.push(calendarHref({ ...params, ...range }), { scroll: false });
    });
  };

  const columns = Math.ceil(year.cells.length / 7);

  return (
    <>
      <section className="mb-4 rounded-xl border bg-card p-5">
        {heading}
        <div className="overflow-x-auto">
          <div className="min-w-[760px]">
            <div
              className="mb-1.5 grid font-mono text-[11px] text-muted-foreground"
              style={{ gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}
            >
              {year.monthColumns.map((m) => (
                <div key={m.month} style={{ gridColumnStart: m.column + 1 }}>
                  {m.month.slice(5)}
                </div>
              ))}
            </div>
            {/* 操作はポインタのドラッグ。キーボードと支援技術向けには下の日付の欄が同じことをする。 */}
            <div
              aria-hidden="true"
              className="grid touch-none select-none grid-flow-col grid-rows-7 gap-[3px]"
              style={{ gridAutoColumns: "minmax(0, 1fr)" }}
              onPointerDown={(e) => {
                const date = (e.target as HTMLElement).dataset.date;
                if (!date) return;
                anchor.current = date;
                setDragging({ from: date, to: date });
                e.currentTarget.setPointerCapture(e.pointerId);
              }}
              onPointerMove={(e) => {
                if (!anchor.current) return;
                const date = dateAt(e.clientX, e.clientY);
                if (date) setDragging(ordered(anchor.current, date));
              }}
              onPointerUp={() => {
                if (dragging) commit(dragging);
                anchor.current = undefined;
                setDragging(undefined);
              }}
            >
              {year.cells.map((cell, i) =>
                cell ? (
                  <div
                    key={cell.date}
                    data-date={cell.amount === undefined ? undefined : cell.date}
                    title={
                      cell.amount === undefined ? cell.date : `${cell.date} ${yen(cell.amount)}`
                    }
                    className={cn(
                      "aspect-square rounded-[3px]",
                      cell.amount === undefined
                        ? "border border-secondary"
                        : `${HEAT[cell.level]} cursor-pointer`,
                      cell.date >= shown.from && cell.date <= shown.to && "ring-2 ring-foreground",
                    )}
                  />
                ) : (
                  // biome-ignore lint/suspicious/noArrayIndexKey: 先頭の週の空きで、並びは変わらない
                  <div key={`pad-${i}`} />
                ),
              )}
            </div>
          </div>
        </div>

        <div className="mt-3.5 flex flex-wrap items-center gap-x-5 gap-y-2 text-xs text-muted-foreground">
          <form
            action="/calendar"
            className="flex flex-wrap items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              const data = new FormData(e.currentTarget);
              commit(ordered(String(data.get("from")), String(data.get("to"))));
            }}
          >
            <input type="hidden" name="year" value={params.year} />
            <input type="hidden" name="month" value={params.month} />
            {params.fixed && <input type="hidden" name="fixed" value="1" />}
            <label htmlFor="from">範囲</label>
            <input
              id="from"
              name="from"
              type="date"
              key={`from-${committed.from}`}
              defaultValue={committed.from}
              className="h-9 rounded-md border bg-background px-2 font-mono text-foreground"
            />
            <span aria-hidden="true">–</span>
            <label htmlFor="to" className="sr-only">
              範囲の終わり
            </label>
            <input
              id="to"
              name="to"
              type="date"
              key={`to-${committed.to}`}
              defaultValue={committed.to}
              className="h-9 rounded-md border bg-background px-2 font-mono text-foreground"
            />
            <button type="submit" className="h-9 rounded-md border bg-card px-3 text-foreground">
              見る
            </button>
          </form>
          <div className="ml-auto flex items-center gap-1" aria-hidden="true">
            <span>¥0</span>
            {HEAT.map((c) => (
              <span key={c} className={cn("size-3.5 rounded-[3px]", c)} />
            ))}
            <span>多い</span>
          </div>
        </div>
      </section>

      <div
        aria-busy={pending}
        className={cn("transition-opacity duration-200", pending && "opacity-40")}
      >
        {children}
      </div>
    </>
  );
}
