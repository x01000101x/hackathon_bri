package agent

import (
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// resolveVersion resolves the version for the new documentation entry
func resolveVersion(filePath string, customVersion string) (string, error) {
	if customVersion != "" {
		// Clean the custom version string
		version := strings.TrimPrefix(customVersion, "v")
		return "v" + version, nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// If file does not exist, default to v1.0.0
			return "v1.0.0", nil
		}
		return "", err
	}

	// Regex to extract latest version from HTML comment
	re := regexp.MustCompile(`<!-- AGENT_VERSION: v([0-9]+\.[0-9]+\.[0-9]+) -->`)
	matches := re.FindStringSubmatch(string(content))
	if len(matches) < 2 {
		// Fallback if no version tag is found
		return "v1.0.0", nil
	}

	currentVersionStr := matches[1]
	parts := strings.Split(currentVersionStr, ".")
	if len(parts) != 3 {
		return "v1.0.0", nil
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return "v1.0.0", nil
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return "v1.0.0", nil
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return "v1.0.0", nil
	}

	// Auto-increment the patch version
	patch++

	return fmt.Sprintf("v%d.%d.%d", major, minor, patch), nil
}

// UpdateHTMLReport creates or updates the HTML document
func UpdateHTMLReport(filePath, title, version, dateStr, markdownReview string) error {
	var currentHTML string

	content, err := os.ReadFile(filePath)
	if err == nil {
		currentHTML = string(content)
	}

	// 1. Generate the single audit card HTML block
	newCardHTML := generateAuditCardHTML(version, dateStr, markdownReview)

	var finalHTML string
	if currentHTML == "" {
		// Create a brand new HTML report
		finalHTML = getHTMLTemplate(title, version, dateStr, newCardHTML)
	} else {
		// File already exists; parse and prepend/append historical audits
		// Let's replace the latest version tag in the HTML comment
		reVersion := regexp.MustCompile(`<!-- AGENT_VERSION: v[0-9]+\.[0-9]+\.[0-9]+ -->`)
		updatedHTML := reVersion.ReplaceAllString(currentHTML, fmt.Sprintf("<!-- AGENT_VERSION: %s -->", version))

		// Update version badge at top
		reBadge := regexp.MustCompile(`<span class="badge badge-version" id="latest-version-badge">v[0-9]+\.[0-9]+\.[0-9]+</span>`)
		updatedHTML = reBadge.ReplaceAllString(updatedHTML, fmt.Sprintf(`<span class="badge badge-version" id="latest-version-badge">%s</span>`, version))

		// Update last updated date
		reUpdateDate := regexp.MustCompile(`<span id="last-updated-date">[^<]+</span>`)
		updatedHTML = reUpdateDate.ReplaceAllString(updatedHTML, fmt.Sprintf(`<span id="last-updated-date">%s</span>`, dateStr))

		// Prepend the new card to the existing audits container
		// The container has an insert marker: <!-- AUDITS_INSERT_MARKER -->
		// We insert the new audit card right after the marker
		marker := "<!-- AUDITS_INSERT_MARKER -->"
		if strings.Contains(updatedHTML, marker) {
			insertion := marker + "\n" + newCardHTML
			finalHTML = strings.Replace(updatedHTML, marker, insertion, 1)
		} else {
			// If marker is missing, fallback to replacing the audits container inner HTML
			containerStart := `<div class="audits-timeline">`
			if strings.Contains(updatedHTML, containerStart) {
				insertion := containerStart + "\n" + newCardHTML
				finalHTML = strings.Replace(updatedHTML, containerStart, insertion, 1)
			} else {
				// Complete reconstruction if structure was modified
				finalHTML = getHTMLTemplate(title, version, dateStr, newCardHTML)
			}
		}
	}

	return os.WriteFile(filePath, []byte(finalHTML), 0644)
}

// generateAuditCardHTML renders a self-contained card for a specific audit version
func generateAuditCardHTML(version, dateStr, markdownReview string) string {
	encodedMarkdown := base64.StdEncoding.EncodeToString([]byte(markdownReview))
	cardID := "audit-" + strings.ReplaceAll(version, ".", "-")

	return fmt.Sprintf(`    <!-- AUDIT_CARD_%s_START -->
    <div class="audit-card glass-panel" id="%s">
      <div class="card-header" onclick="toggleCard('%s')">
        <div class="header-left">
          <span class="badge badge-card-version">%s</span>
          <span class="audit-timestamp"><i class="fas fa-clock"></i> %s</span>
        </div>
        <button class="toggle-btn">
          <i class="fas fa-chevron-down" id="icon-%s"></i>
        </button>
      </div>
      <div class="card-content active" id="content-%s">
        <div class="markdown-rendered"></div>
        <div class="raw-markdown-base64" style="display:none">%s</div>
      </div>
    </div>
    <!-- AUDIT_CARD_%s_END -->`, version, cardID, cardID, version, dateStr, cardID, cardID, encodedMarkdown, version)
}

// getHTMLTemplate generates the full base HTML template with CSS styling
func getHTMLTemplate(title, version, dateStr, initialCardHTML string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>%s | AI Technical Documentation Agent</title>
  <!-- Google Fonts & FontAwesome -->
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500;700&display=swap" rel="stylesheet">
  <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css">
  <!-- Prism.js Syntax Highlighting Theme -->
  <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/prism/1.29.0/themes/prism-tomorrow.min.css">
  <style>
    :root {
      --bg-gradient: linear-gradient(135deg, #ffffff 0%%, #f4f6fc 100%%);
      --bg-main: #ffffff;
      --glass-bg: #ffffff;
      --glass-border: rgba(8, 87, 195, 0.12);
      --glass-border-hover: rgba(8, 87, 195, 0.25);
      --primary-color: #0857C3;
      --secondary-color: #307FE2;
      --accent-color: #71C5E8;
      --accent-glow: radial-gradient(circle at 50%% -20%%, rgba(8, 87, 195, 0.08) 0%%, transparent 70%%);
      --text-main: #1e293b;
      --text-muted: #64748b;
      --success-green: #10b981;
      --card-shadow: 0 4px 20px rgba(8, 87, 195, 0.05);
      --card-shadow-hover: 0 10px 30px rgba(8, 87, 195, 0.1);
    }

    * {
      box-sizing: border-box;
      margin: 0;
      padding: 0;
    }

    body {
      font-family: 'Inter', sans-serif;
      background: var(--bg-gradient);
      color: var(--text-main);
      min-height: 100vh;
      line-height: 1.6;
      padding-bottom: 50px;
      overflow-x: hidden;
      position: relative;
    }

    /* Glow backdrop element */
    body::before {
      content: "";
      position: absolute;
      top: 0;
      left: 0;
      right: 0;
      height: 600px;
      background: var(--accent-glow);
      pointer-events: none;
      z-index: 0;
    }

    .container {
      max-width: 1000px;
      margin: 0 auto;
      padding: 40px 20px;
      position: relative;
      z-index: 1;
    }

    /* Header Styling */
    header {
      margin-bottom: 50px;
      text-align: center;
    }

    .brand-section {
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 12px;
      margin-bottom: 15px;
    }

    .brand-icon {
      font-size: 2.2rem;
      background: linear-gradient(135deg, var(--primary-color), var(--secondary-color));
      -webkit-background-clip: text;
      -webkit-text-fill-color: transparent;
      animation: pulse-glow 3s infinite alternate;
    }

    h1 {
      font-size: 2.4rem;
      font-weight: 700;
      background: linear-gradient(135deg, #0857C3 0%%, #307FE2 100%%);
      -webkit-background-clip: text;
      -webkit-text-fill-color: transparent;
      letter-spacing: -0.5px;
    }

    .subtitle {
      font-size: 1.1rem;
      color: var(--text-muted);
      margin-top: 5px;
    }

    /* Metadata Panel */
    .metadata-bar {
      display: flex;
      justify-content: center;
      align-items: center;
      gap: 20px;
      margin-top: 25px;
      flex-wrap: wrap;
    }

    .badge {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 6px 14px;
      border-radius: 20px;
      font-size: 0.85rem;
      font-weight: 500;
      text-transform: uppercase;
      letter-spacing: 0.5px;
    }

    .badge-version {
      background: rgba(8, 87, 195, 0.08);
      color: var(--primary-color);
      border: 1px solid rgba(8, 87, 195, 0.15);
    }

    .badge-updated {
      background: rgba(16, 185, 129, 0.08);
      color: var(--success-green);
      border: 1px solid rgba(16, 185, 129, 0.15);
    }

    .badge-card-version {
      background: linear-gradient(135deg, var(--primary-color), var(--secondary-color));
      color: #ffffff;
      font-weight: 600;
      border: none;
    }

    /* Panel Styling */
    .glass-panel {
      background: var(--glass-bg);
      border: 1px solid var(--glass-border);
      border-radius: 16px;
      box-shadow: var(--card-shadow);
      transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
    }

    .glass-panel:hover {
      border-color: var(--glass-border-hover);
      box-shadow: var(--card-shadow-hover);
    }

    /* Timeline Container */
    .audits-timeline {
      display: flex;
      flex-direction: column;
      gap: 30px;
    }

    /* Audit Card Details */
    .audit-card {
      overflow: hidden;
    }

    .card-header {
      padding: 20px 24px;
      display: flex;
      justify-content: space-between;
      align-items: center;
      border-bottom: 1px solid var(--glass-border);
      background: rgba(8, 87, 195, 0.02);
      cursor: pointer;
    }

    .header-left {
      display: flex;
      align-items: center;
      gap: 15px;
    }

    .audit-timestamp {
      font-size: 0.9rem;
      color: var(--text-muted);
      font-weight: 400;
    }

    .toggle-btn {
      background: transparent;
      border: none;
      color: var(--text-muted);
      cursor: pointer;
      font-size: 1.1rem;
      width: 32px;
      height: 32px;
      border-radius: 50%%;
      display: flex;
      align-items: center;
      justify-content: center;
      transition: background-color 0.2s;
    }

    .toggle-btn:hover {
      background-color: rgba(8, 87, 195, 0.05);
      color: var(--primary-color);
    }

    .card-content {
      max-height: 0;
      opacity: 0;
      overflow: hidden;
      transition: max-height 0.4s ease-out, opacity 0.3s ease;
      padding: 0 24px;
    }

    .card-content.active {
      max-height: 25000px; /* High enough threshold for markdown render */
      opacity: 1;
      padding: 24px;
    }

    /* Markdown styling inside card */
    .markdown-rendered {
      font-size: 1rem;
      color: #334155;
    }

    .markdown-rendered h1, 
    .markdown-rendered h2, 
    .markdown-rendered h3,
    .markdown-rendered h4,
    .markdown-rendered h5 {
      font-weight: 600;
      margin-top: 24px;
      margin-bottom: 14px;
      color: #0d1b2a;
      letter-spacing: -0.3px;
    }

    .markdown-rendered h1 { font-size: 1.5rem; border-bottom: 2px solid rgba(8, 87, 195, 0.15); padding-bottom: 6px; color: var(--primary-color); }
    .markdown-rendered h2 { font-size: 1.35rem; border-bottom: 1px solid rgba(8, 87, 195, 0.1); padding-bottom: 4px; color: var(--secondary-color); }
    .markdown-rendered h3 { font-size: 1.2rem; color: #1e293b; }
    .markdown-rendered h4 { font-size: 1.05rem; color: #334155; }

    .markdown-rendered p {
      margin-bottom: 16px;
    }

    .markdown-rendered ul, 
    .markdown-rendered ol {
      margin-bottom: 16px;
      padding-left: 20px;
    }

    .markdown-rendered li {
      margin-bottom: 6px;
    }

    /* Markdown Tables (High-contrast blue themes) */
    .markdown-rendered table {
      width: 100%%;
      border-collapse: collapse;
      margin: 20px 0;
      font-size: 0.92rem;
      box-shadow: 0 2px 12px rgba(8, 87, 195, 0.04);
      border-radius: 8px;
      overflow: hidden;
      border: 1px solid var(--glass-border);
    }

    .markdown-rendered th {
      background-color: var(--primary-color);
      color: #ffffff;
      font-weight: 600;
      text-align: left;
      padding: 12px 16px;
      border: 1px solid rgba(8, 87, 195, 0.15);
    }

    .markdown-rendered td {
      padding: 12px 16px;
      border: 1px solid rgba(8, 87, 195, 0.08);
      color: #334155;
      background-color: #ffffff;
      vertical-align: top;
    }

    .markdown-rendered tr:nth-child(even) td {
      background-color: rgba(48, 127, 226, 0.02);
    }

    .markdown-rendered tr:hover td {
      background-color: rgba(48, 127, 226, 0.05);
    }

    .markdown-rendered code {
      font-family: 'JetBrains Mono', monospace;
      font-size: 0.88rem;
      background: rgba(8, 87, 195, 0.05);
      padding: 3px 6px;
      border-radius: 4px;
      color: #b91c1c;
    }

    .markdown-rendered pre {
      margin-bottom: 20px;
      border-radius: 8px;
      overflow-x: auto;
      border: 1px solid rgba(8, 87, 195, 0.1) !important;
      background: #0f172a !important;
    }

    .markdown-rendered pre code {
      font-family: 'JetBrains Mono', monospace;
      color: #e2e8f0;
      background: transparent;
      padding: 0;
      border-radius: 0;
      font-size: 0.88rem;
    }

    .markdown-rendered blockquote {
      border-left: 4px solid var(--primary-color);
      padding-left: 16px;
      margin: 20px 0;
      color: var(--text-muted);
      background: rgba(8, 87, 195, 0.03);
      padding-top: 10px;
      padding-bottom: 10px;
      border-radius: 0 8px 8px 0;
    }

    .markdown-rendered strong {
      color: #0f172a;
    }

    /* Footer styling */
    footer {
      text-align: center;
      margin-top: 60px;
      font-size: 0.85rem;
      color: var(--text-muted);
    }

    footer a {
      color: var(--primary-color);
      text-decoration: none;
    }

    footer a:hover {
      text-decoration: underline;
    }

    /* Keyframes */
    @keyframes pulse-glow {
      0%% {
        filter: drop-shadow(0 0 2px rgba(8, 87, 195, 0.3));
      }
      100%% {
        filter: drop-shadow(0 0 10px rgba(113, 197, 232, 0.7));
      }
    }

    /* Scrollbar style */
    ::-webkit-scrollbar {
      width: 8px;
      height: 8px;
    }
    ::-webkit-scrollbar-track {
      background: #f1f5f9;
    }
    ::-webkit-scrollbar-thumb {
      background: rgba(8, 87, 195, 0.15);
      border-radius: 4px;
    }
    ::-webkit-scrollbar-thumb:hover {
      background: rgba(8, 87, 195, 0.3);
    }
  </style>
</head>
<body>
  <!-- AGENT_VERSION: %s -->
  <div class="container">
    <header>
      <div class="brand-section">
        <i class="fas fa-brain brand-icon"></i>
        <h1>%s</h1>
      </div>
      <p class="subtitle">AI-Driven Automated Documentation & Structural Review</p>
      
      <div class="metadata-bar">
        <span class="badge badge-version"><i class="fas fa-code-branch"></i> <span id="latest-version-badge">%s</span></span>
        <span class="badge badge-updated"><i class="fas fa-sync-alt"></i> Last Sync: <span id="last-updated-date">%s</span></span>
      </div>
    </header>

    <main class="audits-timeline">
<!-- AUDITS_INSERT_MARKER -->
%s
    </main>

    <footer>
      <p>Generated by <a href="https://github.com/google/generative-ai-go" target="_blank">Google Gemini-AI Agent</a>. Powered by DeepMind & Antigravity.</p>
    </footer>
  </div>

  <!-- Marked.js Markdown Parser via CDN -->
  <script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
  <!-- Prism.js Syntax Highlighting via CDN -->
  <script src="https://cdnjs.cloudflare.com/ajax/libs/prism/1.29.0/components/prism-core.min.js"></script>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/prism/1.29.0/plugins/autoloader/prism-autoloader.min.js"></script>

  <script>
    // Config marked to use standard safe features
    marked.setOptions({
      breaks: true,
      gfm: true
    });

    // Expand / collapse audit cards
    function toggleCard(cardId) {
      const cleanId = cardId.replace('audit-', '');
      const content = document.getElementById('content-audit-' + cleanId) || document.getElementById('content-' + cleanId);
      const icon = document.getElementById('icon-audit-' + cleanId) || document.getElementById('icon-' + cleanId);
      
      if (content && content.classList) {
        if (content.classList.contains('active')) {
          content.classList.remove('active');
          if (icon) icon.className = 'fas fa-chevron-right';
        } else {
          content.classList.add('active');
          if (icon) icon.className = 'fas fa-chevron-down';
        }
      }
    }

    // Process all cards and render markdown content on DOM load
    document.addEventListener('DOMContentLoaded', () => {
      document.querySelectorAll('.card-content').forEach(cardContent => {
        const rawMarkdownTemplate = cardContent.querySelector('.raw-markdown');
        const base64Container = cardContent.querySelector('.raw-markdown-base64');
        const renderContainer = cardContent.querySelector('.markdown-rendered');
        
        let rawMD = "";
        if (base64Container && renderContainer) {
          try {
            // Decode raw markdown safely from Base64 (supports non-ASCII characters natively)
            const b64 = base64Container.textContent.trim();
            rawMD = decodeURIComponent(escape(atob(b64)));
          } catch (e) {
            console.error("Failed to decode base64 markdown:", e);
          }
        } else if (rawMarkdownTemplate && renderContainer) {
          // Fallback to legacy template/script tags if they exist
          rawMD = rawMarkdownTemplate.tagName === 'TEMPLATE' 
            ? rawMarkdownTemplate.innerHTML 
            : rawMarkdownTemplate.textContent;
        }

        if (renderContainer && rawMD) {
          renderContainer.innerHTML = marked.parse(rawMD);
        }
      });
      
      // Load Prism syntax highlighting
      if (window.Prism) {
        Prism.highlightAll();
      }
    });
  </script>
</body>
</html>`, title, version, title, version, dateStr, initialCardHTML)
}
