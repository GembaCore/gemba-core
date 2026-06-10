package constitution

import (
	"errors"
	"strings"
	"testing"
)

func TestMigrateConstitution_InjectsIntoFrontmatter(t *testing.T) {
	in := []byte("---\nasdd_mode: true\n---\n\n# Body\n")
	out, err := MigrateConstitution(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "schema_version: "+CurrentVersion) {
		t.Fatalf("expected schema_version line injected, got:\n%s", out)
	}
	// Idempotent: a second pass must not mutate beyond whitespace.
	out2, err := MigrateConstitution(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(out2) {
		t.Fatalf("not idempotent:\nA: %s\nB: %s", out, out2)
	}
}

func TestMigrateConstitution_InjectsIntoConfigFence(t *testing.T) {
	in := []byte("# C\n\n## Config\n\n```yaml\nasdd_mode: true\n```\n")
	out, err := MigrateConstitution(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "schema_version: "+CurrentVersion) {
		t.Fatalf("expected schema_version injected into config fence, got:\n%s", out)
	}
}

func TestMigrateConstitution_NoYAMLPrependsFrontmatter(t *testing.T) {
	in := []byte("# Plain markdown\n\nNo YAML here.\n")
	out, err := MigrateConstitution(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "---\nschema_version: "+CurrentVersion+"\n---\n") {
		t.Fatalf("expected prepended frontmatter, got:\n%s", out)
	}
}

func TestMigrateConstitution_PresentVersionPasses(t *testing.T) {
	in := []byte("---\nschema_version: \"" + CurrentVersion + "\"\nasdd_mode: true\n---\n")
	out, err := MigrateConstitution(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(in) {
		t.Fatalf("present version should be no-op")
	}
}

func TestMigrateConstitution_UnsupportedVersion(t *testing.T) {
	in := []byte("---\nschema_version: 9.9.9\n---\n")
	_, err := MigrateConstitution(in)
	if !errors.Is(err, ErrUnsupportedConstitutionVersion) {
		t.Fatalf("expected ErrUnsupportedConstitutionVersion, got %v", err)
	}
}

func TestCurrentVersion_IsSemver(t *testing.T) {
	if !versionLineRe.MatchString("schema_version: " + CurrentVersion) {
		t.Fatalf("CurrentVersion %q does not match semver pattern", CurrentVersion)
	}
}
