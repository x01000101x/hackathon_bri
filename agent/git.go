package agent

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/fatih/color"
)

// RunGitPull pulls from remote. If a conflict is encountered, it runs the auto-resolver.
func RunGitPull() error {
	if !confirmPrompt("Do you want to run 'git pull'?") {
		color.Yellow("  Operation cancelled.")
		return nil
	}

	color.Yellow("Running 'git pull'...")
	cmd := exec.Command("git", "pull")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	outStr := stdout.String() + "\n" + stderr.String()
	fmt.Println(outStr)

	// Check for merge conflicts
	if err != nil || strings.Contains(outStr, "CONFLICT") || hasUnmergedFiles() {
		color.Red("  Merge conflicts detected!")
		return handleConflicts()
	}

	color.Green("  Pull completed successfully.")
	return nil
}

// RunGitCommit prompts user with git diff and commits with the message.
func RunGitCommit(message string) error {
	if message == "" {
		return fmt.Errorf("commit message cannot be empty")
	}

	// Show git status/diff
	color.Yellow("Showing staged changes (git diff --cached):")
	cmdDiff := exec.Command("git", "diff", "--cached")
	diffOut, _ := cmdDiff.CombinedOutput()
	if len(diffOut) == 0 {
		color.Yellow("  No staged changes found. Showing unstaged diff:")
		cmdDiffUnstaged := exec.Command("git", "diff")
		diffOut, _ = cmdDiffUnstaged.CombinedOutput()
		if len(diffOut) == 0 {
			color.Green("  Nothing to commit.")
			return nil
		}
	}
	fmt.Println(string(diffOut))

	if !confirmPrompt("Do you want to commit these changes?") {
		color.Yellow("  Operation cancelled.")
		return nil
	}

	// Staging changes if none are staged
	cmdStatus := exec.Command("git", "status", "--porcelain")
	statusOut, _ := cmdStatus.Output()
	hasUnstaged := false
	for _, line := range strings.Split(string(statusOut), "\n") {
		if strings.HasPrefix(line, " M") || strings.HasPrefix(line, "??") {
			hasUnstaged = true
			break
		}
	}

	if hasUnstaged && confirmPrompt("There are unstaged changes. Stage all Go/test changes first?") {
		exec.Command("git", "add", "*.go").Run()
	}

	color.Yellow("Running 'git commit'...")
	cmdCommit := exec.Command("git", "commit", "-m", message)
	commitOut, err := cmdCommit.CombinedOutput()
	fmt.Println(string(commitOut))
	if err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}

	color.Green("  Changes committed successfully.")
	return nil
}

// RunGitPush pushes changes to remote.
func RunGitPush() error {
	if !confirmPrompt("Do you want to run 'git push'?") {
		color.Yellow("  Operation cancelled.")
		return nil
	}

	color.Yellow("Running 'git push'...")
	cmd := exec.Command("git", "push")
	out, err := cmd.CombinedOutput()
	fmt.Println(string(out))
	if err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}

	color.Green("  Push completed successfully.")
	return nil
}

func hasUnmergedFiles() bool {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	out, _ := cmd.Output()
	return len(strings.TrimSpace(string(out))) > 0
}

func getConflictFiles() ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []string
	for _, f := range strings.Split(string(out), "\n") {
		f = strings.TrimSpace(f)
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

func handleConflicts() error {
	files, err := getConflictFiles()
	if err != nil {
		return fmt.Errorf("failed to get conflict files: %w", err)
	}

	userEmail, err := getUserEmail()
	if err != nil {
		color.Red("  Could not retrieve git user.email: %v", err)
	} else {
		fmt.Printf("  Current User: %s\n", userEmail)
	}

	for _, file := range files {
		color.Cyan("\nResolving conflict in: %s", file)

		// 1. Get authors of recent local unpushed commits on this branch
		authors, err := getLocalBranchAuthors()
		if err != nil {
			color.Yellow("  Warning: could not get local branch authors: %v", err)
		}

		// Check if all local commits were authored by the current user
		allOwnCommits := true
		if len(authors) == 0 {
			allOwnCommits = false
		} else {
			for _, author := range authors {
				if author != userEmail {
					allOwnCommits = false
					break
				}
			}
		}

		if allOwnCommits {
			color.Green("  -> All local changes in this branch were authored by you (%s).", userEmail)
			color.Green("  -> Automatically accepting remote/incoming changes...")
			err = acceptIncoming(file)
			if err != nil {
				return err
			}
		} else {
			color.Yellow("  -> Local commits were authored by multiple users or a different user: %v", authors)
			fmt.Printf("  How do you want to resolve conflict in %s?\n", file)
			fmt.Println("    [t] Accept remote/incoming changes (theirs)")
			fmt.Println("    [o] Keep local changes (ours)")
			fmt.Println("    [m] Skip and resolve manually later")
			fmt.Print("  Choose option [t/o/m]: ")

			var choice string
			fmt.Scanln(&choice)
			choice = strings.TrimSpace(strings.ToLower(choice))

			switch choice {
			case "t":
				err = acceptIncoming(file)
				if err != nil {
					return err
				}
			case "o":
				err = keepLocal(file)
				if err != nil {
					return err
				}
			default:
				color.Yellow("  Skipped %s. Please resolve it manually.", file)
			}
		}
	}

	return nil
}

func getUserEmail() (string, error) {
	cmd := exec.Command("git", "config", "user.email")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func getLocalBranchAuthors() ([]string, error) {
	// 1. Try to check @{u}..HEAD (unpushed commits against upstream tracking branch)
	cmd := exec.Command("git", "log", "@{u}..HEAD", "--format=%ae")
	out, err := cmd.Output()
	if err != nil {
		// No upstream configured, fallback to last 10 commits on current branch
		cmd = exec.Command("git", "log", "-n", "10", "--format=%ae")
		out, err = cmd.Output()
		if err != nil {
			return nil, err
		}
	}

	authorMap := make(map[string]bool)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		email := strings.TrimSpace(line)
		if email != "" {
			authorMap[email] = true
		}
	}

	var authors []string
	for author := range authorMap {
		authors = append(authors, author)
	}
	return authors, nil
}

func acceptIncoming(file string) error {
	// Accept incoming (theirs)
	cmdCheckout := exec.Command("git", "checkout", "--theirs", file)
	if err := cmdCheckout.Run(); err != nil {
		return fmt.Errorf("failed to checkout remote version: %w", err)
	}

	cmdAdd := exec.Command("git", "add", file)
	if err := cmdAdd.Run(); err != nil {
		return fmt.Errorf("failed to add file: %w", err)
	}

	color.Green("  Resolved conflict by accepting remote/incoming changes in %s.", file)
	return nil
}

func keepLocal(file string) error {
	// Keep local (ours)
	cmdCheckout := exec.Command("git", "checkout", "--ours", file)
	if err := cmdCheckout.Run(); err != nil {
		return fmt.Errorf("failed to checkout local version: %w", err)
	}

	cmdAdd := exec.Command("git", "add", file)
	if err := cmdAdd.Run(); err != nil {
		return fmt.Errorf("failed to add file: %w", err)
	}

	color.Green("  Resolved conflict by keeping local changes in %s.", file)
	return nil
}

func confirmPrompt(label string) bool {
	fmt.Printf("%s [y/N]: ", label)
	var input string
	fmt.Scanln(&input)
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}
