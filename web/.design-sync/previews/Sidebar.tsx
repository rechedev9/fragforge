import {
  SidebarProvider,
  Sidebar,
  SidebarHeader,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarFooter,
  Wordmark,
} from 'cs2video-web';
import { Target, UploadCloud, Film, Flame } from 'lucide-react';

const nav = [
  { label: 'Matches', icon: Target, active: true },
  { label: 'Upload', icon: UploadCloud, active: false },
  { label: 'Library', icon: Film, active: false },
  { label: 'Feed', icon: Flame, active: false },
];

export function Studio() {
  return (
    <div className="dark" style={{ fontFamily: 'var(--font-sans)' }}>
      <SidebarProvider>
        <Sidebar collapsible="none" style={{ height: 520 }}>
          <SidebarHeader>
            <div style={{ padding: '8px 8px 4px' }}>
              <Wordmark />
            </div>
          </SidebarHeader>
          <SidebarContent>
            <SidebarGroup>
              <SidebarGroupContent>
                <SidebarMenu>
                  {nav.map((item) => (
                    <SidebarMenuItem key={item.label}>
                      <SidebarMenuButton isActive={item.active}>
                        <item.icon />
                        <span>{item.label}</span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          </SidebarContent>
          <SidebarFooter>
            <div
              style={{
                padding: 10,
                fontSize: 12,
                color: 'var(--sidebar-foreground)',
                fontFamily: 'var(--font-mono)',
              }}
            >
              3 / 5 slots used
            </div>
          </SidebarFooter>
        </Sidebar>
      </SidebarProvider>
    </div>
  );
}
