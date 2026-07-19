'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import {
  Clapperboard,
  Compass,
  Crosshair,
  Film,
  Newspaper,
  Settings,
  UploadCloud,
  type LucideIcon,
} from 'lucide-react';
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from '@/components/ui/sidebar';
import { BrandMark, Wordmark } from '@/components/brand/wordmark';
import { CaptureReadiness } from '@/components/shell/capture-readiness';
import { NAV_SECTIONS, type NavHref } from '@/lib/nav';
import { cn } from '@/lib/utils';

/** Per-section presentation the shared nav data deliberately leaves out.
 * `stream` remains a category signal, never the global selection colour. */
const NAV_META: Record<NavHref, { icon: LucideIcon; stream?: boolean }> = {
  '/matches': { icon: Crosshair },
  '/upload': { icon: UploadCloud },
  '/streams': { icon: Clapperboard, stream: true },
  '/news': { icon: Newspaper },
  '/videos': { icon: Film },
  '/feed': { icon: Compass },
  '/settings': { icon: Settings },
};

const NAV_ITEMS = NAV_SECTIONS.map((section) => ({ ...section, ...NAV_META[section.href] }));

/** A nav href is active for its exact page and any nested route under it. */
function isActiveHref(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(`${href}/`);
}

/**
 * The persistent left sidebar shell: brand lockup up top, the numbered 01-07
 * nav (active item gets the inset accent bar; the active destination always
 * uses cyan), and a footer with the local CAPTURA readiness card.
 */
export function AppSidebar() {
  const pathname = usePathname();

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader className="p-0 px-6 pt-7 group-data-[collapsible=icon]:px-2">
        <Link
          href="/matches"
          aria-label="Ir a Partidas"
          className="inline-flex min-h-10 items-center group-data-[collapsible=icon]:justify-center"
        >
          <Wordmark className="group-data-[collapsible=icon]:hidden" />
          <BrandMark className="hidden size-7 group-data-[collapsible=icon]:block" />
        </Link>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup className="p-0 pt-8">
          <SidebarMenu className="gap-1">
            {NAV_ITEMS.map((item, index) => {
              const active = isActiveHref(pathname, item.href);
              const Icon = item.icon;
              return (
                <SidebarMenuItem
                  key={item.href}
                  className={cn(index === 4 && 'mt-4 border-t border-sidebar-border pt-4')}
                >
                  <SidebarMenuButton
                    asChild
                    isActive={active}
                    tooltip={item.label}
                    className={cn(
                      'h-12 gap-3 rounded-md px-6 font-[family-name:var(--font-display)] text-[13px] font-semibold uppercase tracking-[0.06em] text-sidebar-foreground transition-colors hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground group-data-[collapsible=icon]:size-10! group-data-[collapsible=icon]:p-2.5!',
                      // Neutralize the shadcn active defaults so the HUD
                      // gradient + inset bar below are what actually shows.
                      'data-[active=true]:bg-transparent data-[active=true]:font-semibold',
                      active &&
                        'bg-gradient-to-r from-primary/12 to-transparent shadow-[inset_3px_0_0_var(--primary)] data-[active=true]:text-primary',
                    )}
                  >
                    <Link
                      href={item.href}
                      aria-label={item.label}
                      aria-current={active ? 'page' : undefined}
                    >
                      <Icon className="size-4 shrink-0 group-data-[collapsible=icon]:size-[18px]" aria-hidden />
                      <span
                        aria-hidden
                        className={cn(
                          'font-[family-name:var(--font-mono)] text-[11px] text-sidebar-foreground/70 group-data-[collapsible=icon]:hidden',
                          active && 'text-primary',
                        )}
                      >
                        {item.number}
                      </span>
                      <span className="flex min-w-0 flex-1 items-center gap-2 group-data-[collapsible=icon]:hidden">
                        <span className="truncate">{item.label}</span>
                        {item.stream ? <span className="size-1.5 shrink-0 rounded-full bg-stream" aria-hidden /> : null}
                      </span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              );
            })}
          </SidebarMenu>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter className="gap-3 px-4 pb-5 group-data-[collapsible=icon]:px-1">
        <CaptureReadiness />
      </SidebarFooter>
    </Sidebar>
  );
}
