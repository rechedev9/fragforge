export default function BootstrapPage(): React.ReactNode {
  return (
    <main className="mx-auto flex min-h-screen max-w-lg items-center px-6 py-12">
      <form action="/api/session/bootstrap" method="post" className="studio-panel w-full space-y-5 p-6">
        <div className="space-y-2">
          <p className="font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.16em] text-primary">
            FragForge local session
          </p>
          <h1 className="text-xl font-semibold">Autoriza este navegador</h1>
          <p className="text-sm text-muted-foreground">
            Introduce la capacidad mostrada por Local Studio. Se guarda solo como una cookie HttpOnly de esta sesión.
          </p>
        </div>
        <label className="block space-y-2" htmlFor="capability">
          <span className="text-sm font-medium">Capacidad local</span>
          <input
            id="capability"
            name="capability"
            type="password"
            autoComplete="off"
            required
            className="h-10 w-full rounded-md border border-input bg-background px-3 font-mono text-sm"
          />
        </label>
        <button type="submit" className="inline-flex h-10 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground">
          Abrir FragForge
        </button>
      </form>
    </main>
  );
}
