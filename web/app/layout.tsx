import './globals.css';
import type { Metadata } from 'next';
import { Chakra_Petch, Share_Tech_Mono } from 'next/font/google';
import { SessionProvider } from '@/lib/session';
import { GrainOverlay } from '@/components/brand/grain-overlay';
import { Toaster } from '@/components/ui/sonner';

const chakraPetch = Chakra_Petch({
  subsets: ['latin'],
  weight: ['400', '500', '600', '700'],
  variable: '--font-chakra-petch',
});

const shareTechMono = Share_Tech_Mono({
  subsets: ['latin'],
  weight: '400',
  variable: '--font-share-tech-mono',
});

export const metadata: Metadata = {
  title: 'FragForge',
  description: 'Forge your CS2 frags into highlight reels — captured on your own rig.',
};

const fontVars = `${chakraPetch.variable} ${shareTechMono.variable}`;

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    // The next/font variable classes live on <html> so the composed
    // --font-sans/--font-mono/--font-display tokens in globals.css resolve at
    // :root (declared on <body> they would compute to guaranteed-invalid at
    // :root and the whole app would silently fall back to system fonts).
    <html lang="en" className={`dark ${fontVars}`}>
      <body className="neon-grid bg-background text-foreground antialiased">
        <SessionProvider>{children}</SessionProvider>
        <GrainOverlay />
        <Toaster />
      </body>
    </html>
  );
}
