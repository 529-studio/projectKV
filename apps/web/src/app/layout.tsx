import type { Metadata, Viewport } from "next";
import localFont from "next/font/local";
import "./globals.css";
import { LiveStreamProvider } from "../contexts/LiveStreamContext.ts";
const geistSans = localFont({
  src: "./fonts/GeistVF.woff",
  variable: "--font-geist-sans",
  weight: "100 900",
});
const geistMono = localFont({
  src: "./fonts/GeistMonoVF.woff",
  variable: "--font-geist-mono",
  weight: "100 900",
});

export const metadata: Metadata = {
  title: "Warded - Giúp đoạn đường của bạn đỡ chông gai hơn mỗi ngày",
  description: "Hệ thống giám sát giao thông thời gian thực và phân tích ùn tắc",
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
  themeColor: "#09090b",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="vi">
      <body
        className={`${geistSans.variable} ${geistMono.variable} antialiased`}
      >
        <LiveStreamProvider>
          {children}
        </LiveStreamProvider>
      </body>
    </html>
  );
}
