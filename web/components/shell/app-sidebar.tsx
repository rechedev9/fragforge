'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { Target, UploadCloud, Film, Compass, LogOut, LogIn, ChevronsUpDown, Plus } from 'lucide-react';
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
  label: string;
  href: string;
  icon: typeof Target;
};

const NAV_ITEMS: NavItem[] = [
  { label: 'Matches', href: '/matches', icon: Target },
  { label: 'Upload', href: '/upload', icon: UploadCloud },
  { label: 'Library', href: '/videos', icon: Film },
  { label: 'Feed', href: '/feed', icon: Compass },
];

/** A nav href is active for its exact page and any nested route under it. */
function isActiveHref(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(`${href}/`);
}

/**
 * The persistent left sidebar shell: brand wordmark, primary nav with active
 * state, and a footer with the slots meter and the user dropdown (sign out).
 */
export function AppSidebar() {
  const pathname = usePathname();

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <Link
          href="/matches"
          className="flex items-center gap-2 px-1 py-1 group-data-[collapsible=icon]:justify-center"
        >
          <Wordmark className="group-data-[collapsible=icon]:hidden" />
          <Wordmark hideMark className="hidden" />
        </Link>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarMenu>
            {NAV_ITEMS.map((item) => {
              const active = isActiveHref(pathname, item.href);
              return (
                <SidebarMenuItem key={item.href}>
                  <SidebarMenuButton
                    asChild
                    isActive={active}
                    tooltip={item.label}
                    className={cn(
                      'relative',
                      active &&
                        'before:absolute before:left-0 before:top-1/2 before:h-5 before:w-0.5 before:-translate-y-1/2 before:rounded-full before:bg-sidebar-primary',
                      active && 'text-sidebar-primary',
                    )}
                  >
                    <Link href={item.href}>
                      <item.icon />
                      <span>{item.label}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              );
            })}
          </SidebarMenu>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <CaptureReadiness />
        {/* Slots quota and Steam sign-in are cloud-only; local studio runs on
            this PC, so the footer shows just capture readiness. */}
        {!isLocalMode() && <SlotsMeter />}
        {!isLocalMode() && <UserMenu />}
      </SidebarFooter>
    </Sidebar>
  );
}

/** Footer slots meter: used / total with a thin lime progress and "Get more". */
function SlotsMeter() {
  const { session } = useSession();
  const slots = session?.slots;
  if (!slots) return null;

  const pct = slots.total > 0 ? Math.min(100, (slots.used / slots.total) * 100) : 0;

  return (
    <div className="rounded-lg border border-sidebar-border bg-sidebar-accent/40 p-2.5 group-data-[collapsible=icon]:hidden">
      <div className="flex items-center justify-between">
        <span className="text-[0.65rem] font-medium uppercase tracking-wide text-sidebar-foreground/70">
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
        onClick={() => toast('More render slots are coming soon.', { description: 'For now each account renders one reel at a time.' })}
        className="mt-2 h-7 w-full justify-start gap-1.5 px-1.5 text-xs text-sidebar-foreground/80 hover:text-sidebar-foreground"
      >
        <Plus className="size-3.5" />
        Get more
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
        className="w-full justify-start gap-2 group-data-[collapsible=icon]:justify-center"
      >
        <Link href="/">
          <LogIn className="size-4" />
          <span className="group-data-[collapsible=icon]:hidden">Sign in</span>
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
          <Avatar className="size-7 rounded-md">
            <AvatarImage src={user.avatarUrl} alt={user.personaName} />
            <AvatarFallback className="rounded-md text-xs">{initials}</AvatarFallback>
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
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
