import type { NextConfig } from "next";
import path from "node:path";

const extraAllowedDevOrigins = (process.env.ALLOWED_DEV_ORIGINS || "")
  .split(",")
  .map((item) => item.trim())
  .filter(Boolean);

const nextConfig: NextConfig = {
  turbopack: {
    root: path.join(__dirname),
  },
  allowedDevOrigins: [
    "127.0.0.1",
    "localhost",
    "0.0.0.0",
    "192.168.208.4",
    "run-agent-69eb0fa915bd16dca16c2d4f-mogyq8p7-preview.agent-sandbox-my-b1-gw.trae.ai",
    ...extraAllowedDevOrigins,
  ],
};

export default nextConfig;
