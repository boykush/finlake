import type { NextConfig } from "next";

const config: NextConfig = {
  // イメージには server.js と要る依存だけを入れる。
  output: "standalone",
};

export default config;
