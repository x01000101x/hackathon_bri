package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// RunGenerate is the main entry point for document generation
func RunGenerate(customVersion string) error {
	// Create context with an overall timeout of 5 minutes for safety
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. Check GEMINI_API_KEY
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("GEMINI_API_KEY environment variable is not set. Please set it before running")
	}

	modelName := os.Getenv("GEMINI_MODEL")

	// 2. Identify base branch (master or main)
	baseBranch := getBaseBranch(ctx)
	fmt.Printf("🔍 Detected base branch: %s\n", baseBranch)

	// 3. Get Go code diff
	goDiff, err := getGitDiff(ctx, baseBranch, "*.go")
	if err != nil {
		return fmt.Errorf("failed to retrieve Go diff: %v", err)
	}

	// 4. Get SQL diff
	sqlDiff, err := getGitDiff(ctx, baseBranch, "*.sql")
	if err != nil {
		return fmt.Errorf("failed to retrieve SQL diff: %v", err)
	}

	if goDiff == "" && sqlDiff == "" {
		fmt.Println("⚠️  Warning: No modifications detected in Go or SQL files. Creating empty audit log entry.")
		goDiff = "No changes detected."
		sqlDiff = "No SQL/GORM database changes detected."
	}

	// Ensure docs directory exists
	if err := os.MkdirAll("docs", 0755); err != nil {
		return fmt.Errorf("failed to create docs directory: %v", err)
	}

	// 5. Versioning Resolution
	version, err := resolveVersion("docs/code_review.html", customVersion)
	if err != nil {
		return fmt.Errorf("failed to resolve version: %v", err)
	}
	fmt.Printf("📦 Documentation version: %s\n", version)

	// 6. Initialize Reusable Review Client
	fmt.Println("🔌 Connecting to Google Generative AI Service...")
	client, err := NewReviewClient(ctx, apiKey, modelName)
	if err != nil {
		return fmt.Errorf("failed to initialize review client: %v", err)
	}
	defer client.Close()

	// 7. Call Gemini LLM to generate reviews
	fmt.Println("🤖 Calling Gemini API to review Code changes...")
	codeReviewContent, err := client.GenerateCodeReview(ctx, goDiff, version)
	if err != nil {
		return fmt.Errorf("failed to generate code review: %v", err)
	}

	fmt.Println("🤖 Calling Gemini API to review Database & Query changes...")
	// We pass both goDiff and sqlDiff to query review because GORM queries and model definitions reside in .go files
	queryReviewContent, err := client.GenerateQueryReview(ctx, goDiff, sqlDiff, version)
	if err != nil {
		return fmt.Errorf("failed to generate query review: %v", err)
	}

	// 8. Update HTML files
	now := time.Now().Format("2006-01-02 15:04:05")
	
	fmt.Println("✍️  Writing docs/code_review.html...")
	err = UpdateHTMLReport("docs/code_review.html", "Code Quality & Structural Audit", version, now, codeReviewContent)
	if err != nil {
		return fmt.Errorf("failed to update code_review.html: %v", err)
	}

	fmt.Println("✍️  Writing docs/query_review.html...")
	err = UpdateHTMLReport("docs/query_review.html", "Database Schema & Query Performance Audit", version, now, queryReviewContent)
	if err != nil {
		return fmt.Errorf("failed to update query_review.html: %v", err)
	}

	return nil
}

// getBaseBranch detects the main branch of the repository (master, main, or fallbacks)
func getBaseBranch(ctx context.Context) string {
	// Create context with short timeout for local git executions
	gitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// 1. Try to detect default remote branch from origin/HEAD
	cmd := exec.CommandContext(gitCtx, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
	if out, err := cmd.Output(); err == nil {
		ref := string(out)
		// e.g. "refs/remotes/origin/main\n" -> "main"
		if len(ref) > len("refs/remotes/origin/") {
			branch := ref[len("refs/remotes/origin/"):]
			// Trim trailing whitespace or carriage returns
			for len(branch) > 0 && (branch[len(branch)-1] == '\n' || branch[len(branch)-1] == '\r') {
				branch = branch[:len(branch)-1]
			}
			if runGitVerify(gitCtx, "origin/"+branch) || runGitVerify(gitCtx, branch) {
				return branch
			}
		}
	}

	// 2. Prioritize main/origin/main over master/origin/master
	if runGitVerify(gitCtx, "main") {
		return "main"
	}
	if runGitVerify(gitCtx, "origin/main") {
		return "origin/main"
	}
	if runGitVerify(gitCtx, "master") {
		return "master"
	}
	if runGitVerify(gitCtx, "origin/master") {
		return "origin/master"
	}
	return "HEAD~1"
}

// runGitVerify runs git rev-parse with context to check if a branch exists
func runGitVerify(ctx context.Context, branch string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", branch)
	err := cmd.Run()
	return err == nil
}

// getGitDiff runs git diff master...HEAD for a specific file pattern with context
func getGitDiff(ctx context.Context, baseBranch, filePattern string) (string, error) {
	gitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(gitCtx, "git", "diff", baseBranch+"...HEAD", "--", filePattern)
	out, err := cmd.Output()
	if err != nil {
		// If git diff fails, let's try git diff baseBranch -- filePattern as a backup
		cmd = exec.CommandContext(gitCtx, "git", "diff", baseBranch, "--", filePattern)
		out, err = cmd.Output()
		if err != nil {
			return "", err
		}
	}
	return string(out), nil
}
