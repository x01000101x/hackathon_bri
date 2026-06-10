package agent

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"

	"github.com/fatih/color"
)

const optimizeSystemPrompt = `You are a senior Go developer specializing in code quality and optimization.
You are given a Go file along with a list of static analysis warnings (branching complexity > 15, duplicated expressions/literals, or unused imports/variables).
Your goal is to optimize the code to fix these warnings:
1. Refactor any functions with branching complexity > 15 into smaller, clean, and testable helper functions.
2. Remove any unused variables or imports.
3. Replace duplicated string literals or expressions (appearing 3+ times) with constants or helper variables/functions.
4. Keep the exact same business logic and behavior. Do not change how the code functions.
5. Output ONLY the optimized Go source code. Do not wrap in markdown or add explanations.`

// RunOptimization performs AST analysis, reports issues, generates optimized code via Gemini,
// and prompts the user with a color diff to confirm applying changes.
func RunOptimization(filePath string) error {
	color.Cyan("=== Starting Optimization Analysis ===")
	fmt.Printf("  Analyzing file: %s\n\n", filePath)

	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	originalCode := string(contentBytes)

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, originalCode, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse Go AST: %w", err)
	}

	var warnings []string

	// 1. Branching Complexity Check
	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		comp := computeComplexity(fn.Body)
		if comp > 15 {
			warn := fmt.Sprintf("Function '%s' has a branching complexity of %d (max limit is 15)", fn.Name.Name, comp)
			warnings = append(warnings, warn)
			color.Red("  [WARNING] %s", warn)
		}
		return true
	})

	// 2. Duplicated String Literals Check
	stringCounts := make(map[string]int)
	ast.Inspect(node, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		val := lit.Value
		// Ignore empty or very short strings
		if len(val) > 4 {
			stringCounts[val]++
		}
		return true
	})
	for val, count := range stringCounts {
		if count >= 3 {
			warn := fmt.Sprintf("String literal %s is duplicated %d times", val, count)
			warnings = append(warnings, warn)
			color.Yellow("  [SUGGESTION] %s", warn)
		}
	}

	// 3. Duplicated Expressions Check
	exprCounts := make(map[string]int)
	ast.Inspect(node, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.BinaryExpr, *ast.CallExpr, *ast.SelectorExpr:
			exprStr := nodeToString(fset, e)
			// Filter out short/simple expressions to avoid noise
			if len(exprStr) > 8 && !strings.Contains(exprStr, "\n") {
				exprCounts[exprStr]++
			}
		}
		return true
	})
	for expr, count := range exprCounts {
		if count >= 3 {
			warn := fmt.Sprintf("Expression '%s' is duplicated %d times", expr, count)
			warnings = append(warnings, warn)
			color.Yellow("  [SUGGESTION] %s", warn)
		}
	}

	// 4. Unused Imports Check (Lightweight AST-based check)
	usedPackages := make(map[string]bool)
	ast.Inspect(node, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok {
			usedPackages[ident.Name] = true
		}
		return true
	})
	for _, imp := range node.Imports {
		if imp.Name != nil && imp.Name.Name == "_" {
			continue // ignore blank imports
		}
		pathVal := strings.Trim(imp.Path.Value, `"`)
		parts := strings.Split(pathVal, "/")
		pkgName := parts[len(parts)-1]
		if imp.Name != nil {
			pkgName = imp.Name.Name
		}
		if !usedPackages[pkgName] {
			warn := fmt.Sprintf("Imported package '%s' (path: %s) appears to be unused", pkgName, pathVal)
			warnings = append(warnings, warn)
			color.Yellow("  [SUGGESTION] %s", warn)
		}
	}

	if len(warnings) == 0 {
		color.Green("\n  No static analysis warnings found. File is clean!")
	}

	color.Yellow("\nRequesting AI-optimized code version from Gemini...")
	warningsText := strings.Join(warnings, "\n")
	userPrompt := fmt.Sprintf("Please optimize the following Go file based on these warnings:\n%s\n\nSource Code:\n%s", warningsText, originalCode)

	optimizedCode, err := CallGemini(optimizeSystemPrompt, userPrompt)
	if err != nil {
		return fmt.Errorf("failed to optimize via Gemini: %w", err)
	}

	cleanOptimized := StripMarkdownFences(optimizedCode)

	if cleanOptimized == "" || strings.TrimSpace(cleanOptimized) == strings.TrimSpace(originalCode) {
		color.Green("  No changes suggested by Gemini.")
		return nil
	}

	color.Cyan("\n=== Suggested Optimizations Diff ===")
	PrintDiff(originalCode, cleanOptimized)

	// Ask user for confirmation
	fmt.Print("\nApply these optimizations? [y/N]: ")
	var input string
	fmt.Scanln(&input)
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "y" || input == "yes" {
		err = os.WriteFile(filePath, []byte(cleanOptimized), 0644)
		if err != nil {
			return fmt.Errorf("failed to write optimized file: %w", err)
		}
		color.Green("  Optimizations applied successfully!")
	} else {
		color.Yellow("  Optimizations discarded.")
	}

	return nil
}

func computeComplexity(body *ast.BlockStmt) int {
	if body == nil {
		return 0
	}
	count := 0
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.IfStmt:
			count++
		case *ast.CaseClause:
			count++
		}
		return true
	})
	return count
}

func nodeToString(fset *token.FileSet, node ast.Node) string {
	var buf bytes.Buffer
	err := printer.Fprint(&buf, fset, node)
	if err != nil {
		return ""
	}
	return buf.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func GetDiff(a, b []string) []string {
	n := len(a)
	m := len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}

	var result []string
	i, j := n, m
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			result = append([]string{"  " + a[i-1]}, result...)
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			result = append([]string{"+" + b[j-1]}, result...)
			j--
		} else if i > 0 && (j == 0 || dp[i-1][j] >= dp[i][j-1]) {
			result = append([]string{"-" + a[i-1]}, result...)
			i--
		}
	}
	return result
}

func PrintDiff(oldStr, newStr string) {
	oldLines := strings.Split(oldStr, "\n")
	newLines := strings.Split(newStr, "\n")
	diffLines := GetDiff(oldLines, newLines)

	type diffLine struct {
		text string
		op   rune
	}
	parsed := make([]diffLine, len(diffLines))
	for idx, line := range diffLines {
		if len(line) == 0 {
			parsed[idx] = diffLine{text: "", op: ' '}
		} else {
			parsed[idx] = diffLine{text: line[1:], op: rune(line[0])}
		}
	}

	n := len(parsed)
	show := make([]bool, n)
	for i := 0; i < n; i++ {
		if parsed[i].op != ' ' {
			for k := i - 3; k <= i+3; k++ {
				if k >= 0 && k < n {
					show[k] = true
				}
			}
		}
	}

	inHunk := false
	for i := 0; i < n; i++ {
		if show[i] {
			if !inHunk {
				color.Blue("@@ ... @@")
				inHunk = true
			}
			switch parsed[i].op {
			case '+':
				color.Green("+%s", parsed[i].text)
			case '-':
				color.Red("-%s", parsed[i].text)
			default:
				color.White(" %s", parsed[i].text)
			}
		} else {
			inHunk = false
		}
	}
}
