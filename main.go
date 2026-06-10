package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"bytes"
	"path/filepath"
	"strings"
	"time"
)

// ─── Gemini API Configuration ───────────────────────────────────────────────

const (
	geminiAPI   = "https://generativelanguage.googleapis.com/v1beta/models"
	maxTokens   = 8192
)

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiGenerationConfig struct {
	MaxOutputTokens  int    `json:"maxOutputTokens,omitempty"`
	ResponseMimeType string `json:"responseMimeType,omitempty"`
}

type GeminiRequest struct {
	SystemInstruction *GeminiContent         `json:"system_instruction,omitempty"`
	Contents          []GeminiContent        `json:"contents"`
	GenerationConfig  GeminiGenerationConfig `json:"generationConfig"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content GeminiContent `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

// ─── Planning and Generation Types ──────────────────────────────────────────

type PlannedFile struct {
	Filename     string `json:"filename"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
}

const planSystemPrompt = `You are an expert software architect and systems engineer.
Given a Business Requirements Document (BRD) for an application request, analyze it and plan the project structure.
Determine all the source code files, configurations, schemas, and documentation files required to build a fully working, production-ready application.
Your output must be a valid JSON array of file objects. Do not include markdown code block formatting or any text other than the raw JSON.

Each file object must contain:
1. "filename": The relative path and name of the file (e.g., "db/connection.go", "handlers/user.go", "main.go", "go.mod").
2. "description": A short explanation of what this file does.
3. "instructions": Extremely detailed technical instructions on what implementation details, structs, functions, routing, error handling, or variables are required in this file. Be very specific to ensure the code generator can write complete, production-quality code.

Example output format:
[
  {
    "filename": "main.go",
    "description": "App entry point",
    "instructions": "Set up HTTP server on port 8080. Import and register endpoints from the handlers package. Implement graceful shutdown on SIGINT/SIGTERM."
  }
]
`

const generateSystemPrompt = `You are a senior developer.
Given a Business Requirements Document (BRD) and specific instructions for a target file, you generate the COMPLETE, production-ready source code/content for that file.
You output ONLY the code content — no explanations outside comments.
Do not output markdown code fences (like triple backtick go ... triple backtick). Start the output directly.
Ensure proper imports, variables, structures, and business logic according to the BRD. Use robust error handling and proper logging.`

// ─── Gemini API Client ──────────────────────────────────────────────────────

func callGemini(apiKey, model, system, userPrompt string, jsonMode bool) (string, error) {
	config := GeminiGenerationConfig{
		MaxOutputTokens: maxTokens,
	}
	if jsonMode {
		config.ResponseMimeType = "application/json"
	}

	payload := GeminiRequest{
		SystemInstruction: &GeminiContent{
			Role:  "system",
			Parts: []GeminiPart{{Text: system}},
		},
		Contents: []GeminiContent{
			{Role: "user", Parts: []GeminiPart{{Text: userPrompt}}},
		},
		GenerationConfig: config,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiAPI, model, apiKey)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var apiResp GeminiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w, raw response: %s", err, string(respBody))
	}

	if apiResp.Error != nil {
		return "", fmt.Errorf("API error [%s]: %s", apiResp.Error.Status, apiResp.Error.Message)
	}

	if len(apiResp.Candidates) == 0 || len(apiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no response candidates returned")
	}

	var result strings.Builder
	for _, part := range apiResp.Candidates[0].Content.Parts {
		result.WriteString(part.Text)
	}
	return result.String(), nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	// Remove opening fence (```go, ```proto, ```, etc)
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		if strings.HasPrefix(first, "```") {
			lines = lines[1:]
		}
	}
	// Remove closing fence
	if len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "```" {
			lines = lines[:len(lines)-1]
		}
	}
	return strings.Join(lines, "\n")
}

func banner(msg string) {
	line := strings.Repeat("─", 60)
	fmt.Printf("\n%s\n  %s\n%s\n", line, msg, line)
}

func step(n, total int, msg string) {
	fmt.Printf("  [%d/%d] %s... ", n, total, msg)
}

func ok() { fmt.Println("✓") }

// ─── Main ────────────────────────────────────────────────────────────────────

func main() {
	banner("Agent BRD → Code Generator (Pure Go)")

	// 1. Define command line flags
	fileFlag := flag.String("file", "BRD_Endpoint_Migration.docx", "Path to the input BRD document (.docx, .doc, or .pdf)")
	outputFlag := flag.String("output", "generated", "Directory to write generated source code")
	modelFlag := flag.String("model", "gemini-2.5-flash", "Gemini Model name to use")
	apiKeyFlag := flag.String("api-key", "", "Gemini API Key (overrides GEMINI_API_KEY environment variable)")
	flag.Parse()

	// 2. Resolve API key
	apiKey := *apiKeyFlag
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		log.Fatal("Error: Gemini API Key is not set. Please set the GEMINI_API_KEY environment variable or use the -api-key flag.")
	}

	// 3. Extract text from Document using our Go native library
	banner("Step 1 — Document Extraction (Native Go)")
	fmt.Printf("  Reading %s...\n", filepath.Base(*fileFlag))
	
	brdText, err := ExtractText(*fileFlag)
	if err != nil {
		log.Fatalf("Extraction failed: %v", err)
	}
	
	wordCount := len(strings.Fields(brdText))
	fmt.Printf("  Successfully extracted %d words, %d characters.\n", wordCount, len(brdText))

	// Display a short preview
	fmt.Println("\n  BRD Content Preview (first 300 characters):")
	preview := brdText
	if len(preview) > 300 {
		preview = preview[:300] + "..."
	}
	for _, line := range strings.Split(preview, "\n") {
		if strings.TrimSpace(line) != "" {
			fmt.Printf("    %s\n", line)
		}
	}

	// 4. Stage 1: Planning
	banner("Step 2 — Dynamic Project Planning (Gemini)")
	fmt.Println("  Analyzing BRD requirements to identify required files...")
	
	planPrompt := fmt.Sprintf("Here is the Business Requirements Document (BRD):\n\n%s\n\nPlease analyze it and output a plan of files to generate in JSON format.", brdText)
	planJSON, err := callGemini(apiKey, *modelFlag, planSystemPrompt, planPrompt, true)
	if err != nil {
		log.Fatalf("Planning stage failed: %v", err)
	}

	var plan []PlannedFile
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		log.Fatalf("Failed to parse project plan JSON: %v. Raw response was: %s", err, planJSON)
	}

	fmt.Printf("  Identified %d files to generate:\n", len(plan))
	for _, f := range plan {
		fmt.Printf("    • %s (%s)\n", f.Filename, f.Description)
	}

	// 5. Create output directory
	if err := os.MkdirAll(*outputFlag, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	// 6. Stage 2: Code Generation
	banner("Step 3 — AI Code Generation")
	
	for i, f := range plan {
		step(i+1, len(plan), fmt.Sprintf("Generating %s", f.Filename))
		
		genPrompt := fmt.Sprintf("BRD:\n%s\n\nTarget File: %s\nDescription: %s\nInstructions:\n%s\n\nGenerate the complete code for this file.", brdText, f.Filename, f.Description, f.Instructions)
		code, err := callGemini(apiKey, *modelFlag, generateSystemPrompt, genPrompt, false)
		if err != nil {
			fmt.Printf("✗\n  Error generating %s: %v\n", f.Filename, err)
			continue
		}

		cleanCode := stripMarkdownFences(code)
		
		// Write to output path (creating subdirs if necessary)
		outPath := filepath.Join(*outputFlag, f.Filename)
		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			fmt.Printf("✗\n  Error creating directory for %s: %v\n", f.Filename, err)
			continue
		}

		if err := os.WriteFile(outPath, []byte(cleanCode), 0644); err != nil {
			fmt.Printf("✗\n  Error writing %s: %v\n", f.Filename, err)
			continue
		}
		ok()

		// Short pause to avoid rate limiting
		time.Sleep(500 * time.Millisecond)
	}

	// 7. Stage 3: Generate project-level README.md
	banner("Step 4 — Creating Project Documentation")
	step(1, 1, "Generating README.md")

	readmePrompt := fmt.Sprintf("Here is the BRD:\n%s\n\nHere are the generated files:\n", brdText)
	for _, f := range plan {
		readmePrompt += fmt.Sprintf("- %s: %s\n", f.Filename, f.Description)
	}
	readmePrompt += "\nGenerate a comprehensive README.md explaining the architecture, how to run, and how to test the project."

	readmeContent, err := callGemini(apiKey, *modelFlag, generateSystemPrompt, readmePrompt, false)
	if err != nil {
		fmt.Printf("✗\n  Error generating README.md: %v\n", err)
	} else {
		readmePath := filepath.Join(*outputFlag, "README.md")
		if err := os.WriteFile(readmePath, []byte(stripMarkdownFences(readmeContent)), 0644); err != nil {
			fmt.Printf("✗\n  Error writing README.md: %v\n", err)
		} else {
			ok()
		}
	}

	banner("Code Generation Complete")
	fmt.Printf("  All files written to: %s/\n", *outputFlag)
	fmt.Println("  Ready to test and deploy.")
}
