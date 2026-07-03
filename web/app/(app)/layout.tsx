import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar';
import { AppSidebar } from '@/components/shell/app-sidebar';
import { Wordmark } from '@/components/brand/wordmark';

/**
 * Authenticated app shell. Renders the persistent left sidebar (which collapses
 * to an icon rail on desktop and to a Sheet on mobile) alongside the routed
 * screen. Onboarding screens live outside this group and have no sidebar.
 */
export default function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    // The NEON HUD mockup sidebar is 232px wide; the shadcn default is 16rem.
    <SidebarProvider style={{ '--sidebar-width': '232px' } as React.CSSProperties}>
      <AppSidebar />
      {/* SidebarInset paints an opaque bg-background over <body>, so it needs
          its own neon-grid layer for the HUD grid to show in the content area. */}
      <SidebarInset className="neon-grid">
        <header className="sticky top-0 z-30 flex h-12 items-center gap-2 border-b border-border bg-background/80 px-3 backdrop-blur md:hidden">
          <SidebarTrigger />
          <Wordmark katakana={false} />
        </header>
        <main className="mx-auto w-full max-w-[1200px] flex-1 px-4 py-6 md:px-8 md:py-10">
          {children}
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}
