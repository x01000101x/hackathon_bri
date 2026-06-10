package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	geminiAPI   = "https://generativelanguage.googleapis.com/v1beta/models"
	geminiModel = "gemini-2.5-flash"
	maxTokens   = 8192
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

// APIKey is the configured Gemini API key.
var APIKey string

// CallGemini sends a prompt to Gemini with system instructions and returns the text response.
func CallGemini(system, userPrompt string) (string, error) {
	if APIKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY is not configured")
	}

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

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiAPI, geminiModel, APIKey)
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

// StripMarkdownFences removes markdown code blocks if the model outputs them.
func StripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		if strings.HasPrefix(first, "```") {
			lines = lines[1:]
		}
	}
	if len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "```" {
			lines = lines[:len(lines)-1]
		}
	}
	return strings.Join(lines, "\n")
}
