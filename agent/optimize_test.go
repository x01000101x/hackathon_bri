package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestComputeComplexity(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected int
	}{
		{
			name: "Simple function",
			src: `package main
func hello() {
	println("hello")
}`,
			expected: 0,
		},
		{
			name: "One if statement",
			src: `package main
func hello(x int) {
	if x > 10 {
		println("large")
	}
}`,
			expected: 1,
		},
		{
			name: "If-else if-else and switch case statements",
			src: `package main
func hello(x int) {
	if x > 10 {
		println("large")
	} else if x > 5 {
		println("medium")
	} else {
		println("small")
	}

	switch x {
	case 1:
		println("one")
	case 2:
		println("two")
	default:
		println("other")
	}
}`,
			expected: 5, // 2 IfStmts + 3 CaseClauses (case 1, case 2, default)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			fileNode, err := parser.ParseFile(fset, "test.go", tt.src, 0)
			if err != nil {
				t.Fatalf("failed to parse test source: %v", err)
			}

			var fn *ast.FuncDecl
			for _, decl := range fileNode.Decls {
				if f, ok := decl.(*ast.FuncDecl); ok {
					fn = f
					break
				}
			}

			if fn == nil {
				t.Fatalf("no function found in test source")
			}

			actual := computeComplexity(fn.Body)
			if actual != tt.expected {
				t.Errorf("expected complexity %d, got %d", tt.expected, actual)
			}
		})
	}
}
