import './globals.css';
import type { Metadata } from 'next';
import { Space_Grotesk, Inter, JetBrains_Mono } from 'next/font/google';
import { SessionProvider } from '@/lib/session';
import { GrainOverlay } from '@/components/brand/grain-overlay';
import { Toaster } from '@/components/ui/sonner';

const spaceGrotesk = Space_Grotesk({
  subsets: ['latin'],
  variable: '--font-space-grotesk',
});

const inter = Inter({
  subsets: ['latin'],
  variable: '--font-inter',
});

const jetbrainsMono = JetBrains_Mono({
  subsets: ['latin'],
  variable: '--font-jetbrains-mono',
});

export const metadata: Metadata = {
  title: 'FragForge',
  description: 'Forge your CS2 frags into highlight reels — captured on your own rig.',
};

const fontVars = `${spaceGrotesk.variable} ${inter.variable} ${jetbrainsMono.variable}`;

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark">
      <body className={`${fontVars} bg-background text-foreground antialiased`}>
        <SessionProvider>{children}</SessionProvider>
        <GrainOverlay />
        <Toaster />
      </body>
    </html>
  );
}
