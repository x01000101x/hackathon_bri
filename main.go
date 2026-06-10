package main

import (
	"flag"
	"fmt"
	"os"

	"hackaton-bri/agent"
)

func main() {
	// Define custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s <command> [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  generate    Analyze git diff and generate/update technical documentation\n\n")
		fmt.Fprintf(os.Stderr, "Options for 'generate':\n")
		flag.PrintDefaults()
	}

	if len(os.Args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "generate":
		generateCmd := flag.NewFlagSet("generate", flag.ExitOnError)
		customVersion := generateCmd.String("version", "", "Force documentation to use specified version string instead of auto-incrementing")
		
		// Parse flags after the subcommand
		if err := generateCmd.Parse(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("🚀 Initializing AI-Driven Technical Documentation Agent...")
		
		// Run documentation generation
		err := agent.RunGenerate(*customVersion)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Error executing document agent: %v\n", err)
			os.Exit(1)
		}
		
		fmt.Println("🎉 AI documentation successfully updated!")

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		flag.Usage()
		os.Exit(1)
	}
}
