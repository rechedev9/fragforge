import { ImageResponse } from "next/og";
import { readFile } from "node:fs/promises";
import path from "node:path";

export const alt = "FragForge — Your best CS2 frags, ready to post";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

const CYAN = "#22d9ee";
const WHITE = "#f2fbff";
const PINK = "#ec4899";

export default async function OpengraphImage() {
  const heroImage = await readFile(
    path.join(process.cwd(), "public", "images", "hero-replay-forge-og.jpg"),
  );
  const heroSource = `data:image/jpeg;base64,${heroImage.toString("base64")}`;

  return new ImageResponse(
    (
      <div
        style={{
          position: "relative",
          height: "100%",
          width: "100%",
          display: "flex",
          overflow: "hidden",
          backgroundColor: "#050812",
        }}
      >
        <img
          alt=""
          src={heroSource}
          width={1200}
          height={630}
          style={{
            position: "absolute",
            inset: 0,
            width: "100%",
            height: "100%",
            objectFit: "cover",
          }}
        />

        <div
          style={{
            position: "absolute",
            inset: 0,
            display: "flex",
            backgroundImage:
              "linear-gradient(90deg, rgba(3,6,14,0.98) 0%, rgba(3,6,14,0.93) 38%, rgba(3,6,14,0.40) 72%, rgba(3,6,14,0.14) 100%)",
          }}
        />
        <div
          style={{
            position: "absolute",
            inset: 0,
            display: "flex",
            backgroundImage:
              "linear-gradient(0deg, rgba(3,6,14,0.92) 0%, transparent 35%, rgba(3,6,14,0.35) 100%)",
          }}
        />

        <div
          style={{
            position: "absolute",
            left: 0,
            top: 0,
            width: 10,
            height: "100%",
            display: "flex",
            background: `linear-gradient(180deg, ${CYAN}, ${PINK})`,
          }}
        />

        <div
          style={{
            position: "relative",
            width: "100%",
            display: "flex",
            flexDirection: "column",
            padding: "58px 70px 54px 76px",
          }}
        >
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
            }}
          >
            <div style={{ display: "flex", alignItems: "center", gap: 17 }}>
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "center",
                  width: 48,
                  height: 48,
                  border: `2px solid ${CYAN}`,
                  backgroundColor: "rgba(4,18,26,0.82)",
                }}
              >
                <svg
                  width="28"
                  height="28"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke={CYAN}
                  strokeWidth="2.4"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <circle cx="12" cy="12" r="7" />
                  <path d="M12 2v4M12 18v4M2 12h4M18 12h4" />
                </svg>
              </div>
              <div style={{ display: "flex", fontSize: 31, fontWeight: 700 }}>
                <span style={{ color: WHITE }}>FRAG</span>
                <span style={{ color: CYAN }}>//</span>
                <span style={{ color: WHITE }}>FORGE</span>
              </div>
            </div>

            <div
              style={{
                display: "flex",
                padding: "10px 17px",
                border: "1px solid rgba(255,255,255,0.25)",
                backgroundColor: "rgba(3,6,14,0.72)",
                color: WHITE,
                fontSize: 18,
                fontWeight: 700,
                letterSpacing: "0.08em",
              }}
            >
              FREE WINDOWS APP
            </div>
          </div>

          <div
            style={{
              display: "flex",
              marginTop: 74,
              color: CYAN,
              fontSize: 21,
              fontWeight: 700,
              letterSpacing: "0.16em",
            }}
          >
            CS2 DEMO → VERTICAL SHORT
          </div>

          <div
            style={{
              display: "flex",
              flexDirection: "column",
              marginTop: 18,
              fontSize: 72,
              fontWeight: 800,
              lineHeight: 0.98,
              color: WHITE,
              letterSpacing: "-0.035em",
            }}
          >
            <span>YOUR BEST FRAGS.</span>
            <span style={{ color: CYAN }}>READY TO POST.</span>
          </div>

          <div
            style={{
              display: "flex",
              marginTop: 28,
              color: "#c7d5e3",
              fontSize: 25,
            }}
          >
            Real HLAE capture. Native killfeed. 1080 × 1920 at 60 FPS.
          </div>

          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 16,
              marginTop: "auto",
              fontSize: 20,
              color: "#a7b7c8",
              letterSpacing: "0.04em",
            }}
          >
            <span style={{ color: CYAN, fontWeight: 700 }}>100% LOCAL</span>
            <span>•</span>
            <span>NO ACCOUNT</span>
            <span>•</span>
            <span>FRAGFORGE.GRAVITYROOM.APP</span>
          </div>
        </div>
      </div>
    ),
    { ...size },
  );
}
