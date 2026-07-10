"use client";

import { useEffect, useRef, useState } from "react";

const DELAYS = ["delay-0", "delay-100", "delay-200"] as const;

export default function Reveal({
  children,
  className = "",
  delay = 0,
}: {
  children: React.ReactNode;
  className?: string;
  delay?: number;
}) {
  const ref = useRef<HTMLDivElement>(null);
  // Server-rendered content starts visible. Motion is an enhancement applied
  // after hydration, never a requirement for reading the page.
  const [visible, setVisible] = useState(true);

  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setVisible(true);
      return;
    }

    const element = ref.current;
    if (!element) return;
    if (element.getBoundingClientRect().top <= window.innerHeight * 0.9) return;

    setVisible(false);

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (!entry.isIntersecting) return;
        setVisible(true);
        observer.disconnect();
      },
      { threshold: 0.14 },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const delayClass = DELAYS[Math.min(Math.max(delay, 0), DELAYS.length - 1)];

  return (
    <div
      ref={ref}
      data-reveal
      className={[
        className,
        delayClass,
        "transform-gpu transition-[opacity,transform] duration-700 ease-out motion-reduce:transition-none",
        visible ? "translate-y-0 opacity-100" : "translate-y-10 opacity-0",
      ].join(" ")}
    >
      {children}
    </div>
  );
}
