const number = new Intl.NumberFormat("ja-JP");

export function yen(amount: number): string {
  return `¥${number.format(Math.abs(amount))}`;
}

/** 符号つきの金額。減った分は U+2212 で示す。 */
export function signedYen(amount: number): string {
  if (amount === 0) return "±¥0";
  return `${amount > 0 ? "+" : "−"}${yen(amount)}`;
}

export function percent(ratio: number): string {
  return `${(ratio * 100).toFixed(1)}%`;
}
