package agent

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
)

const testGenSystemPrompt = `You are a senior Go developer specializing in writing unit tests.
Your goal is to write a complete, robust, and compile-ready test file using Go's standard 'testing' package.
Rules:
1. Cover happy paths, edge cases, error conditions, and boundary values.
2. Use table-driven tests where appropriate.
3. If mocks exist in the package or a mocks/ directory, import and use them.
4. Output ONLY the test Go code. Do not wrap in markdown or add explanations.`

// RunTestGen detects changed files, runs mockery if needed, generates tests via Gemini,
// runs them, and self-corrects on failure.
func RunTestGen(targetPkg string) error {
	color.Cyan("=== Starting Unit Test Generator ===")

	// 1. Detect changed files
	color.Yellow("\nDetecting modified Go files...")
	files, err := getChangedGoFiles()
	if err != nil {
		return fmt.Errorf("failed to get changed files: %w", err)
	}

	if len(files) == 0 {
		color.Green("  No uncommitted Go changes detected via Git status.")
		fmt.Print("  Enter path of a Go file you want to test (or press Enter to exit): ")
		var input string
		fmt.Scanln(&input)
		input = strings.TrimSpace(input)
		if input == "" {
			return nil
		}
		if _, err := os.Stat(input); err != nil {
			return fmt.Errorf("file not found: %s", input)
		}
		files = []string{input}
	} else {
		fmt.Println("  Modified files found:")
		for _, f := range files {
			color.White("    - %s", f)
		}
	}

	for _, file := range files {
		err := processFileTests(file)
		if err != nil {
			color.Red("  Failed for %s: %v", file, err)
		}
	}

	return nil
}

func getChangedGoFiles() ([]string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []string
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		path := parts[1]
		// Match Go source files that are modified (M), added (A), or untracked (??)
		if (status == "M" || status == "A" || status == "??" || status == "AM") && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			if _, err := os.Stat(path); err == nil {
				files = append(files, path)
			}
		}
	}
	return files, nil
}

func processFileTests(filePath string) error {
	color.Cyan("\n--- Processing %s ---", filePath)
	pkgDir := filepath.Dir(filePath)

	// 1. Check for interfaces in package to run mockery
	if detectInterfacesInPackage(pkgDir) {
		_ = runMockery(pkgDir)
	}

	// 2. Read file contents
	srcBytes, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	srcCode := string(srcBytes)

	// 3. Ask Gemini to generate tests
	color.Yellow("Generating unit tests via Gemini...")
	userPrompt := fmt.Sprintf("Please generate a robust unit test file for this Go source code:\n\nSource Code:\n%s", srcCode)

	generatedTest, err := CallGemini(testGenSystemPrompt, userPrompt)
	if err != nil {
		return fmt.Errorf("gemini error: %w", err)
	}

	cleanTestCode := StripMarkdownFences(generatedTest)

	// Preview and ask for permission
	color.Cyan("\nGenerated Unit Test Preview:")
	fmt.Println(cleanTestCode)

	testFilePath := strings.TrimSuffix(filePath, ".go") + "_test.go"
	fmt.Printf("\nWrite this unit test file to %s? [y/N]: ", testFilePath)
	var input string
	fmt.Scanln(&input)
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "y" || input == "yes" {
		err = os.WriteFile(testFilePath, []byte(cleanTestCode), 0644)
		if err != nil {
			return fmt.Errorf("failed to write test file: %w", err)
		}
		color.Green("  Test file written! Starting execution & self-correction loop...")

		return runTestAndSelfCorrect(filePath, testFilePath)
	}

	color.Yellow("  Skipped writing test file.")
	return nil
}

func detectInterfacesInPackage(dir string) bool {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.DeclarationErrors)
	if err != nil {
		return false
	}
	found := false
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				if _, ok := n.(*ast.InterfaceType); ok {
					found = true
					return false // stop searching
				}
				return true
			})
			if found {
				break
			}
		}
		if found {
			break
		}
	}
	return found
}

func runMockery(dir string) error {
	fmt.Printf("  Interfaces found. Running mockery in %s... ", dir)
	cmd := exec.Command("mockery", "--all", "--dir", dir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Mockery warning: %v\n", stderr.String())
		return err
	}
	color.Green("✓ (Mocks generated)")
	return nil
}

func runTestAndSelfCorrect(filePath, testFilePath string) error {
	maxAttempts := 3
	pkgDir := filepath.Dir(filePath)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Printf("  Running tests (Attempt %d/%d)... ", attempt, maxAttempts)

		// Run go test for the package
		cmd := exec.Command("go", "test", "-v", "./"+pkgDir)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()

		testOutput := out.String()
		if err == nil {
			color.Green("✓ (Tests Passed!)")
			return nil
		}

		color.Red("✗ (Tests Failed)")
		fmt.Println("--- Test Failure Output ---")
		fmt.Println(testOutput)
		fmt.Println("---------------------------")

		if attempt == maxAttempts {
			return fmt.Errorf("unit tests failed after %d attempts", maxAttempts)
		}

		// Self-correct loop
		srcContent, _ := os.ReadFile(filePath)
		testContent, _ := os.ReadFile(testFilePath)

		color.Yellow("  Sending compile/test failure to Gemini for self-correction...")
		correctionPrompt := fmt.Sprintf(`The generated unit tests failed with the following output:
%s

Original Source Code:
%s

Generated Test Code:
%s

Please fix the test code to make it compile and pass. Output ONLY the complete corrected Go test file. Do not wrap in markdown or add explanations.`, testOutput, string(srcContent), string(testContent))

		correctedCode, err := CallGemini(testGenSystemPrompt, correctionPrompt)
		if err != nil {
			return fmt.Errorf("failed to call Gemini for correction: %w", err)
		}

		cleanCorrected := StripMarkdownFences(correctedCode)
		if err := os.WriteFile(testFilePath, []byte(cleanCorrected), 0644); err != nil {
			return fmt.Errorf("failed to write corrected test: %w", err)
		}
	}
	return nil
}
