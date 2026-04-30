package mock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "mock-tpl-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func TestGetTemplate_AllNames(t *testing.T) {
	for _, name := range []string{
		"init-repo", "npm-install", "write-component", "write-test",
		"build", "serve", "error-then-recover", "noop",
	} {
		if GetTemplate(name) == nil {
			t.Errorf("expected template %q", name)
		}
	}
}

func TestGetTemplate_Unknown(t *testing.T) {
	if GetTemplate("not-a-template") != nil {
		t.Error("expected nil for unknown template")
	}
}

func TestNoop_WritesFiles(t *testing.T) {
	dir := tempDir(t)
	h := GetTemplate("noop")
	err := h(
		TemplateContext{ProjectDir: dir, BeadID: "tspa-2", Labels: []string{"milestone:m1"}},
		Frontmatter{Files: []string{"tsconfig.json", "index.html", "src/main.tsx"}, Extras: map[string]string{}},
	)
	if err != nil {
		t.Fatalf("noop: %v", err)
	}
	for _, rel := range []string{"tsconfig.json", "index.html", "src/main.tsx"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected %q present: %v", rel, err)
		}
	}
}

func TestNoop_UnknownFileFails(t *testing.T) {
	dir := tempDir(t)
	h := GetTemplate("noop")
	err := h(
		TemplateContext{ProjectDir: dir, BeadID: "tspa-x", Labels: []string{}},
		Frontmatter{Files: []string{"unknown/path.ts"}, Extras: map[string]string{}},
	)
	if err == nil || !strings.Contains(err.Error(), "no registry entry") {
		t.Fatalf("expected no-registry-entry error; got %v", err)
	}
}

func TestWriteComponent_M2AppTSX_HelloWorld(t *testing.T) {
	dir := tempDir(t)
	h := GetTemplate("write-component")
	err := h(
		TemplateContext{ProjectDir: dir, BeadID: "tspa-4", Labels: []string{"milestone:m2"}},
		Frontmatter{Files: []string{"src/App.tsx"}, Extras: map[string]string{}},
	)
	if err != nil {
		t.Fatalf("write-component: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "src/App.tsx"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, `data-testid="app-root"`) {
		t.Error("missing app-root testid")
	}
	if !strings.Contains(s, "Hello world") {
		t.Error("missing Hello world")
	}
	if strings.Contains(s, "TemperatureTable") {
		t.Error("M2 App.tsx should not reference TemperatureTable")
	}
}

func TestWriteComponent_M3AppTSX_TemperatureTableWrapper(t *testing.T) {
	dir := tempDir(t)
	h := GetTemplate("write-component")
	err := h(
		TemplateContext{ProjectDir: dir, BeadID: "tspa-11", Labels: []string{"milestone:m3"}},
		Frontmatter{Files: []string{"src/App.tsx"}, Extras: map[string]string{}},
	)
	if err != nil {
		t.Fatalf("write-component: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "src/App.tsx"))
	s := string(body)
	if !strings.Contains(s, "TemperatureTable") {
		t.Error("M3 App.tsx must wrap TemperatureTable")
	}
	if !strings.Contains(s, `data-testid="app-root"`) {
		t.Error("M3 App.tsx must keep app-root testid")
	}
}

func TestTemperatureTable_TestidsAndStructure(t *testing.T) {
	dir := tempDir(t)
	h := GetTemplate("write-component")
	err := h(
		TemplateContext{ProjectDir: dir, BeadID: "tspa-9", Labels: []string{"milestone:m3"}},
		Frontmatter{Files: []string{"src/TemperatureTable.tsx"}, Extras: map[string]string{}},
	)
	if err != nil {
		t.Fatalf("write-component: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "src/TemperatureTable.tsx"))
	s := string(body)
	if !strings.Contains(s, `data-testid="temperature-table"`) {
		t.Error("must have temperature-table testid")
	}
	if !strings.Contains(s, "row-${r.celsius}") {
		t.Error("must template row-{c} testids")
	}
}

func TestTemperatureRows_FormulaAndRange(t *testing.T) {
	dir := tempDir(t)
	h := GetTemplate("write-component")
	err := h(
		TemplateContext{ProjectDir: dir, BeadID: "tspa-8", Labels: []string{"milestone:m3"}},
		Frontmatter{Files: []string{"src/temperatureRows.ts"}, Extras: map[string]string{}},
	)
	if err != nil {
		t.Fatalf("write-component: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "src/temperatureRows.ts"))
	s := string(body)
	if !strings.Contains(s, "c <= 300") {
		t.Error("must iterate up to 300")
	}
	if !strings.Contains(s, "c += 20") {
		t.Error("must step by 20")
	}
	if !strings.Contains(s, "9") || !strings.Contains(s, "5") || !strings.Contains(s, "32") {
		t.Error("must include 9/5/32 formula constants")
	}
	if !strings.Contains(s, "toFixed(1)") {
		t.Error("must format to 1 decimal place")
	}
}

func TestErrorThenRecover_FirstAttemptFails(t *testing.T) {
	dir := tempDir(t)
	h := GetTemplate("error-then-recover")
	ctx := TemplateContext{ProjectDir: dir, BeadID: "tspa-err-test-1", Labels: []string{}}
	fm := Frontmatter{Extras: map[string]string{}}
	if err := h(ctx, fm); err == nil {
		t.Fatal("first attempt must fail")
	} else if !strings.Contains(err.Error(), "deliberate first-attempt failure") {
		t.Fatalf("wrong error: %v", err)
	}
	if err := h(ctx, fm); err != nil {
		t.Fatalf("second attempt must succeed: %v", err)
	}
}
