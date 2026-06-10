package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// ─── Gemini API types ───────────────────────────────────────────────────────

const (
	geminiAPI   = "https://generativelanguage.googleapis.com/v1beta/models"
	geminiModel = "gemini-2.5-flash"
	maxTokens   = 8192
	outputDir   = "generated"
)

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiRequest struct {
	SystemInstruction *GeminiContent         `json:"system_instruction,omitempty"`
	Contents          []GeminiContent        `json:"contents"`
	GenerationConfig  GeminiGenerationConfig `json:"generationConfig"`
}

type GeminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens"`
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

// ─── DOCX text extractor ────────────────────────────────────────────────────

// extractDocxText opens a .docx file and extracts human-readable text from
// word/document.xml using a lightweight XML tag stripper.
func extractDocxText(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	var docFile *zip.File
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}
	if docFile == nil {
		return "", fmt.Errorf("word/document.xml not found in docx archive")
	}

	rc, err := docFile.Open()
	if err != nil {
		return "", fmt.Errorf("open document.xml: %w", err)
	}
	defer rc.Close()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("read document.xml: %w", err)
	}

	return parseDocXML(raw), nil
}

// parseDocXML strips XML markup and reconstructs paragraph-separated plain text.
// It respects <w:p> paragraph boundaries and <w:t> text runs.
func parseDocXML(xmlData []byte) string {
	var sb strings.Builder

	// We do a simple state-machine parse to avoid pulling in a full XML library.
	// State: 0=outside tag, 1=inside tag
	inTag := false
	var tagBuf strings.Builder
	var textBuf strings.Builder
	newPara := false

	for _, b := range xmlData {
		ch := rune(b)
		switch {
		case ch == '<':
			inTag = true
			tagBuf.Reset()
		case ch == '>':
			inTag = false
			tag := tagBuf.String()
			// Paragraph boundary
			if tag == "w:p" || tag == "/w:p" || strings.HasPrefix(tag, "w:p ") {
				line := strings.TrimRightFunc(textBuf.String(), unicode.IsSpace)
				if line != "" {
					if newPara {
						sb.WriteString("\n\n")
					}
					sb.WriteString(line)
					newPara = true
				} else if tag == "/w:p" {
					// empty paragraph → blank line
					if newPara {
						sb.WriteString("\n")
					}
				}
				textBuf.Reset()
			}
			// Tab run
			if tag == "w:tab" {
				textBuf.WriteString("\t")
			}
		case inTag:
			tagBuf.WriteRune(ch)
		default:
			textBuf.WriteRune(ch)
		}
	}

	text := sb.String()
	// Collapse runs of 3+ newlines into 2
	re := regexp.MustCompile(`\n{3,}`)
	text = re.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// ─── Gemini API client ──────────────────────────────────────────────────────

func callGemini(apiKey, system, userPrompt string) (string, error) {
	payload := GeminiRequest{
		SystemInstruction: &GeminiContent{
			Role:  "system",
			Parts: []GeminiPart{{Text: system}},
		},
		Contents: []GeminiContent{
			{Role: "user", Parts: []GeminiPart{{Text: userPrompt}}},
		},
		GenerationConfig: GeminiGenerationConfig{
			MaxOutputTokens: maxTokens,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiAPI, geminiModel, apiKey)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
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
		return "", fmt.Errorf("unmarshal response: %w", err)
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

// ─── Code generation steps ──────────────────────────────────────────────────

const systemPrompt = `You are a senior Go backend engineer specializing in gRPC services and financial systems.
Given a Business Requirements Document (BRD) for an endpoint migration, you generate production-quality Go code.
You output ONLY Go source code files — no markdown prose outside code blocks, no explanations outside comments.
Each file should begin with a package declaration and comprehensive doc comments describing its purpose.
Use idiomatic Go: proper error wrapping, context propagation, structured logging, and testable design.`

type GeneratedFile struct {
	Filename    string
	Description string
	Prompt      string
}

func generationSteps(brdText string) []GeneratedFile {
	brdSnippet := brdText
	if len(brdSnippet) > 6000 {
		brdSnippet = brdSnippet[:6000] + "\n\n[... BRD truncated for context ...]"
	}

	return []GeneratedFile{
		{
			Filename:    "transaction.proto",
			Description: "Protobuf schema",
			Prompt: fmt.Sprintf(`Based on the following BRD, generate the complete Protocol Buffers 3 .proto file for the TransactionService.
Include all message definitions (TransactionRequest, TransactionResponse, GetTransactionRequest, StreamStatusRequest, StatusUpdate),
the TxStatus enum, the TransactionService service with all three RPCs (ProcessTransaction, GetTransaction, StreamStatus),
and proper import for google/protobuf/timestamp.proto.
Add field comments describing each field's purpose and constraints from the BRD.

BRD:
%s`, brdSnippet),
		},
		{
			Filename:    "server.go",
			Description: "gRPC server implementation",
			Prompt: fmt.Sprintf(`Based on the following BRD, generate a complete Go file implementing the gRPC TransactionService server.
Requirements from BRD:
- Package: transaction/v2
- Implement ProcessTransaction (unary): validate input, check idempotency cache, process, return TransactionResponse
- Implement GetTransaction (unary): look up transaction by ID, return NOT_FOUND if missing
- Implement StreamStatus (server-stream): stream status updates for a transaction_id
- Use proper gRPC status codes as defined in the BRD error mapping table
- Include idempotency check using an in-memory sync.Map cache (production would use Redis)
- Propagate context deadlines
- Return INVALID_ARGUMENT for: empty transaction_id, zero/negative amount, invalid currency code (not 3 chars), empty account IDs
- Return ALREADY_EXISTS for duplicate transaction_id with different payload
- Structured logging using log/slog
- All public types and functions must have doc comments

BRD:
%s`, brdSnippet),
		},
		{
			Filename:    "interceptors.go",
			Description: "gRPC interceptors (auth, logging, rate limiting)",
			Prompt: fmt.Sprintf(`Based on the following BRD, generate a complete Go file with gRPC interceptors.
Requirements from BRD:
- Package: transaction/v2
- UnaryAuthInterceptor: validates a Bearer JWT token from metadata key "authorization".
  Extract and validate the token format (for this implementation, check it starts with "Bearer " and has 3 dot-separated parts).
  Return UNAUTHENTICATED if missing or malformed.
- UnaryLoggingInterceptor: logs method name, duration, status code, and transaction_id (extracted from request if available via reflection).
  MUST NOT log full payload per BRD security requirement 5.2.
- UnaryRateLimitInterceptor: implements a simple token-bucket per authenticated client (client_id from JWT subject claim).
  Allow 1000 RPM. Return RESOURCE_EXHAUSTED when exceeded.
- UnaryRecoveryInterceptor: catches panics and returns INTERNAL status.
- ChainUnaryInterceptors: chains them in the correct order: Recovery → Auth → RateLimit → Logging.
- Use log/slog for structured logging.

BRD:
%s`, brdSnippet),
		},
		{
			Filename:    "client.go",
			Description: "Go gRPC client SDK v2",
			Prompt: fmt.Sprintf(`Based on the following BRD, generate a complete Go client SDK file for the TransactionService v2 gRPC endpoint.
Requirements:
- Package: txnclient
- TransactionClient struct with a grpc.ClientConn and the generated pb client
- NewTransactionClient(addr string, opts ...grpc.DialOption) (*TransactionClient, error): connects with mTLS if credentials provided
- ProcessTransaction(ctx context.Context, req *pb.TransactionRequest) (*pb.TransactionResponse, error)
- GetTransaction(ctx context.Context, transactionID string) (*pb.TransactionResponse, error)
- StreamStatus(ctx context.Context, transactionID string, handler func(*pb.StatusUpdate)) error: reads from stream and calls handler for each update
- WithMTLS(certFile, keyFile, caFile string) grpc.DialOption helper
- Close() error
- All methods must propagate context and return wrapped errors with transaction_id included
- Add retry logic for UNAVAILABLE status (max 3 retries, exponential backoff starting 100ms)

BRD:
%s`, brdSnippet),
		},
		{
			Filename:    "main.go",
			Description: "Server entrypoint with mTLS and health check",
			Prompt: fmt.Sprintf(`Based on the following BRD, generate a complete Go main.go file that starts the TransactionService gRPC server.
Requirements from BRD:
- Package: main
- Parse config from environment variables: GRPC_PORT (default 50051), TLS_CERT_FILE, TLS_KEY_FILE, TLS_CA_FILE
- If TLS vars are set, configure mTLS (mutual TLS) using tls.RequireAndVerifyClientCert
- Register TransactionService on the gRPC server with the full interceptor chain
- Register grpc health check service (google.golang.org/grpc/health) for load balancer probes  
- Register reflection service for grpcurl debugging (only in non-production; check ENV=production)
- Prometheus metrics server on :9090 /metrics endpoint (use promhttp.Handler)
- Graceful shutdown: listen for SIGTERM/SIGINT, call GracefulStop() with a 30-second timeout
- Structured startup/shutdown logs with slog

BRD:
%s`, brdSnippet),
		},
		{
			Filename:    "server_test.go",
			Description: "Unit tests for the server",
			Prompt: fmt.Sprintf(`Based on the following BRD acceptance criteria, generate a complete Go test file for the TransactionService server.
Test all acceptance criteria from section 7 of the BRD:
- AC-01: TestProcessTransaction_ValidPayload — process a valid transaction, assert SUCCESS status
- AC-02: TestGetTransaction_ExistingID — get an existing transaction, assert correct fields returned
- AC-05: TestProcessTransaction_InvalidPayload — send invalid payloads and assert INVALID_ARGUMENT  
- AC-06: TestProcessTransaction_Idempotent — send same transaction_id twice, assert second returns same cached response
- TestGetTransaction_NotFound — assert NOT_FOUND for unknown ID
- TestProcessTransaction_EmptyCurrency — assert INVALID_ARGUMENT for empty currency_code
- TestProcessTransaction_NegativeAmount — assert INVALID_ARGUMENT for amount <= 0
Use google.golang.org/grpc/status and google.golang.org/grpc/codes for assertions.
Use bufconn (google.golang.org/grpc/test/bufconn) for in-process testing — no actual network needed.
Table-driven tests where appropriate.
Each test must have a clear description comment.

BRD:
%s`, brdSnippet),
		},
	}
}

// ─── File writer ─────────────────────────────────────────────────────────────

func writeFile(dir, filename, content string) error {
	// Strip leading/trailing markdown fences if Claude adds them
	content = stripMarkdownFences(content)

	path := filepath.Join(dir, filename)
	return os.WriteFile(path, []byte(content), 0o644)
}

func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	// Remove opening fence (```go, ```proto, ```)
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

// ─── Banner helpers ─────────────────────────────────────────────────────────

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
	banner("BRD OCR → Go Code Generator")

	// 1. Resolve input file
	docxPath := "BRD_Endpoint_Migration.docx"
	if len(os.Args) > 1 {
		docxPath = os.Args[1]
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY environment variable is not set")
	}

	// 2. Extract text from DOCX
	banner("Step 1 — OCR: Extracting BRD text from DOCX")
	step(1, 1, fmt.Sprintf("Reading %s", filepath.Base(docxPath)))
	brdText, err := extractDocxText(docxPath)
	if err != nil {
		log.Fatalf("Failed to extract DOCX text: %v", err)
	}
	ok()

	wordCount := len(strings.Fields(brdText))
	fmt.Printf("  Extracted %d words, %d characters\n", wordCount, len(brdText))

	// 3. Show a brief preview
	fmt.Println("\n  BRD Preview (first 300 chars):")
	preview := brdText
	if len(preview) > 300 {
		preview = preview[:300] + "..."
	}
	for _, line := range strings.Split(preview, "\n") {
		fmt.Printf("    %s\n", line)
	}

	// 4. Prepare output directory
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		log.Fatalf("Create output dir: %v", err)
	}

	// 5. Generate code files
	banner("Step 2 — AI Code Generation via Gemini API")
	steps := generationSteps(brdText)

	for i, gs := range steps {
		step(i+1, len(steps), fmt.Sprintf("Generating %-42s (%s)", gs.Filename, gs.Description))

		code, err := callGemini(apiKey, systemPrompt, gs.Prompt)
		if err != nil {
			fmt.Printf("✗\n  ERROR: %v\n", err)
			continue
		}

		if err := writeFile(outputDir, gs.Filename, code); err != nil {
			fmt.Printf("✗\n  ERROR writing file: %v\n", err)
			continue
		}
		ok()

		// Small courtesy delay between API calls
		if i < len(steps)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	// 6. Write a summary README
	banner("Step 3 — Writing README")
	step(1, 1, "Generating README.md")
	readme := buildReadme(docxPath, steps)
	if err := os.WriteFile(filepath.Join(outputDir, "README.md"), []byte(readme), 0o644); err != nil {
		fmt.Printf("✗  ERROR: %v\n", err)
	} else {
		ok()
	}

	banner("Done")
	fmt.Printf("  Generated %d files in ./%s/\n\n", len(steps)+1, outputDir)
	for _, gs := range steps {
		fmt.Printf("    • %s/%s\n", outputDir, gs.Filename)
	}
	fmt.Printf("    • %s/README.md\n\n", outputDir)
}

func buildReadme(docxPath string, files []GeneratedFile) string {
	var sb strings.Builder
	sb.WriteString("# BRD-Generated Go Code — Transaction Service v2 (gRPC)\n\n")
	sb.WriteString(fmt.Sprintf("Auto-generated by `brd-ocr-tool` from `%s` on %s.\n\n", filepath.Base(docxPath), time.Now().Format("2006-01-02")))
	sb.WriteString("## Generated Files\n\n")
	sb.WriteString("| File | Description |\n|------|-------------|\n")
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("| `%s` | %s |\n", f.Filename, f.Description))
	}
	sb.WriteString("\n## Quick Start\n\n")
	sb.WriteString("```bash\n# 1. Generate protobuf Go bindings (requires protoc + protoc-gen-go-grpc)\nprotoc --go_out=. --go-grpc_out=. transaction.proto\n\n")
	sb.WriteString("# 2. Get dependencies\ngo mod tidy\n\n")
	sb.WriteString("# 3. Run tests\ngo test ./...\n\n")
	sb.WriteString("# 4. Start server\nGRPC_PORT=50051 go run main.go\n```\n\n")
	sb.WriteString("## Architecture\n\n")
	sb.WriteString("```\n Client → [mTLS] → gRPC Server :50051\n                       │\n                  Interceptor Chain\n                  ┌─── Recovery\n                  ├─── Auth (JWT)\n                  ├─── Rate Limit (token bucket)\n                  └─── Logging (structured)\n                       │\n                  TransactionService\n                  ├── ProcessTransaction  (Unary)\n                  ├── GetTransaction      (Unary)\n                  └── StreamStatus        (Server Stream)\n```\n")
	return sb.String()
}
