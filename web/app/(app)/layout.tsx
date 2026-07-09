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
    <SidebarProvider style={{ '--sidebar-width': '240px' } as React.CSSProperties}>
      <AppSidebar />
      {/* SidebarInset paints an opaque bg-background over <body>, so it needs
          its own neon-grid layer for the HUD grid to show in the content area. */}
      <SidebarInset className="neon-grid">
        <header className="sticky top-0 z-30 flex h-14 items-center gap-3 border-b border-border bg-background/90 px-4 backdrop-blur-md md:hidden">
          <SidebarTrigger className="-ml-2 size-10" />
          <Wordmark katakana={false} />
        </header>
        <main className="mx-auto w-full max-w-[1440px] flex-1 px-4 py-8 sm:px-6 md:px-10 md:py-12 lg:px-12">
          {children}
        </main>
      </SidebarInset>
    </SidebarProvider>
  );
}
