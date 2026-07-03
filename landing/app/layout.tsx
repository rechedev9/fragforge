import type { Metadata } from "next";
import { Space_Grotesk, Inter, JetBrains_Mono } from "next/font/google";
import "./globals.css";

const spaceGrotesk = Space_Grotesk({
  variable: "--font-space-grotesk",
  subsets: ["latin"],
  display: "swap",
});

const inter = Inter({
  variable: "--font-inter",
  subsets: ["latin"],
  display: "swap",
});

const jetbrainsMono = JetBrains_Mono({
  variable: "--font-jetbrains-mono",
  subsets: ["latin"],
  display: "swap",
});

const SITE_DESCRIPTION =
  "FragForge Studio is a free Windows app that turns CS2 demo files into upload-ready 1080x1920 60fps vertical Shorts - captured with HLAE + CS2 and edited entirely on your own PC. No account, no uploads.";

export const metadata: Metadata = {
  metadataBase: new URL("https://fragforge-landing.vercel.app"),
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
        className={`${spaceGrotesk.variable} ${inter.variable} ${jetbrainsMono.variable} bg-background text-foreground antialiased`}
      >
        {children}
      </body>
    </html>
  );
}
