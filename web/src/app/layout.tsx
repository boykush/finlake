import type { Metadata } from "next";
import { IBM_Plex_Mono, IBM_Plex_Sans_JP } from "next/font/google";
import "./globals.css";

const sans = IBM_Plex_Sans_JP({
  weight: ["400", "500", "600"],
  variable: "--font-plex-sans",
  preload: false,
});
const mono = IBM_Plex_Mono({
  weight: ["400", "500"],
  subsets: ["latin"],
  variable: "--font-plex-mono",
});

export const metadata: Metadata = {
  title: "finlake",
  // 家計は非公開。前段の認証をすり抜けたときにも索引されないようにする。
  robots: { index: false, follow: false },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="ja" className={`${sans.variable} ${mono.variable}`}>
      <body className="min-h-dvh antialiased">{children}</body>
    </html>
  );
}
