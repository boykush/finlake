import type { FlowLayout, FlowLink, FlowNode } from "@/lib/flow";
import { yen } from "@/lib/format";
import { cn } from "@/lib/utils";

export const SANKEY_SIZE = { width: 820, height: 560 };
// 支出側のラベルは図の右に出す。その分の余白。
const LABEL_WIDTH = 200;

const linkColor: Record<FlowNode["role"], string> = {
  income: "stroke-income/25",
  shortfall: "stroke-expense/25",
  rest: "stroke-income/35",
  expense: "stroke-flow/30",
  hub: "",
};

const nodeColor: Record<FlowNode["role"], string> = {
  income: "fill-income",
  hub: "fill-income",
  rest: "fill-income",
  shortfall: "fill-expense",
  expense: "fill-flow",
};

function Band({ link, selected }: { link: FlowLink; selected: boolean }) {
  return (
    <path
      d={link.path}
      strokeWidth={link.width}
      className={cn(
        "transition-[stroke] duration-300",
        selected ? "stroke-expense/80" : linkColor[link.node.role],
        link.node.role === "expense" &&
          !selected &&
          "group-hover:stroke-flow/60 group-focus-visible:stroke-flow/60",
      )}
    />
  );
}

export function Sankey({
  layout,
  selected,
  hrefFor,
  onSelect,
}: {
  layout: FlowLayout;
  /** 選択中の支出の大項目。 */
  selected: string | undefined;
  hrefFor: (category: string) => string;
  onSelect: (category: string, event: React.MouseEvent) => void;
}) {
  const isSelected = (node: FlowNode) => node.role === "expense" && node.label === selected;

  const width = SANKEY_SIZE.width + LABEL_WIDTH;

  return (
    // 狭い画面では図を縮めずに横へ送る。縮めると細い帯のラベルが重なる。
    <div className="overflow-x-auto">
      <div className="relative min-w-[760px]">
        <svg
          viewBox={`0 0 ${width} ${SANKEY_SIZE.height}`}
          className="block h-auto w-full"
          role="img"
          aria-label="収入から各大項目への流れを帯の太さで示した図"
        >
          <g fill="none">
            {layout.links.map((link) =>
              link.node.role === "expense" ? (
                <a
                  key={link.id}
                  href={hrefFor(link.node.label)}
                  onClick={(event) => onSelect(link.node.label, event)}
                  className="group outline-none"
                  aria-label={`${link.node.label} ${yen(link.node.amount)}`}
                >
                  <Band link={link} selected={isSelected(link.node)} />
                </a>
              ) : (
                <Band key={link.id} link={link} selected={false} />
              ),
            )}
          </g>
          {layout.nodes.map((node) => (
            <rect
              key={node.id}
              x={node.x}
              y={node.y}
              width={node.width}
              height={node.height}
              className={cn(
                "transition-[fill] duration-300",
                isSelected(node) ? "fill-expense" : nodeColor[node.role],
              )}
            />
          ))}
        </svg>
        {/* ラベルは図と一緒に縮まないよう、SVG の外に重ねる。 */}
        {layout.nodes.map((node) => (
          <div
            key={node.id}
            aria-hidden="true"
            className={cn(
              "pointer-events-none absolute -translate-y-1/2 whitespace-nowrap text-xs leading-none transition-colors duration-300",
              isSelected(node)
                ? "text-expense"
                : node.role === "expense"
                  ? "text-muted-foreground"
                  : node.role === "rest"
                    ? "text-income"
                    : "text-foreground",
            )}
            style={{
              left: `calc(${((node.x + node.width) / width) * 100}% + 8px)`,
              top: `${((node.y + node.height / 2) / SANKEY_SIZE.height) * 100}%`,
            }}
          >
            {node.label}
            <span className="ml-1.5 font-mono">{yen(node.amount)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
