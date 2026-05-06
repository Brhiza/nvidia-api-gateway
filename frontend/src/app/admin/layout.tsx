import type { Metadata } from "next";
import "../globals.css";

import AdminShell from "@/components/dashboard/admin-shell";

export const metadata: Metadata = {
  title: "NVIDIA API 网关 | 管理后台",
  description: "NVIDIA API 网关管理后台",
};

export default function AdminLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return <AdminShell>{children}</AdminShell>;
}
