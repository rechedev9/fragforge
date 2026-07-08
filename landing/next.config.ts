import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // The VPS deploy (deploy/vps) runs the landing from the standalone server
  // bundle, same as web/; dev and `next start` are unaffected.
  output: "standalone",
};

export default nextConfig;
