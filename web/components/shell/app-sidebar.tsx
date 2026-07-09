'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import {
  Aperture,
  ChevronsUpDown,
  Clapperboard,
  Compass,
  Crosshair,
  Film,
  LogIn,
  LogOut,
  Plus,
  UploadCloud,
  type LucideIcon,
} from 'lucide-react';
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
  icon: LucideIcon;
  /** Stream remains a category signal, never the global selection colour. */
  stream?: boolean;
};

const NAV_ITEMS: NavItem[] = [
  { number: '01', label: 'Partidas', href: '/matches', icon: Crosshair },
  { number: '02', label: 'Subir demo', href: '/upload', icon: UploadCloud },
  { number: '03', label: 'Clips de stream', href: '/streams', icon: Clapperboard, stream: true },
  { number: '04', label: 'Biblioteca', href: '/videos', icon: Film },
  { number: '05', label: 'Feed', href: '/feed', icon: Compass },
];

/** A nav href is active for its exact page and any nested route under it. */
function isActiveHref(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(`${href}/`);
}

/**
 * The persistent left sidebar shell, NEON HUD style: wordmark + katakana up
 * top, the numbered 01-05 nav (active item gets the inset accent bar; the
 * active destination always uses cyan), and a footer with the
 * CAPTURA readiness card plus, in cloud mode, the slots meter and user menu.
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
          <Aperture className="hidden size-5 text-primary group-data-[collapsible=icon]:block" aria-hidden />
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
                  className={cn(index === 3 && 'mt-4 border-t border-sidebar-border pt-4')}
                >
                  <SidebarMenuButton
                    asChild
                    isActive={active}
                    tooltip={item.label}
                    className={cn(
                      'h-12 gap-3 rounded-none px-6 font-[family-name:var(--font-display)] text-[13px] font-semibold uppercase tracking-[0.06em] text-sidebar-foreground transition-colors hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground group-data-[collapsible=icon]:size-10! group-data-[collapsible=icon]:p-2.5!',
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
    <div className="border border-sidebar-border bg-sidebar-accent/40 p-3 group-data-[collapsible=icon]:hidden">
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
        className="mt-2 w-full justify-start gap-1.5 px-2 text-xs uppercase tracking-[0.08em] text-sidebar-foreground/80 hover:text-sidebar-foreground"
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
          <Avatar className="size-8 rounded-none">
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
