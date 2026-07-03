'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { LogOut, LogIn, ChevronsUpDown, Plus } from 'lucide-react';
import { toast } from 'sonner';
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { Button } from '@/components/ui/button';
import { Progress } from '@/components/ui/progress';
import { Wordmark } from '@/components/brand/wordmark';
import { CaptureReadiness } from '@/components/shell/capture-readiness';
import { useSession } from '@/lib/session';
import { isLocalMode } from '@/lib/mode';
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
 * CAPTURA readiness card plus, in cloud mode, the slots meter and user menu.
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
        {/* Slots quota and Steam sign-in are cloud-only; local studio runs on
            this PC, so the footer shows just capture readiness. */}
        {!isLocalMode() && <SlotsMeter />}
        {!isLocalMode() && <UserMenu />}
      </SidebarFooter>
    </Sidebar>
  );
}

/** Footer slots meter: used / total with a thin cyan progress and "Conseguir más". */
function SlotsMeter() {
  const { session } = useSession();
  const slots = session?.slots;
  if (!slots) return null;

  const pct = slots.total > 0 ? Math.min(100, (slots.used / slots.total) * 100) : 0;

  return (
    <div className="border border-sidebar-border bg-sidebar-accent/40 p-2.5 group-data-[collapsible=icon]:hidden">
      <div className="flex items-center justify-between">
        <span className="font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.18em] text-sidebar-foreground/60">
          Slots
        </span>
        <span className="font-[family-name:var(--font-mono)] text-xs tabular-nums text-sidebar-foreground">
          {slots.used}/{slots.total}
        </span>
      </div>
      <Progress value={pct} className="mt-2 h-1" />
      <Button
        variant="ghost"
        size="sm"
        onClick={() => toast('Más slots de render, muy pronto.', { description: 'De momento cada cuenta renderiza un reel a la vez.' })}
        className="mt-2 h-7 w-full justify-start gap-1.5 px-1.5 text-xs uppercase tracking-[0.08em] text-sidebar-foreground/80 hover:text-sidebar-foreground"
      >
        <Plus className="size-3.5" />
        Conseguir más
      </Button>
    </div>
  );
}

/** Footer user: avatar + persona with a dropdown to sign out. */
function UserMenu() {
  const { session, signOut } = useSession();
  const router = useRouter();
  const user = session?.user;

  // Signed out (e.g. after a reload before the session restores, or a real
  // logged-out visit): offer a way back to sign in rather than an empty footer.
  if (!user) {
    return (
      <Button
        asChild
        variant="outline"
        size="sm"
        className="w-full justify-start gap-2 uppercase tracking-[0.08em] group-data-[collapsible=icon]:justify-center"
      >
        <Link href="/">
          <LogIn className="size-4" />
          <span className="group-data-[collapsible=icon]:hidden">Iniciar sesión</span>
        </Link>
      </Button>
    );
  }

  const initials = user.personaName.slice(0, 2).toUpperCase();

  const handleSignOut = async () => {
    await signOut();
    router.push('/');
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className="h-auto w-full justify-start gap-2 px-1.5 py-1.5 text-left group-data-[collapsible=icon]:justify-center"
        >
          <Avatar className="size-7 rounded-none">
            <AvatarImage src={user.avatarUrl} alt={user.personaName} />
            <AvatarFallback className="rounded-none text-xs">{initials}</AvatarFallback>
          </Avatar>
          <span className="flex-1 truncate text-sm font-medium group-data-[collapsible=icon]:hidden">
            {user.personaName}
          </span>
          <ChevronsUpDown className="size-4 text-muted-foreground group-data-[collapsible=icon]:hidden" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start" className="w-(--radix-dropdown-menu-trigger-width) min-w-48">
        <DropdownMenuLabel className="truncate font-normal text-muted-foreground">
          {user.personaName}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={handleSignOut}>
          <LogOut />
          Cerrar sesión
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
