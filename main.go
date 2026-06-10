package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"brd_gen/agent"

	"github.com/spf13/cobra"
)

var apiKey string

func main() {
	var rootCmd = &cobra.Command{
		Use:   "go-agent",
		Short: "Go Agent CLI - Developer assistant for migrations, optimizations, unit testing, and git helper",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Resolve API Key
			if apiKey == "" {
				apiKey = os.Getenv("GEMINI_API_KEY")
			}
			if apiKey == "" {
				// Try to read .env file in workspace
				if data, err := os.ReadFile(".env"); err == nil {
					lines := strings.Split(string(data), "\n")
					for _, line := range lines {
						line = strings.TrimSpace(line)
						if strings.HasPrefix(line, "GEMINI_API_KEY=") {
							apiKey = strings.TrimPrefix(line, "GEMINI_API_KEY=")
							apiKey = strings.TrimSpace(apiKey)
						}
					}
				}
			}
			agent.APIKey = apiKey
		},
	}

	rootCmd.PersistentFlags().StringVar(&apiKey, "api-key", "", "Gemini API Key (overrides GEMINI_API_KEY env var)")

	// 1. Command 'brd'
	var brdCmd = &cobra.Command{
		Use:   "brd [path]",
		Short: "Extract BRD text from docx and generate service files",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			path := "BRD_Endpoint_Migration.docx"
			if len(args) > 0 {
				path = args[0]
			}
			if agent.APIKey == "" {
				log.Fatal("GEMINI_API_KEY must be set or passed via --api-key flag")
			}
			if err := agent.RunBRDGen(path); err != nil {
				log.Fatalf("BRD generation error: %v", err)
			}
		},
	}
	rootCmd.AddCommand(brdCmd)

	// 2. Command 'migrate'
	var migrateFile string
	var targetDir string
	var migrateCmd = &cobra.Command{
		Use:   "migrate",
		Short: "Migrate code to newer language version or framework versions",
		Run: func(cmd *cobra.Command, args []string) {
			if migrateFile == "" {
				log.Fatal("--file or -f is required")
			}
			if agent.APIKey == "" {
				log.Fatal("GEMINI_API_KEY must be set or passed via --api-key flag")
			}
			if err := agent.RunMigration(migrateFile, targetDir); err != nil {
				log.Fatalf("Migration error: %v", err)
			}
		},
	}
	migrateCmd.Flags().StringVarP(&migrateFile, "file", "f", "", "Source file or directory to migrate (required)")
	migrateCmd.Flags().StringVarP(&targetDir, "target-dir", "t", "./migrated", "Target directory to write migrated files")
	rootCmd.AddCommand(migrateCmd)

	// 3. Command 'optimize'
	var optimizeFile string
	var optimizeCmd = &cobra.Command{
		Use:   "optimize",
		Short: "Analyze and optimize Go code for complexity, unused variables, and duplicates",
		Run: func(cmd *cobra.Command, args []string) {
			if optimizeFile == "" {
				log.Fatal("--file or -f is required")
			}
			if agent.APIKey == "" {
				log.Fatal("GEMINI_API_KEY must be set or passed via --api-key flag")
			}
			if err := agent.RunOptimization(optimizeFile); err != nil {
				log.Fatalf("Optimization error: %v", err)
			}
		},
	}
	optimizeCmd.Flags().StringVarP(&optimizeFile, "file", "f", "", "Go file to optimize (required)")
	rootCmd.AddCommand(optimizeCmd)

	// 4. Command 'test'
	var testPkg string
	var testCmd = &cobra.Command{
		Use:   "test",
		Short: "Generate unit tests for modified files, generate mocks, and run tests with self-correction",
		Run: func(cmd *cobra.Command, args []string) {
			if agent.APIKey == "" {
				log.Fatal("GEMINI_API_KEY must be set or passed via --api-key flag")
			}
			if err := agent.RunTestGen(testPkg); err != nil {
				log.Fatalf("Test generation error: %v", err)
			}
		},
	}
	testCmd.Flags().StringVarP(&testPkg, "pkg", "p", "./...", "Package or path to test")
	rootCmd.AddCommand(testCmd)

	// 5. Command 'git'
	var gitCmd = &cobra.Command{
		Use:   "git",
		Short: "Run secure Git lifecycle operations (pull, commit, push) with merge conflict auto-resolution",
	}

	var gitPullCmd = &cobra.Command{
		Use:   "pull",
		Short: "Pull remote changes and automatically resolve merge conflicts if authored by you",
		Run: func(cmd *cobra.Command, args []string) {
			if err := agent.RunGitPull(); err != nil {
				log.Fatalf("Git pull error: %v", err)
			}
		},
	}
	gitCmd.AddCommand(gitPullCmd)

	var commitMsg string
	var gitCommitCmd = &cobra.Command{
		Use:   "commit",
		Short: "Stage and commit changes with diff review confirmation",
		Run: func(cmd *cobra.Command, args []string) {
			if commitMsg == "" {
				log.Fatal("--message or -m is required")
			}
			if err := agent.RunGitCommit(commitMsg); err != nil {
				log.Fatalf("Git commit error: %v", err)
			}
		},
	}
	gitCommitCmd.Flags().StringVarP(&commitMsg, "message", "m", "", "Commit message (required)")
	gitCmd.AddCommand(gitCommitCmd)

	var gitPushCmd = &cobra.Command{
		Use:   "push",
		Short: "Push committed changes to remote",
		Run: func(cmd *cobra.Command, args []string) {
			if err := agent.RunGitPush(); err != nil {
				log.Fatalf("Git push error: %v", err)
			}
		},
	}
	gitCmd.AddCommand(gitPushCmd)

	rootCmd.AddCommand(gitCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
