'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
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
import { Wordmark } from '@/components/brand/wordmark';
import { CaptureReadiness } from '@/components/shell/capture-readiness';
import { cn } from '@/lib/utils';

type NavItem = {
  /** Mono HUD index rendered before the label ("01".."05"). */
  number: string;
  label: string;
  href: string;
  /** Active accent: the stream route lights magenta, everything else cyan. */
  accent: 'cyan' | 'magenta';
};

const NAV_ITEMS: NavItem[] = [
  { number: '01', label: 'Partidas', href: '/matches', accent: 'cyan' },
  { number: '02', label: 'Subir demo', href: '/upload', accent: 'cyan' },
  { number: '03', label: 'Clips de stream', href: '/streams', accent: 'magenta' },
  { number: '04', label: 'Biblioteca', href: '/videos', accent: 'cyan' },
  { number: '05', label: 'Feed', href: '/feed', accent: 'cyan' },
];

/** A nav href is active for its exact page and any nested route under it. */
function isActiveHref(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(`${href}/`);
}

/**
 * The persistent left sidebar shell, NEON HUD style: wordmark + katakana up
 * top, the numbered 01-05 nav (active item gets the inset accent bar; the
 * stream route activates in magenta, the rest in cyan), and a footer with the
 * CAPTURA readiness card.
 */
export function AppSidebar() {
  const pathname = usePathname();

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader className="p-0 px-6 pt-6 group-data-[collapsible=icon]:px-2">
        <Link href="/matches" className="inline-flex group-data-[collapsible=icon]:justify-center">
          <Wordmark className="group-data-[collapsible=icon]:hidden" />
        </Link>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup className="p-0 pt-9">
          <SidebarMenu className="gap-0.5">
            {NAV_ITEMS.map((item) => {
              const active = isActiveHref(pathname, item.href);
              const magenta = item.accent === 'magenta';
              let numberTone: string;
              if (!active) {
                numberTone = 'text-sidebar-foreground/60';
              } else if (magenta) {
                numberTone = 'text-destructive';
              } else {
                numberTone = 'text-primary';
              }
              return (
                <SidebarMenuItem key={item.href}>
                  <SidebarMenuButton
                    asChild
                    isActive={active}
                    tooltip={item.label}
                    className={cn(
                      'h-auto gap-3 rounded-none px-6 py-[11px] font-[family-name:var(--font-display)] text-[13px] font-semibold uppercase tracking-[0.08em] text-sidebar-foreground',
                      // Neutralize the shadcn active defaults so the HUD
                      // gradient + inset bar below are what actually shows.
                      'data-[active=true]:bg-transparent data-[active=true]:font-semibold',
                      active &&
                        (magenta
                          ? 'bg-gradient-to-r from-destructive/10 to-transparent shadow-[inset_2px_0_0_var(--destructive)] data-[active=true]:text-destructive'
                          : 'bg-gradient-to-r from-primary/10 to-transparent shadow-[inset_2px_0_0_var(--primary)] data-[active=true]:text-primary'),
                    )}
                  >
                    <Link href={item.href}>
                      <span
                        aria-hidden
                        className={cn('font-[family-name:var(--font-mono)] text-[10px]', numberTone)}
                      >
                        {item.number}
                      </span>
                      <span className="group-data-[collapsible=icon]:hidden">{item.label}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              );
            })}
          </SidebarMenu>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter className="gap-2 px-4 pb-6">
        <CaptureReadiness />
      </SidebarFooter>
    </Sidebar>
  );
}
