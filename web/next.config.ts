import type { NextConfig } from "next";

const config: NextConfig = {
  // イメージには server.js と要る依存だけを入れる。
  output: "standalone",
  // 画面はどれも都度描くので、使わないキャッシュに既定の 50MB をメモリで持たせない。
  cacheMaxMemorySize: 0,
};

export default config;
