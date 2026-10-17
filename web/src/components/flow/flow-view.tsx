"use client";

import { useRouter } from "next/navigation";
import { useOptimistic, useTransition } from "react";
import type { FlowLayout } from "@/lib/flow";
import { flowHref } from "@/lib/href";
import { cn } from "@/lib/utils";
import { Sankey } from "./sankey";

/**
 * Sankey と大項目のボタン。選んだ大項目は押した時点で帯とボタンに反映し、サーバーが内訳を描き終えるまで
 * 右の内訳（children）を薄くしておく。
 */
export function FlowView({
  month,
  layout,
  categories,
  selected,
  children,
}: {
  month: string;
  layout: FlowLayout;
  categories: string[];
  selected: string | undefined;
  children: React.ReactNode;
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [shown, setShown] = useOptimistic(selected);

  const select = (category: string | undefined, event: React.MouseEvent) => {
    // 新しいタブで開くときはブラウザに任せる。
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0) return;
    event.preventDefault();
    startTransition(() => {
      setShown(category);
      router.push(flowHref(month, category), { scroll: false });
    });
  };

  return (
    <div className="flex flex-wrap items-start gap-4">
      <section className="min-w-0 flex-[999_1_620px] rounded-xl border bg-card p-5">
        <div className="mb-3 flex flex-wrap items-baseline gap-x-4 gap-y-1">
          <h1 className="text-lg font-semibold">お金の流れ</h1>
          <p className="text-[13px] text-muted-foreground">
            大項目を選ぶと、帯と右の内訳が一緒に切り替わります
          </p>
        </div>
        <Sankey
          layout={layout}
          selected={shown}
          hrefFor={(category) => flowHref(month, category)}
          onSelect={select}
        />
        <nav aria-label="支出の大項目" className="mt-4 flex flex-wrap gap-2">
          {[undefined, ...categories].map((category) => {
            const current = category === shown;
            return (
              <a
                key={category ?? ""}
                href={flowHref(month, category)}
                onClick={(event) => select(category, event)}
                aria-current={current ? "true" : undefined}
                className={cn(
                  "flex min-h-11 items-center rounded-full border px-3.5 text-sm transition-colors",
                  current
                    ? "border-expense bg-expense-muted text-expense"
                    : "bg-background hover:bg-muted",
                )}
              >
                {category ?? "すべて"}
              </a>
            );
          })}
        </nav>
      </section>
      <div
        aria-busy={pending}
        className={cn(
          "flex min-w-0 flex-[1_1_340px] transition-opacity duration-200",
          pending && "opacity-40",
        )}
      >
        {children}
      </div>
    </div>
  );
}
