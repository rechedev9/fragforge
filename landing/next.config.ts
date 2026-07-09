import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Standalone build for whatever static/Node host serves the landing page
  // (deployment target undecided since the VPS deploy was removed); dev and
  // `next start` are unaffected.
  output: "standalone",
};

export default nextConfig;
