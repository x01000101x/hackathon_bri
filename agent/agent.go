package agent

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// RunGenerate is the main entry point for document generation
func RunGenerate(customVersion string) error {
	// 1. Check GEMINI_API_KEY
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("GEMINI_API_KEY environment variable is not set. Please set it before running")
	}

	// 2. Identify base branch (master or main)
	baseBranch := getBaseBranch()
	fmt.Printf("🔍 Detected base branch: %s\n", baseBranch)

	// 3. Get Go code diff
	goDiff, err := getGitDiff(baseBranch, "*.go")
	if err != nil {
		return fmt.Errorf("failed to retrieve Go diff: %v", err)
	}

	// 4. Get SQL diff
	sqlDiff, err := getGitDiff(baseBranch, "*.sql")
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

	// 6. Call Gemini LLM to generate reviews
	fmt.Println("🤖 Calling Gemini API to review Code changes...")
	codeReviewContent, err := GenerateCodeReview(goDiff, version)
	if err != nil {
		return fmt.Errorf("failed to generate code review: %v", err)
	}

	fmt.Println("🤖 Calling Gemini API to review Database & Query changes...")
	// We pass both goDiff and sqlDiff to query review because GORM queries and model definitions reside in .go files
	queryReviewContent, err := GenerateQueryReview(goDiff, sqlDiff, version)
	if err != nil {
		return fmt.Errorf("failed to generate query review: %v", err)
	}

	// 7. Update HTML files
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
func getBaseBranch() string {
	// Check if master branch exists locally
	if runGitVerify("master") {
		return "master"
	}
	// Check if main branch exists locally
	if runGitVerify("main") {
		return "main"
	}
	// Try remote branches
	if runGitVerify("origin/master") {
		return "origin/master"
	}
	if runGitVerify("origin/main") {
		return "origin/main"
	}
	// Default fallback to HEAD~1
	return "HEAD~1"
}

// runGitVerify runs git rev-parse to check if a branch exists
func runGitVerify(branch string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", branch)
	err := cmd.Run()
	return err == nil
}

// getGitDiff runs git diff master...HEAD for a specific file pattern
func getGitDiff(baseBranch, filePattern string) (string, error) {
	// git diff baseBranch...HEAD runs the diff from the common ancestor of baseBranch and HEAD
	cmd := exec.Command("git", "diff", baseBranch+"...HEAD", "--", filePattern)
	out, err := cmd.Output()
	if err != nil {
		// If git diff fails, let's try git diff baseBranch -- filePattern as a backup
		cmd = exec.Command("git", "diff", baseBranch, "--", filePattern)
		out, err = cmd.Output()
		if err != nil {
			return "", err
		}
	}
	return string(out), nil
}
