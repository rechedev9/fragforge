import type { Metadata } from "next";
import { Chakra_Petch, Share_Tech_Mono } from "next/font/google";
import "./globals.css";

// NEON HUD type system: Chakra Petch for display/sans, Share Tech Mono for
// every number/label/eyebrow. Same next/font/google wiring as web/app/layout.tsx.
const chakraPetch = Chakra_Petch({
  variable: "--font-chakra-petch",
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
  display: "swap",
});

const shareTechMono = Share_Tech_Mono({
  variable: "--font-share-tech-mono",
  subsets: ["latin"],
  weight: "400",
  display: "swap",
});

const SOCIAL_TITLE = "Your CS2 frags, ready to post | FragForge";
const SITE_DESCRIPTION =
  "Turn CS2 demos and stream moments into polished vertical Shorts with local capture, ready-to-post edits and optional xAI subtitles.";

export const metadata: Metadata = {
  metadataBase: new URL("https://fragforge.gravityroom.app"),
  title: SOCIAL_TITLE,
  description: SITE_DESCRIPTION,
  applicationName: "FragForge Studio",
  keywords: [
    "CS2",
    "Counter-Strike 2",
    "demo to video",
    "vertical Shorts",
    "HLAE",
    "highlights",
    "frag movie",
  ],
  openGraph: {
    type: "website",
    siteName: "FragForge Studio",
    title: SOCIAL_TITLE,
    description: SITE_DESCRIPTION,
    url: "/",
  },
  twitter: {
    card: "summary_large_image",
    title: SOCIAL_TITLE,
    description: SITE_DESCRIPTION,
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" className="dark">
      <body
        className={`${chakraPetch.variable} ${shareTechMono.variable} bg-[#050812] text-[#f2fbff] antialiased`}
      >
        {children}
      </body>
    </html>
  );
}
