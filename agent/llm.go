package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// GenerateCodeReview sends the Go diff to Gemini and receives a code quality audit in Markdown
func GenerateCodeReview(goDiff, version string) (string, error) {
	prompt := fmt.Sprintf(`You are an expert Go developer and Senior Code Auditor. 
Analyze the following git diff representing changes introduced in version %s of our Go application.

Conduct a thorough Code Quality, Structural, and Code-Smell Audit. Your review must be returned in clean, professional Markdown format, covering the following points:
1. **Summary of Code Modifications**: Describe what changed and what features or packages are affected.
2. **Structural Adjustments & Architecture**: Assess the design of packages, interfaces, structs, and dependency structures.
3. **Go Quality, Idiomatic Style & Error Handling**: Evaluate naming conventions, correctness, proper error wrapping, and adherence to Go best practices.
4. **Code Smells & Security Flaws**: Detect issues such as resource leaks (e.g., unclosed files or response bodies), potential goroutine leaks, race conditions, or unhandled inputs.
5. **Actionable Recommendations**: Provide concrete recommendations with short code snippets showing how to fix any identified issues.

Here is the Git Diff:
"""
%s
"""
`, version, goDiff)

	return callGeminiAPI(prompt)
}

// GenerateQueryReview sends the Go & SQL diff to Gemini and receives a database and query audit in Markdown
func GenerateQueryReview(goDiff, sqlDiff, version string) (string, error) {
	prompt := fmt.Sprintf(`You are an expert Database Administrator, GORM specialist, and SQL Performance Tuning Engineer.
Analyze the following git diffs (including Go file modifications and raw SQL migrations) representing database-related changes introduced in version %s.

Conduct a rigorous Database Schema, GORM, and Query Performance Audit. Your review must be returned in clean, professional Markdown format, covering:
1. **Schema Migrations & Structural Changes**: Summary of new/modified tables, fields, constraints, or migrations.
2. **GORM & Database Interactions**: Review of GORM models, associations, preloading, transactions, and custom query hooks.
3. **Raw SQL Analysis**: Inspect any raw SQL queries written or executed.
4. **Query Performance & Optimizations**: Perform a deep audit of indexing strategies, potential N+1 query patterns, slow query risks, and lock contentions.
5. **Actionable Database Recommendations**: Provide concrete performance tuning or model corrections, including optimized SQL/Go code snippets.

Here are the Git Diffs:

--- GO DIFF (for GORM models and queries) ---
%s

--- SQL DIFF (for raw migrations and schemas) ---
%s
`, version, goDiff, sqlDiff)

	return callGeminiAPI(prompt)
}

// callGeminiAPI interacts with the official Google Generative AI Go SDK
func callGeminiAPI(prompt string) (string, error) {
	ctx := context.Background()
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return "", fmt.Errorf("failed to create Gemini client: %v", err)
	}
	defer client.Close()

	// Use gemini-1.5-flash as the default high-speed model
	modelName := "gemini-1.5-flash"
	if customModel := os.Getenv("GEMINI_MODEL"); customModel != "" {
		modelName = customModel
	}

	model := client.GenerativeModel(modelName)
	
	// Set reasonable creativity limits for analytical work
	temp := float32(0.2)
	model.Temperature = &temp

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", fmt.Errorf("Gemini API execution error: %v", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return "", fmt.Errorf("Gemini returned an empty response candidate list")
	}

	var parts []string
	for _, part := range resp.Candidates[0].Content.Parts {
		parts = append(parts, fmt.Sprint(part))
	}

	result := strings.Join(parts, "")
	if result == "" {
		return "", fmt.Errorf("Gemini generated an empty response content")
	}

	return result, nil
}
