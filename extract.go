package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/lee501/doc"
)

// ExtractText selects the correct extractor based on the file extension
// and extracts plain text from it.
func ExtractText(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".docx":
		return extractDocx(filePath)
	case ".pdf":
		return extractPdf(filePath)
	case ".doc":
		return extractDoc(filePath)
	default:
		return "", fmt.Errorf("unsupported file extension: %s", ext)
	}
}

// extractDocx extracts text and tables from a modern .docx file (XML/ZIP based)
func extractDocx(path string) (string, error) {
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

	return parseDocxXML(rc)
}

// parseDocxXML reads the word/document.xml file and extracts text,
// preserving paragraphs and table structures.
func parseDocxXML(r io.Reader) (string, error) {
	decoder := xml.NewDecoder(r)
	var sb strings.Builder

	inText := false
	inTable := false
	var cellText []string
	var rowCells []string

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		switch se := token.(type) {
		case xml.StartElement:
			switch se.Name.Local {
			case "tbl":
				inTable = true
			case "tr":
				rowCells = nil
			case "tc":
				cellText = nil
			case "t":
				inText = true
			}
		case xml.EndElement:
			switch se.Name.Local {
			case "p":
				if !inTable {
					sb.WriteString("\n\n")
				}
			case "tbl":
				inTable = false
				sb.WriteString("\n")
			case "tr":
				if inTable && len(rowCells) > 0 {
					sb.WriteString(strings.Join(rowCells, " | ") + "\n")
				}
			case "tc":
				if inTable {
					rowCells = append(rowCells, strings.TrimSpace(strings.Join(cellText, "")))
				}
			case "t":
				inText = false
			}
		case xml.CharData:
			if inText {
				val := string(se)
				if inTable {
					cellText = append(cellText, val)
				} else {
					sb.WriteString(val)
				}
			}
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

// extractPdf extracts text from a .pdf file using ledongthuc/pdf
func extractPdf(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	b, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("get plain text from pdf: %w", err)
	}
	buf.ReadFrom(b)
	return buf.String(), nil
}

// extractDoc extracts text from a legacy binary .doc file using lee501/doc
func extractDoc(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open doc: %w", err)
	}
	defer file.Close()

	reader, err := doc.ParseDoc(file)
	if err != nil {
		return "", fmt.Errorf("parse doc: %w", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		return "", fmt.Errorf("read doc content: %w", err)
	}
	return buf.String(), nil
}
