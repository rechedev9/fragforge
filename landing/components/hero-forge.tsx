"use client";

import { useEffect, useState } from "react";
import dynamic from "next/dynamic";

// The WebGL scene (three + postprocessing) is loaded only when motion is
// allowed, so reduced-motion users and the initial paint never pay for it.
const ForgeCanvas = dynamic(() => import("./forge-canvas"), { ssr: false });

/**
 * HeroForge is the light client boundary for the hero background. It decides,
 * on the client only, whether to mount the animated forge or a static designed
 * fallback, and picks the particle budget / DPR cap from the viewport.
 *
 * Accessibility contract: under prefers-reduced-motion NO canvas is ever
 * mounted; only the static CSS forge glow renders.
 */
export default function HeroForge() {
  const [mounted, setMounted] = useState(false);
  const [reducedMotion, setReducedMotion] = useState(true);
  const [config, setConfig] = useState<{ count: number; dpr: [number, number] }>({
    count: 14000,
    dpr: [1, 2],
  });

  useEffect(() => {
    setMounted(true);

    const isSmall = window.innerWidth < 768;
    setConfig({
      count: isSmall ? 7000 : 14000,
      dpr: isSmall ? [1, 1.5] : [1, 2],
    });

    const mql = window.matchMedia("(prefers-reduced-motion: reduce)");
    setReducedMotion(mql.matches);
    const onChange = (e: MediaQueryListEvent) => setReducedMotion(e.matches);
    mql.addEventListener("change", onChange);
    return () => mql.removeEventListener("change", onChange);
  }, []);

  const showCanvas = mounted && !reducedMotion;

  return (
    <div className="absolute inset-0 overflow-hidden" data-testid="hero-forge">
      {/* Static, always-present fallback: a designed cyan forge glow low-center
          (NEON HUD). Under reduced motion this is the whole visual. */}
      <div className="absolute inset-0 hero-forge-fallback" aria-hidden="true" />

      {showCanvas && <ForgeCanvas count={config.count} dpr={config.dpr} />}
    </div>
  );
}
