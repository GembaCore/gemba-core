// testing/acceptance/temperature-spa/shared/runner/templates.ts
//
// gm-root.27.2 — bead-template handler registry.
//
// Each handler is a `(ctx, bead) => Promise<void>` that performs the
// mechanical work the matching bead frontmatter declares. The mock
// runner picks the handler by name (frontmatter `template:` field)
// and invokes it.
//
// The handlers are deliberately specific to the temperature-spa
// target. There's no generic "write-component" — write-component
// dispatches to a per-file content registry keyed by the bead's
// `files:` frontmatter. The registry lives below in CONTENT_REGISTRY.
//
// References:
//   - D16 docs/design/acceptance-temperature-spa.md §4 (template list)
//   - D17 docs/design/acceptance-temperature-spa.md §5 (per-bead frontmatter)

import { spawnSync, type SpawnSyncReturns } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import type { Frontmatter } from './frontmatter';

export type TemplateContext = {
  /** Absolute path to the target project's root. */
  projectDir: string;
  /** The bead being worked. */
  beadID: string;
  /** The bead's labels — used to disambiguate per-milestone variants. */
  labels: string[];
  /**
   * Logger callback so the mock runner can stream template progress
   * into the test report. Optional; defaults to no-op.
   */
  log?: (msg: string) => void;
};

export type TemplateHandler = (
  ctx: TemplateContext,
  fm: Frontmatter
) => Promise<void>;

/**
 * Look up a handler by name. Returns undefined if no template
 * matches; the caller (mock runner) escalates with template_unknown.
 */
export function getTemplate(name: string | undefined): TemplateHandler | undefined {
  if (!name) return undefined;
  return TEMPLATES[name];
}

const TEMPLATES: Record<string, TemplateHandler> = {
  'init-repo': initRepo,
  'npm-install': npmInstall,
  'write-component': writeComponent,
  'write-test': writeComponent, // same path: write file from registry
  build: build,
  serve: serveTemplate,
  'error-then-recover': errorThenRecover,
  noop: noop,
};

// ─────────────────────────────────────────────────────────────────────
// Templates
// ─────────────────────────────────────────────────────────────────────

async function initRepo(ctx: TemplateContext, fm: Frontmatter): Promise<void> {
  ctx.log?.(`init-repo: ${ctx.beadID}`);
  // git init.
  runSync('git', ['init', '--initial-branch=main'], ctx.projectDir);
  // Write each declared file from the content registry.
  await writeRegistryFiles(ctx, fm);
}

async function npmInstall(ctx: TemplateContext): Promise<void> {
  ctx.log?.(`npm-install: ${ctx.beadID}`);
  // Prefer offline cache (D16 §4.3). If --offline fails (cache miss),
  // fall through to a regular install — keeps the test resilient when
  // the cache hasn't been pre-warmed yet.
  const offline = spawnSync('npm', ['install', '--offline', '--silent'], {
    cwd: ctx.projectDir,
    stdio: 'pipe',
  });
  if (offline.status !== 0) {
    ctx.log?.('npm-install: offline cache miss, falling back to online');
    runSync('npm', ['install', '--silent'], ctx.projectDir);
  }
}

async function writeComponent(ctx: TemplateContext, fm: Frontmatter): Promise<void> {
  ctx.log?.(`write-component: ${ctx.beadID} files=${(fm.files ?? []).join(',')}`);
  await writeRegistryFiles(ctx, fm);
}

async function build(ctx: TemplateContext): Promise<void> {
  ctx.log?.(`build: ${ctx.beadID}`);
  runSync('npm', ['run', 'build'], ctx.projectDir);
}

async function serveTemplate(ctx: TemplateContext): Promise<void> {
  // serve-template is a marker — actual preview-server lifecycle is
  // owned by the M2/M3 step beads (gm-root.27.9 / .10), which spawn
  // and tear down vite preview around their oracle assertions. The
  // template's job is just to verify dist/ is buildable.
  ctx.log?.(`serve: ${ctx.beadID} (no-op; preview owned by step bead)`);
}

let errorThenRecoverCount = 0;
async function errorThenRecover(ctx: TemplateContext): Promise<void> {
  ctx.log?.(`error-then-recover: attempt ${errorThenRecoverCount + 1}`);
  errorThenRecoverCount += 1;
  if (errorThenRecoverCount === 1) {
    throw new Error(
      'error-then-recover: deliberate first-attempt failure (gm-root.27.2)'
    );
  }
}

async function noop(ctx: TemplateContext, fm: Frontmatter): Promise<void> {
  // Drop in any files declared in the frontmatter. Useful for "just
  // copy these config files" beads (e.g., M1.2 which writes
  // tsconfig.json + index.html + src/main.tsx).
  ctx.log?.(`noop: ${ctx.beadID} files=${(fm.files ?? []).join(',')}`);
  await writeRegistryFiles(ctx, fm);
}

// ─────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────

async function writeRegistryFiles(
  ctx: TemplateContext,
  fm: Frontmatter
): Promise<void> {
  const files = fm.files ?? [];
  if (files.length === 0) return;
  for (const relPath of files) {
    if (relPath.endsWith('/')) {
      // Directory marker (e.g., 'node_modules/'); not a file we
      // synthesize — skip silently. Templates like npm-install
      // produce these via real commands.
      continue;
    }
    const content = resolveContent(relPath, ctx.labels, ctx.beadID);
    if (content === undefined) {
      throw new Error(
        `MockAgentRunner: no registry entry for file '${relPath}' (bead ${ctx.beadID})`
      );
    }
    const abs = join(ctx.projectDir, relPath);
    mkdirSync(dirname(abs), { recursive: true });
    writeFileSync(abs, content, 'utf8');
  }
}

function resolveContent(
  relPath: string,
  labels: string[],
  beadID: string
): string | undefined {
  // App.tsx has two versions: the M2 "Hello world" and the M3
  // "render TemperatureTable". Disambiguate by milestone label.
  if (relPath === 'src/App.tsx') {
    if (labels.includes('milestone:m3')) return CONTENT_REGISTRY['src/App.tsx#m3'];
    return CONTENT_REGISTRY['src/App.tsx#m2'];
  }
  return CONTENT_REGISTRY[relPath];
}

/**
 * Static content for every file the target SPA contains. Authoring
 * is hand-tuned to the D17 contract (testids + frontmatter) and the
 * D18 oracle (numeric correctness, formatted-string °F).
 *
 * If the M3 oracle fails on a row mismatch, look here first — this
 * is the source of truth the mock writes.
 */
const CONTENT_REGISTRY: Record<string, string> = {
  'package.json': JSON.stringify(
    {
      name: 'temperature-spa',
      private: true,
      version: '0.0.0',
      type: 'module',
      scripts: {
        dev: 'vite',
        build: 'tsc --noEmit && vite build',
        preview: 'vite preview',
        test: 'vitest run',
      },
      dependencies: {
        react: '^18.3.1',
        'react-dom': '^18.3.1',
      },
      devDependencies: {
        '@testing-library/react': '^16.1.0',
        '@types/react': '^18.3.12',
        '@types/react-dom': '^18.3.1',
        '@vitejs/plugin-react': '^4.3.4',
        jsdom: '^25.0.1',
        typescript: '^5.6.3',
        vite: '^5.4.11',
        vitest: '^2.1.8',
      },
    },
    null,
    2
  ) + '\n',
  'vite.config.ts':
    `import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
  },
});
`,
  'tsconfig.json': JSON.stringify(
    {
      compilerOptions: {
        target: 'ES2022',
        lib: ['ES2022', 'DOM', 'DOM.Iterable'],
        module: 'ESNext',
        moduleResolution: 'Bundler',
        jsx: 'react-jsx',
        strict: true,
        esModuleInterop: true,
        resolveJsonModule: true,
        skipLibCheck: true,
        allowImportingTsExtensions: false,
        isolatedModules: true,
        noEmit: true,
      },
      include: ['src'],
    },
    null,
    2
  ) + '\n',
  'index.html':
    `<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Temperature SPA</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
`,
  'src/main.tsx':
    `import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';

const root = document.getElementById('root');
if (!root) throw new Error('root element not found');
ReactDOM.createRoot(root).render(<React.StrictMode><App /></React.StrictMode>);
`,
  'src/App.tsx#m2':
    `export default function App() {
  return <div data-testid="app-root">Hello world</div>;
}
`,
  'src/App.tsx#m3':
    `import TemperatureTable from './TemperatureTable';

export default function App() {
  return (
    <div data-testid="app-root">
      <TemperatureTable />
    </div>
  );
}
`,
  'src/App.test.tsx':
    `import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import App from './App';

describe('App', () => {
  it('renders Hello world inside app-root', () => {
    render(<App />);
    const root = screen.getByTestId('app-root');
    expect(root.textContent).toBe('Hello world');
  });
});
`,
  'src/temperatureRows.ts':
    `// 16 rows for °C ∈ {0, 20, 40, ..., 300}. °F = °C × 9/5 + 32, one decimal.
export interface TemperatureRow {
  celsius: number;
  fahrenheit: string;
}

export function temperatureRows(): TemperatureRow[] {
  const rows: TemperatureRow[] = [];
  for (let c = 0; c <= 300; c += 20) {
    rows.push({
      celsius: c,
      fahrenheit: ((c * 9) / 5 + 32).toFixed(1),
    });
  }
  return rows;
}
`,
  'src/TemperatureTable.tsx':
    `import { temperatureRows } from './temperatureRows';

export default function TemperatureTable() {
  const rows = temperatureRows();
  return (
    <table data-testid="temperature-table">
      <thead>
        <tr>
          <th>Celsius</th>
          <th>Fahrenheit</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr key={r.celsius} data-testid={\`row-\${r.celsius}\`}>
            <td>{r.celsius}</td>
            <td>{r.fahrenheit}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
`,
  'src/TemperatureTable.test.tsx':
    `import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import TemperatureTable from './TemperatureTable';

describe('TemperatureTable', () => {
  it('renders exactly 16 rows', () => {
    render(<TemperatureTable />);
    const rows = screen.getAllByTestId(/^row-/);
    expect(rows).toHaveLength(16);
  });

  it('row-0 shows 32.0', () => {
    render(<TemperatureTable />);
    const row = screen.getByTestId('row-0');
    expect(row.textContent).toContain('0');
    expect(row.textContent).toContain('32.0');
  });

  it('row-100 shows 212.0', () => {
    render(<TemperatureTable />);
    const row = screen.getByTestId('row-100');
    expect(row.textContent).toContain('100');
    expect(row.textContent).toContain('212.0');
  });

  it('row-300 shows 572.0', () => {
    render(<TemperatureTable />);
    const row = screen.getByTestId('row-300');
    expect(row.textContent).toContain('300');
    expect(row.textContent).toContain('572.0');
  });
});
`,
};

function runSync(cmd: string, args: string[], cwd: string): void {
  const res: SpawnSyncReturns<Buffer> = spawnSync(cmd, args, {
    cwd,
    stdio: 'pipe',
  });
  if (res.status !== 0) {
    const err = res.stderr?.toString() ?? '';
    throw new Error(
      `MockAgentRunner: \`${cmd} ${args.join(' ')}\` exited ${res.status} in ${cwd}\n${err}`
    );
  }
}
