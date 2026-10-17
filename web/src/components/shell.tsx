import { ChevronLeft, ChevronRight } from "lucide-react";
import Link from "next/link";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const VIEWS = [
  { href: "/", label: "フロー" },
  { href: "/calendar", label: "カレンダー" },
] as const;

export function Shell({
  current,
  actions,
  children,
}: {
  current: (typeof VIEWS)[number]["href"];
  actions?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="mx-auto max-w-[1360px] px-6 pt-7 pb-12 text-sm">
      <header className="mb-7 flex flex-wrap items-center gap-x-7 gap-y-3">
        <div className="font-mono text-lg font-medium">finlake</div>
        <nav aria-label="ビュー" className="flex flex-wrap gap-1">
          {VIEWS.map((view) => (
            <Link
              key={view.href}
              href={view.href}
              aria-current={view.href === current ? "page" : undefined}
              className={cn(
                "rounded-lg px-3.5 py-2.5 transition-colors",
                view.href === current
                  ? "bg-secondary text-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {view.label}
            </Link>
          ))}
        </nav>
        {actions && <div className="ml-auto flex items-center gap-2">{actions}</div>}
      </header>
      <main>{children}</main>
    </div>
  );
}

/** 前後への移動。行き先が無い端では、押せない見た目だけを残す。 */
export function Stepper({
  label,
  previous,
  next,
}: {
  label: string;
  previous: { href: string; label: string } | undefined;
  next: { href: string; label: string } | undefined;
}) {
  return (
    <>
      <StepLink to={previous}>
        <ChevronLeft />
      </StepLink>
      <div className="min-w-20 text-center font-mono text-[15px]">{label}</div>
      <StepLink to={next}>
        <ChevronRight />
      </StepLink>
    </>
  );
}

function StepLink({
  to,
  children,
}: {
  to: { href: string; label: string } | undefined;
  children: React.ReactNode;
}) {
  const className = cn(buttonVariants({ variant: "outline", size: "icon" }), "size-11 bg-card");
  if (!to) {
    return (
      <span aria-hidden="true" className={cn(className, "opacity-40")}>
        {children}
      </span>
    );
  }
  return (
    <Link href={to.href} aria-label={to.label} className={className}>
      {children}
    </Link>
  );
}

export function Stat({
  label,
  value,
  note,
  className,
}: {
  label: string;
  value: React.ReactNode;
  note?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className="rounded-xl border bg-card px-4.5 py-4">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={cn("font-mono text-[26px] font-medium", className)}>{value}</dd>
      {note && <dd className="text-xs text-muted-foreground">{note}</dd>}
    </div>
  );
}
