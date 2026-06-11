package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// ReviewClient manages a reusable session with the Google Gemini API
type ReviewClient struct {
	client    *genai.Client
	modelName string
}

// NewReviewClient instantiates a single Gemini client session
func NewReviewClient(ctx context.Context, apiKey, modelName string) (*ReviewClient, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %v", err)
	}

	if modelName == "" {
		modelName = "gemini-1.5-flash"
	}

	return &ReviewClient{
		client:    client,
		modelName: modelName,
	}, nil
}

// Close terminates the underlying client connections
func (r *ReviewClient) Close() error {
	return r.client.Close()
}

// GenerateCodeReview sends the Go diff to Gemini and receives a code quality audit in Markdown formatted as a SOP Deployment
func (r *ReviewClient) GenerateCodeReview(ctx context.Context, goDiff, version string) (string, error) {
	prompt := fmt.Sprintf(`You are an expert Release Manager, Go Developer, and DevOps Engineer.
Analyze the following git diff representing changes introduced in version %s of our application.

Based on the code changes and database queries, generate a comprehensive, deployment-ready Standard Operating Procedure (SOP) Deployment document. You MUST return your response in clean, professional Markdown adhering STRICTLY to the following structure and layout. Do not add any conversational text before or after the markdown.

# SOP Deployment

## 1. Application
1. SenyuM Tenaga Pemasar

## 2. Programming Language
1. Go Language
2. Typescript

## 3. Framework
1. Echo
2. Next Js

## 4. Database
1. PostGreSQL

## PTL (Procedure Task List)
Provide a markdown table listing the deployment activities based on the actual changes in this diff. If there are database/sql changes, list the alter/create/update queries, database names, and table names. If there are backend services modified, list the branch merging and deploy service tasks. Use this layout:
No | Activity | Details / SQL / Links
---|---|---
(List tasks sequentially, e.g., Alter table, Create index, Merge branch, Deploy service. Include Bitbucket/GitHub PR links matching the changes or repository details if inferred, or use placeholder/example links matching the repo name bitbucket.bri.co.id/projects/DGB/repos/... e.g. for umi-ms-product, umi-corner-bff, or umi-corner-microsite if applicable).

## 3. SOP Backup
Provide a markdown table showing the backup steps. Use this layout:
No | Activity | Details
---|---|---
1 | Service | Backup code tersedia dalam bentuk branch release versi sebelumnya yang dapat dilihat history deploymentnya melalui Jenkins atau Helm
2 | Mobile App | Backup code tersedia dalam bentuk branch release versi sebelumnya
3 | Backup Database | Details: Backup Database (contoh menggunakan GUI DBeaver): Connect pada database yang ingin di backup. Klik kanan pada setiap db name yang di backup, pilih "Dump Database". Pilih table yang ingin di backup, pilih "Next". Gunakan spesifik DB name untuk nama file backup, pilih output file backup. Pilih "Start" dan mulai proses backup. <br>DB yang perlu di backup: (Specify DB name and table names affected by changes, e.g. ms-product - products)
4 | Backup file .env existing pada service berikut | (List the affected services, e.g., umi-ms-product, umi-corner-bff, etc.)

## 4. SOP Deployment Program
For each modified service (e.g. umi-corner-bff, umi-corner-microsite, umi-ms-product, or other microservices detected in the diff), provide the step-by-step Jenkins deployment instructions. Format like this:
* **Deploy [service-name]**
  1. Buka jenkins pada halaman Jenkins [service-name] dan login menggunakan credential yang telah diberikan
  2. Pilih **Build with Parameter**
     * Branch : Pilih branch master
     * Datacenter : Pilih data center GTI
     * Deploy_To : Pilih environment Production
     * Cluster : Pilih custer GTI Prd New Cluster
  3. Klik **Build**
  4. Tunggu sampai proses deployment selesai

## SOP Rollback

### 1. Eksekusi Query
Provide the exact rollback SQL statements (e.g., DROP COLUMN, DROP INDEX, delete updates) to revert any database changes introduced in this diff.
If no database changes exist, write: "-- No database changes to rollback"

### 2. Rollback Service
Provide the Helm rollback steps for each service that was deployed. Format like this:
* **ROLLBACK [service-name]**
  1. Masuk ke server master kubernetes on premise
  2. Ketik command: 'helm history [service-name]' untuk mengetahui riwayat deploy dan mendapatkan nomor revisi
  3. Ketik: 'helm rollback [service-name] [revision]'

## Alternate Backup Plan
Provide the git revert-based rollback build steps for each service. Format like this:
* **Deploy [service-name]**
  1. Clone repository [service-name]
  2. Checkout branch master: git checkout master
  3. Revert Commit id: git revert -M [inferred-or-placeholder-commit-id]
  4. Push Origin Master: git push origin master
  5. Buka jenkins pada halaman Jenkins [service-name] dan login menggunakan credential yang telah diberikan
  6. Pilih **Build with Parameter**
     * Branch : Pilih branch master
     * Datacenter : Pilih data center GTI
     * Deploy_To : Pilih environment Production
     * Cluster : Pilih custer GTI Prd New Cluster
  7. Klik **Build**
  8. Tunggu sampai proses deployment selesai

Here is the Git Diff:
"""
%s
"""
`, version, goDiff)

	return r.callGeminiAPI(ctx, prompt)
}

// GenerateQueryReview sends the Go & SQL diff to Gemini and receives a database and query audit in Markdown format with two tables
func (r *ReviewClient) GenerateQueryReview(ctx context.Context, goDiff, sqlDiff, version string) (string, error) {
	prompt := fmt.Sprintf(`You are an expert Database Administrator, GORM specialist, and SQL Performance Tuning Engineer.
Analyze the following git diffs (including Go file modifications and raw SQL migrations) representing database-related changes introduced in version %s.

Perform a thorough Database Schema and GORM Query performance review. You MUST return your response in clean, professional Markdown containing EXACTLY two tables as defined below. Do not add any conversational text before or after the tables.

### Table 1: Features & Queries Audit
Create a markdown table with the following headers:
No | Fitur | Query | Database | index | explanation
---|---|---|---|---|---
- **Fitur**: Name of the features that changed in the code (e.g. Get Product by is active).
- **Query**: SQL query executed or generated by GORM for the feature (e.g. select * from products p where is_show = '0').
- **Database**: Name of the database affected (e.g. ms-product).
- **index**: The index of the table (e.g., Table:products, PRIMARY (id), products_is_show_idx(is_show)).
- **explanation**: Detailed explanation of query structure, efficiency, or tuning.

### Table 2: Database Schema & Script Audit
Create a markdown table with the following headers:
No | Script | Table | Database | Description | Note (optional)
---|---|---|---|---|---
- **Script**: SQL script or GORM auto-migration statement that changed (e.g., ALTER TABLE public.products ADD is_eod varchar(10) NULL).
- **Table**: Name of the table.
- **Database**: Name of the database.
- **Description**: What is done or what changes are made.
- **Note (optional)**: Optional notes.

Here are the Git Diffs:

--- GO DIFF (for GORM models and queries) ---
%s

--- SQL DIFF (for raw migrations and schemas) ---
%s
`, version, goDiff, sqlDiff)

	return r.callGeminiAPI(ctx, prompt)
}

// callGeminiAPI interacts with the official Google Generative AI Go SDK
func (r *ReviewClient) callGeminiAPI(ctx context.Context, prompt string) (string, error) {
	model := r.client.GenerativeModel(r.modelName)
	
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
