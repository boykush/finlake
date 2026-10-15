import "server-only";
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-node";
import { LedgerService } from "@/gen/finlake/v1/ledger_pb";

// API はクラスタの中だけにあり、ブラウザからは呼ばない。呼ぶのはサーバーコンポーネント。
function baseUrl(): string {
  const url = process.env.FINLAKE_API_URL;
  if (!url) {
    throw new Error("FINLAKE_API_URL is not set");
  }
  return url;
}

export function ledger() {
  return createClient(
    LedgerService,
    createConnectTransport({ baseUrl: baseUrl(), httpVersion: "1.1" }),
  );
}
