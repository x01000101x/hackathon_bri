package agent

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/fatih/color"
)

const (
	outputDir = "generated"
)

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

const systemPrompt = `You are a senior Go backend engineer specializing in gRPC services and financial systems.
Given a Business Requirements Document (BRD) for an endpoint migration, you generate production-quality Go code.
You output ONLY Go source code files — no markdown prose outside code blocks, no explanations outside comments.
Each file should begin with a package declaration and comprehensive doc comments describing its purpose.
Use idiomatic Go: proper error wrapping, context propagation, structured logging, and testable design.`

// RunBRDGen runs the OCR DOCX parsing and calls Gemini API to write out generated code files.
func RunBRDGen(docxPath string) error {
	color.Cyan("=== BRD OCR → Go Code Generator ===")

	// 1. Check if the file exists
	if _, err := os.Stat(docxPath); os.IsNotExist(err) {
		return fmt.Errorf("docx file not found: %s", docxPath)
	}

	// 2. Extract text from DOCX
	color.Yellow("\nStep 1 — OCR: Extracting BRD text from DOCX")
	fmt.Printf("  Reading %s... ", filepath.Base(docxPath))
	brdText, err := extractDocxText(docxPath)
	if err != nil {
		return fmt.Errorf("failed to extract DOCX text: %v", err)
	}
	color.Green("✓")

	wordCount := len(strings.Fields(brdText))
	fmt.Printf("  Extracted %d words, %d characters\n", wordCount, len(brdText))

	// 3. Preview
	fmt.Println("\n  BRD Preview (first 200 chars):")
	preview := brdText
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	for _, line := range strings.Split(preview, "\n") {
		if strings.TrimSpace(line) != "" {
			color.White("    %s", line)
		}
	}

	// 4. Prepare output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %v", err)
	}

	// 5. Generate code files
	color.Yellow("\nStep 2 — AI Code Generation via Gemini API")
	steps := generationSteps(brdText)

	for i, gs := range steps {
		fmt.Printf("  [%d/%d] Generating %-42s (%s)... ", i+1, len(steps), gs.Filename, gs.Description)

		code, err := CallGemini(systemPrompt, gs.Prompt)
		if err != nil {
			color.Red("✗")
			fmt.Printf("  ERROR: %v\n", err)
			continue
		}

		cleanCode := StripMarkdownFences(code)
		path := filepath.Join(outputDir, gs.Filename)
		if err := os.WriteFile(path, []byte(cleanCode), 0644); err != nil {
			color.Red("✗")
			fmt.Printf("  ERROR writing file: %v\n", err)
			continue
		}
		color.Green("✓")

		if i < len(steps)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	// 6. Write README
	color.Yellow("\nStep 3 — Writing README")
	fmt.Print("  Generating README.md... ")
	readme := buildReadme(docxPath, steps)
	if err := os.WriteFile(filepath.Join(outputDir, "README.md"), []byte(readme), 0644); err != nil {
		color.Red("✗")
		fmt.Printf("  ERROR: %v\n", err)
	} else {
		color.Green("✓")
	}

	color.Cyan("\nDone")
	fmt.Printf("  Generated %d files in ./%s/\n\n", len(steps)+1, outputDir)
	return nil
}

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

func parseDocXML(xmlData []byte) string {
	var sb strings.Builder
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
			if tag == "w:p" || tag == "/w:p" || strings.HasPrefix(tag, "w:p ") {
				line := strings.TrimRightFunc(textBuf.String(), unicode.IsSpace)
				if line != "" {
					if newPara {
						sb.WriteString("\n\n")
					}
					sb.WriteString(line)
					newPara = true
				} else if tag == "/w:p" {
					if newPara {
						sb.WriteString("\n")
					}
				}
				textBuf.Reset()
			}
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
	re := regexp.MustCompile(`\n{3,}`)
	text = re.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
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
