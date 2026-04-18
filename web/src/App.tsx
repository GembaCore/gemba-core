import { useQuery } from '@tanstack/react-query';
import { Routes, Route, Link, useLocation } from 'react-router-dom';

// Placeholder shell. Real app chrome (sidebar, topbar, cmd palette, route
// tree) ships with gm-e4.1. This exists only to prove that:
//  1. React renders
//  2. Tailwind is wired
//  3. react-query can reach /api/health through the Vite proxy (dev) or
//     the embedded handler (prod)

async function fetchJSON<T>(path: string): Promise<T> {
  const r = await fetch(path);
  if (!r.ok) throw new Error(`${path}: ${r.status}`);
  return r.json();
}

function Nav() {
  const loc = useLocation();
  const link = (to: string, label: string) => (
    <Link
      to={to}
      className={
        'px-3 py-1.5 rounded-md text-sm ' +
        (loc.pathname === to
          ? 'bg-neutral-800 text-white'
          : 'text-neutral-400 hover:text-white hover:bg-neutral-900')
      }
    >
      {label}
    </Link>
  );
  return (
    <nav className="flex gap-1 items-center px-4 h-12 border-b border-neutral-800">
      <span className="font-semibold mr-4">Gemba</span>
      {link('/', 'Home')}
      {link('/health', 'Health')}
      <span className="ml-auto text-xs text-neutral-500">scaffold</span>
    </nav>
  );
}

function Home() {
  return (
    <div className="p-8 max-w-2xl">
      <h1 className="text-2xl font-semibold mb-4">Gemba scaffold</h1>
      <p className="text-neutral-400 mb-6">
        This is the gm-e1.4 placeholder. Real app shell lands with gm-e4.1.
      </p>
      <p className="text-neutral-400">
        The build toolchain is live: React 18, TypeScript, Tailwind, TanStack Query, React Router.
        The frontend talks to the Go backend through{' '}
        <code className="text-neutral-200">/api/*</code>.
      </p>
    </div>
  );
}

function Health() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['health'],
    queryFn: () => fetchJSON<{ status: string }>('/api/health'),
  });
  return (
    <div className="p-8 max-w-2xl">
      <h2 className="text-lg font-semibold mb-4">API health</h2>
      {isLoading && <div className="text-neutral-400">loading…</div>}
      {error && (
        <div className="text-red-400">
          {(error as Error).message}
          <p className="mt-2 text-sm text-neutral-500">
            Is <code>gemba serve</code> running? In dev, Vite proxies to :7666 by default.
          </p>
        </div>
      )}
      {data && (
        <pre className="bg-neutral-900 p-4 rounded-md text-neutral-200">
          {JSON.stringify(data, null, 2)}
        </pre>
      )}
    </div>
  );
}

export default function App() {
  return (
    <div className="min-h-screen bg-neutral-950 text-neutral-200">
      <Nav />
      <Routes>
        <Route path="/" element={<Home />} />
        <Route path="/health" element={<Health />} />
        <Route path="*" element={<Home />} />
      </Routes>
    </div>
  );
}
