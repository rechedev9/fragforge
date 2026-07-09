import { redirect } from 'next/navigation';

/**
 * The desktop build has no marketing/login landing: FragForge only ever runs
 * against its own bundled orchestrator in local mode, so the root route is
 * just the dashboard entry.
 */
export default function RootPage(): never {
  redirect('/matches');
}
