package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	// Setup a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "agent_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	t.Run("Custom version override (removes v prefix if any)", func(t *testing.T) {
		version, err := resolveVersion("non_existent_file.html", "v2.3.4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if version != "v2.3.4" {
			t.Errorf("expected v2.3.4, got %s", version)
		}

		version, err = resolveVersion("non_existent_file.html", "1.2.3")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if version != "v1.2.3" {
			t.Errorf("expected v1.2.3, got %s", version)
		}
	})

	t.Run("Default to v1.0.0 when file does not exist", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "new_report.html")
		version, err := resolveVersion(filePath, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if version != "v1.0.0" {
			t.Errorf("expected v1.0.0, got %s", version)
		}
	})

	t.Run("Parse and increment patch version from existing HTML", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "existing_report.html")
		
		// Write a dummy HTML containing the version tag <!-- AGENT_VERSION: v1.4.12 -->
		dummyHTML := `<!DOCTYPE html>
		<html>
		<head>
			<meta charset="UTF-8">
			<title>Test Code Review</title>
		</head>
		<body>
			<!-- AGENT_VERSION: v1.4.12 -->
			<h1>Code Quality</h1>
		</body>
		</html>`
		
		err := os.WriteFile(filePath, []byte(dummyHTML), 0644)
		if err != nil {
			t.Fatalf("failed to write dummy HTML: %v", err)
		}

		version, err := resolveVersion(filePath, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Expect v1.4.12 to increment to v1.4.13
		if version != "v1.4.13" {
			t.Errorf("expected v1.4.13, got %s", version)
		}
	})

	t.Run("Handle malformed version in file gracefully", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "malformed_report.html")
		
		// Write a dummy HTML containing a malformed version tag <!-- AGENT_VERSION: v1.invalid.3 -->
		dummyHTML := `<!-- AGENT_VERSION: v1.invalid.3 -->`
		
		err := os.WriteFile(filePath, []byte(dummyHTML), 0644)
		if err != nil {
			t.Fatalf("failed to write dummy HTML: %v", err)
		}

		version, err := resolveVersion(filePath, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Malformed should fallback to v1.0.0
		if version != "v1.0.0" {
			t.Errorf("expected fallback v1.0.0, got %s", version)
		}
	})
}
