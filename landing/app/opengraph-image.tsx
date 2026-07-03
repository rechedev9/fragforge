import { ImageResponse } from "next/og";

// Edge-safe social card: charcoal background, lime accent, wordmark + tagline.
// Uses only inline styles and satori's default fonts (no external asset fetch).
export const runtime = "edge";

export const alt = "FragForge Studio — Turn CS2 demos into viral Shorts";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

const CHARCOAL = "#0f1013";
const LIME = "#c2f042";
const LIME_DARK = "#1e2a08";
const WHITE = "#fafafa";
const MUTED = "#9a9ba3";
const BORDER = "#2a2c31";

export default function OpengraphImage() {
  return new ImageResponse(
    (
      <div
        style={{
          height: "100%",
          width: "100%",
          display: "flex",
          flexDirection: "column",
          alignItems: "flex-start",
          justifyContent: "center",
          backgroundColor: CHARCOAL,
          backgroundImage: `radial-gradient(1000px 520px at 50% 118%, rgba(194,240,66,0.20), transparent 60%)`,
          padding: "88px 96px",
        }}
      >
        {/* Wordmark row */}
        <div style={{ display: "flex", alignItems: "center", gap: 24 }}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              width: 84,
              height: 84,
              borderRadius: 20,
              backgroundColor: LIME,
            }}
          >
            <svg
              width="48"
              height="48"
              viewBox="0 0 24 24"
              fill="none"
              stroke={LIME_DARK}
              strokeWidth="2.4"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M8.5 14.5A2.5 2.5 0 0 0 11 12c0-1.38-.5-2-1-3-1.072-2.143-.224-4.054 2-6 .5 2.5 2 4.9 4 6.5 2 1.6 3 3.5 3 5.5a7 7 0 1 1-14 0c0-1.153.433-2.294 1-3a2.5 2.5 0 0 0 2.5 2.5z" />
            </svg>
          </div>
          <div style={{ display: "flex", fontSize: 52, fontWeight: 700 }}>
            <span style={{ color: LIME }}>Frag</span>
            <span style={{ color: WHITE }}>Forge</span>
            <span style={{ color: MUTED, marginLeft: 18 }}>Studio</span>
          </div>
        </div>

        {/* Headline */}
        <div
          style={{
            display: "flex",
            marginTop: 56,
            fontSize: 82,
            fontWeight: 700,
            lineHeight: 1.05,
            color: WHITE,
            letterSpacing: "-0.02em",
          }}
        >
          Turn CS2 demos into
        </div>
        <div
          style={{
            display: "flex",
            fontSize: 82,
            fontWeight: 700,
            lineHeight: 1.05,
            letterSpacing: "-0.02em",
          }}
        >
          <span style={{ color: WHITE }}>viral&nbsp;</span>
          <span style={{ color: LIME }}>Shorts</span>
        </div>

        {/* Tagline */}
        <div
          style={{
            display: "flex",
            marginTop: 40,
            fontSize: 30,
            color: MUTED,
          }}
        >
          Free Windows app · captures &amp; edits locally · your demos never leave
          your PC
        </div>

        {/* Version pill */}
        <div
          style={{
            display: "flex",
            marginTop: 48,
            alignItems: "center",
            gap: 14,
            padding: "12px 22px",
            borderRadius: 12,
            border: `1px solid ${BORDER}`,
            color: WHITE,
            fontSize: 26,
          }}
        >
          <span style={{ color: LIME }}>↓</span>
          v0.2.7 · 124 MB · Windows 10/11
        </div>
      </div>
    ),
    { ...size },
  );
}
