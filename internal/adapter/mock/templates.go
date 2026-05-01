// templates.go (gm-root.28.5) — port of
// testing/acceptance/temperature-spa/shared/runner/templates.ts.
//
// Eight handlers and a per-file content registry hand-tuned to the
// D17 (gm-xw8a) JSONL pack and the D18 (gm-bvlm) numeric oracle.
//
// Each handler receives a TemplateContext (projectDir, beadID, labels,
// log) and a Frontmatter (template, testid, files, extras). Handlers
// perform the mechanical work the matching bead frontmatter declares
// — writing files, shelling out to npm/vite, etc.

package mock

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// TemplateContext is threaded through every handler.
type TemplateContext struct {
	ProjectDir string
	BeadID     string
	Labels     []string
	Log        func(string)
}

func (c TemplateContext) log(msg string) {
	if c.Log != nil {
		c.Log(msg)
	}
}

// TemplateHandler is the signature every template implements.
type TemplateHandler func(ctx TemplateContext, fm Frontmatter) error

// GetTemplate returns a handler by name. Returns nil if unknown.
// Mirrors the TS getTemplate.
func GetTemplate(name string) TemplateHandler {
	switch name {
	case "init-repo":
		return initRepo
	case "npm-install":
		return npmInstall
	case "write-component", "write-test":
		return writeComponent
	case "build":
		return buildTpl
	case "serve":
		return serveTpl
	case "error-then-recover":
		return errorThenRecover
	case "noop":
		return noop
	default:
		return nil
	}
}

// ─── Handlers ──────────────────────────────────────────────────────

func initRepo(ctx TemplateContext, fm Frontmatter) error {
	ctx.log(fmt.Sprintf("init-repo: %s", ctx.BeadID))
	if err := runCmd(ctx.ProjectDir, "git", "init", "--initial-branch=main"); err != nil {
		return err
	}
	return writeRegistryFiles(ctx, fm)
}

func npmInstall(ctx TemplateContext, _ Frontmatter) error {
	ctx.log(fmt.Sprintf("npm-install: %s", ctx.BeadID))
	if err := runCmd(ctx.ProjectDir, "npm", "install", "--offline", "--silent"); err != nil {
		ctx.log("npm-install: offline cache miss, falling back to online")
		if err2 := runCmd(ctx.ProjectDir, "npm", "install", "--silent"); err2 != nil {
			return err2
		}
	}
	return nil
}

func writeComponent(ctx TemplateContext, fm Frontmatter) error {
	ctx.log(fmt.Sprintf("write-component: %s files=%v", ctx.BeadID, fm.Files))
	return writeRegistryFiles(ctx, fm)
}

func buildTpl(ctx TemplateContext, _ Frontmatter) error {
	ctx.log(fmt.Sprintf("build: %s", ctx.BeadID))
	return runCmd(ctx.ProjectDir, "npm", "run", "build")
}

func serveTpl(ctx TemplateContext, _ Frontmatter) error {
	// Preview-server lifecycle is owned by the M2/M3 step beads; the
	// template's job here is just to be a no-op marker.
	ctx.log(fmt.Sprintf("serve: %s (no-op; preview owned by step bead)", ctx.BeadID))
	return nil
}

// errorThenRecoverState — per-bead so concurrent sessions don't race
// over a package-scoped counter. Each bead's first attempt throws;
// subsequent attempts succeed.
var (
	errorThenRecoverMu  sync.Mutex
	errorThenRecoverHit = map[string]bool{}
)

func errorThenRecover(ctx TemplateContext, _ Frontmatter) error {
	errorThenRecoverMu.Lock()
	first := !errorThenRecoverHit[ctx.BeadID]
	errorThenRecoverHit[ctx.BeadID] = true
	errorThenRecoverMu.Unlock()
	ctx.log(fmt.Sprintf("error-then-recover: bead=%s first=%v", ctx.BeadID, first))
	if first {
		return fmt.Errorf("error-then-recover: deliberate first-attempt failure for %s (gm-root.28.5)", ctx.BeadID)
	}
	return nil
}

func noop(ctx TemplateContext, fm Frontmatter) error {
	ctx.log(fmt.Sprintf("noop: %s files=%v", ctx.BeadID, fm.Files))
	return writeRegistryFiles(ctx, fm)
}

// ─── Helpers ──────────────────────────────────────────────────────

func writeRegistryFiles(ctx TemplateContext, fm Frontmatter) error {
	for _, rel := range fm.Files {
		if len(rel) > 0 && rel[len(rel)-1] == '/' {
			// Directory marker (e.g. 'node_modules/'); skip — real
			// commands like npm-install create these.
			continue
		}
		body, ok := resolveContent(rel, ctx.Labels)
		if !ok {
			return fmt.Errorf("mock: no registry entry for file %q (bead %s)", rel, ctx.BeadID)
		}
		if err := writeProjectFile(ctx.ProjectDir, rel, body); err != nil {
			return err
		}
		if rel == "src/App.tsx" && hasLabel(ctx.Labels, "milestone:m3") {
			if err := writeProjectFile(ctx.ProjectDir, "src/App.test.tsx", fileContentAppTestTSXM3); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeProjectFile(projectDir, rel, body string) error {
	abs := filepath.Join(projectDir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(body), 0o644)
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func resolveContent(rel string, labels []string) (string, bool) {
	// App.tsx has three versions disambiguated by milestone label.
	if rel == "src/App.tsx" {
		for _, l := range labels {
			if l == "milestone:m3" {
				v, ok := contentRegistry["src/App.tsx#m3"]
				return v, ok
			}
			if l == "milestone:m2" {
				v, ok := contentRegistry["src/App.tsx#m2"]
				return v, ok
			}
		}
		v, ok := contentRegistry["src/App.tsx#m1"]
		return v, ok
	}
	v, ok := contentRegistry[rel]
	return v, ok
}

func runCmd(cwd string, cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Dir = cwd
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mock: %s %v failed in %s: %w\n%s", cmd, args, cwd, err, string(out))
	}
	return nil
}

// ─── Content registry ─────────────────────────────────────────────
//
// Hand-tuned to the D17 contract (testids + frontmatter) and the D18
// oracle (numeric correctness, formatted-string °F). If the M3
// oracle fails on a row mismatch, look here first — this is the
// source of truth the mock writes.

var contentRegistry = map[string]string{
	"package.json":                  fileContentPackageJSON,
	"vite.config.ts":                fileContentViteConfig,
	"tsconfig.json":                 fileContentTSConfig,
	"index.html":                    fileContentIndexHTML,
	"src/main.tsx":                  fileContentMainTSX,
	"src/App.tsx#m1":                fileContentAppTSXm1,
	"src/App.tsx#m2":                fileContentAppTSXm2,
	"src/App.tsx#m3":                fileContentAppTSXm3,
	"src/App.test.tsx":              fileContentAppTestTSX,
	"src/temperatureRows.ts":        fileContentTemperatureRows,
	"src/TemperatureTable.tsx":      fileContentTemperatureTable,
	"src/TemperatureTable.test.tsx": fileContentTemperatureTableTest,
}

const fileContentPackageJSON = `{
  "name": "temperature-spa",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc --noEmit && vite build",
    "preview": "vite preview",
    "test": "vitest run"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@testing-library/react": "^16.1.0",
    "@types/react": "^18.3.12",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.4",
    "@asamuzakjp/css-color": "^3.2.0",
    "jsdom": "^25.0.1",
    "typescript": "^5.6.3",
    "vite": "^5.4.11",
    "vitest": "^2.1.8"
  }
}
`

const fileContentViteConfig = `import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
  },
});
`

const fileContentTSConfig = `{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "jsx": "react-jsx",
    "strict": true,
    "esModuleInterop": true,
    "resolveJsonModule": true,
    "skipLibCheck": true,
    "isolatedModules": true,
    "noEmit": true
  },
  "include": ["src"]
}
`

const fileContentIndexHTML = `<!DOCTYPE html>
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
`

const fileContentMainTSX = `import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';

const root = document.getElementById('root');
if (!root) throw new Error('root element not found');
ReactDOM.createRoot(root).render(<React.StrictMode><App /></React.StrictMode>);
`

const fileContentAppTSXm1 = `export default function App() {
  return <div data-testid="app-root">Scaffold ready</div>;
}
`

const fileContentAppTSXm2 = `export default function App() {
  return <div data-testid="app-root">Hello world</div>;
}
`

const fileContentAppTSXm3 = `import TemperatureTable from './TemperatureTable';

export default function App() {
  return (
    <div data-testid="app-root">
      <TemperatureTable />
    </div>
  );
}
`

const fileContentAppTestTSX = `import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import App from './App';

describe('App', () => {
  it('renders Hello world inside app-root', () => {
    render(<App />);
    const root = screen.getByTestId('app-root');
    expect(root.textContent).toBe('Hello world');
  });
});
`

const fileContentAppTestTSXM3 = `import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import App from './App';

describe('App', () => {
  it('keeps app-root and renders the temperature table', () => {
    render(<App />);
    expect(screen.getByTestId('app-root')).toBeTruthy();
    expect(screen.getByTestId('temperature-table')).toBeTruthy();
  });
});
`

const fileContentTemperatureRows = `// 16 rows for °C ∈ {0, 20, 40, ..., 300}. °F = °C × 9/5 + 32, one decimal.
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
`

const fileContentTemperatureTable = "import { temperatureRows } from './temperatureRows';\n\nexport default function TemperatureTable() {\n  const rows = temperatureRows();\n  return (\n    <table data-testid=\"temperature-table\">\n      <thead>\n        <tr>\n          <th>Celsius</th>\n          <th>Fahrenheit</th>\n        </tr>\n      </thead>\n      <tbody>\n        {rows.map((r) => (\n          <tr key={r.celsius} data-testid={`row-${r.celsius}`}>\n            <td>{r.celsius}</td>\n            <td>{r.fahrenheit}</td>\n          </tr>\n        ))}\n      </tbody>\n    </table>\n  );\n}\n"

const fileContentTemperatureTableTest = `import { describe, it, expect } from 'vitest';
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
`
