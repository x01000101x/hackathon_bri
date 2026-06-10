package agent

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
)

const migrationSystemPrompt = `You are a senior Go developer. Your task is to migrate the provided Go source code to the latest Go version/standards and modern framework patterns.
CRITICAL RULES:
1. Do NOT change or alter any business logic, processes, or algorithms.
2. Only update language features, deprecated package functions, router definitions, and general syntax upgrades.
3. Output ONLY the migrated Go source code. Do not wrap in markdown or add explanations.`

// RunMigration walks through the srcPath, migrates any Go files, and writes them to targetDir.
func RunMigration(srcPath, targetDir string) error {
	color.Cyan("=== Starting Migration ===")
	fmt.Printf("  Source: %s\n  Target Directory: %s\n\n", srcPath, targetDir)

	info, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("source path error: %w", err)
	}

	if !info.IsDir() {
		if !strings.HasSuffix(srcPath, ".go") {
			return fmt.Errorf("source file is not a Go file")
		}
		return migrateFile(srcPath, filepath.Join(targetDir, filepath.Base(srcPath)))
	}

	// It's a directory
	err = filepath.WalkDir(srcPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		// Calculate relative path to preserve directory structure
		rel, err := filepath.Rel(srcPath, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		dest := filepath.Join(targetDir, rel)
		err = migrateFile(path, dest)
		if err != nil {
			color.Red("  Failed to migrate %s: %v", path, err)
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk source directory: %w", err)
	}

	color.Green("\nMigration complete! All migrated files are written to %s", targetDir)
	return nil
}

func migrateFile(src, dest string) error {
	fmt.Printf("  Migrating %s... ", filepath.Base(src))

	content, err := os.ReadFile(src)
	if err != nil {
		color.Red("✗")
		return fmt.Errorf("read file error: %w", err)
	}

	userPrompt := fmt.Sprintf("Please migrate this file to the latest Go version standards. Do not modify business logic.\n\nSource Code:\n%s", string(content))

	migratedCode, err := CallGemini(migrationSystemPrompt, userPrompt)
	if err != nil {
		color.Red("✗")
		return fmt.Errorf("gemini call error: %w", err)
	}

	cleanCode := StripMarkdownFences(migratedCode)

	// Create parent directories if they do not exist
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		color.Red("✗")
		return fmt.Errorf("create directory error: %w", err)
	}

	if err := os.WriteFile(dest, []byte(cleanCode), 0644); err != nil {
		color.Red("✗")
		return fmt.Errorf("write file error: %w", err)
	}

	color.Green("✓ (Saved to %s)", dest)
	return nil
}
