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

const SITE_DESCRIPTION =
  "FragForge Studio is a free Windows app that turns CS2 demo files into upload-ready 1080x1920 60fps vertical Shorts - captured with HLAE + CS2 and edited entirely on your own PC. No account, no uploads.";

export const metadata: Metadata = {
  metadataBase: new URL("https://fragforge.gravityroom.app"),
  title: "FragForge Studio — Turn CS2 demos into viral Shorts",
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
    title: "FragForge Studio — Turn CS2 demos into viral Shorts",
    description: SITE_DESCRIPTION,
    url: "/",
  },
  twitter: {
    card: "summary_large_image",
    title: "FragForge Studio — Turn CS2 demos into viral Shorts",
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
