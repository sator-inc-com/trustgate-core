package inspector

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// ExtractResult holds the extracted text and metadata from a file.
type ExtractResult struct {
	Text     string // extracted text content
	FileType string // detected file type (pdf, docx, xlsx, pptx, image)
	Pages    int    // number of pages/slides/sheets (0 if unknown)
	Error    string // extraction error, if any
}

// ExtractText extracts text from a file based on its content type and filename.
// Supports: .docx, .xlsx, .pptx, .pdf (text layer), images (metadata only).
func ExtractText(data []byte, filename string) (*ExtractResult, error) {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".docx":
		return extractDOCX(data)
	case ".xlsx":
		return extractXLSX(data)
	case ".pptx":
		return extractPPTX(data)
	case ".pdf":
		return extractPDF(data)
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".tiff", ".tif":
		return extractImageMetadata(data, filename)
	default:
		return &ExtractResult{
			FileType: "unknown",
			Error:    fmt.Sprintf("unsupported file type: %s", ext),
		}, nil
	}
}

// extractDOCX extracts text from a .docx file (ZIP → word/document.xml → <w:t> tags).
func extractDOCX(data []byte) (*ExtractResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open docx zip: %w", err)
	}

	var text strings.Builder
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open document.xml: %w", err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read document.xml: %w", err)
		}
		extractXMLText(content, &text, "t")
		break
	}

	return &ExtractResult{
		Text:     text.String(),
		FileType: "docx",
		Pages:    1, // docx doesn't easily expose page count without rendering
	}, nil
}

// extractXLSX extracts text from a .xlsx file.
// Reads shared strings + inline strings from sheet cells.
func extractXLSX(data []byte) (*ExtractResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open xlsx zip: %w", err)
	}

	// Step 1: Read shared strings
	sharedStrings := readSharedStrings(zr)

	// Step 2: Read all sheets
	var text strings.Builder
	sheetCount := 0
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "xl/worksheets/sheet") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		sheetCount++
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		extractSheetText(content, sharedStrings, &text)
	}

	return &ExtractResult{
		Text:     text.String(),
		FileType: "xlsx",
		Pages:    sheetCount,
	}, nil
}

// readSharedStrings reads xl/sharedStrings.xml and returns the string table.
func readSharedStrings(zr *zip.Reader) []string {
	for _, f := range zr.File {
		if f.Name != "xl/sharedStrings.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil
		}

		var result []string
		decoder := xml.NewDecoder(bytes.NewReader(content))
		var inSI, inT bool
		var currentStr strings.Builder

		for {
			tok, err := decoder.Token()
			if err != nil {
				break
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "si" {
					inSI = true
					currentStr.Reset()
				} else if t.Name.Local == "t" && inSI {
					inT = true
				}
			case xml.CharData:
				if inT {
					currentStr.Write(t)
				}
			case xml.EndElement:
				if t.Name.Local == "t" {
					inT = false
				} else if t.Name.Local == "si" {
					result = append(result, currentStr.String())
					inSI = false
				}
			}
		}
		return result
	}
	return nil
}

// extractSheetText extracts cell values from a sheet XML.
func extractSheetText(content []byte, sharedStrings []string, out *strings.Builder) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var cellType string
	var inV bool

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "c" {
				cellType = ""
				for _, a := range t.Attr {
					if a.Name.Local == "t" {
						cellType = a.Value
					}
				}
			} else if t.Name.Local == "v" {
				inV = true
			}
		case xml.CharData:
			if inV {
				val := string(t)
				if cellType == "s" && sharedStrings != nil {
					// Shared string reference
					idx := 0
					fmt.Sscanf(val, "%d", &idx)
					if idx < len(sharedStrings) {
						out.WriteString(sharedStrings[idx])
					}
				} else {
					out.WriteString(val)
				}
				out.WriteString(" ")
			}
		case xml.EndElement:
			if t.Name.Local == "v" {
				inV = false
			} else if t.Name.Local == "row" {
				out.WriteString("\n")
			}
		}
	}
}

// extractPPTX extracts text from a .pptx file (ZIP → ppt/slides/slide*.xml).
func extractPPTX(data []byte) (*ExtractResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open pptx zip: %w", err)
	}

	var text strings.Builder
	slideCount := 0
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "ppt/slides/slide") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		slideCount++
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		extractXMLText(content, &text, "t")
		text.WriteString("\n")
	}

	return &ExtractResult{
		Text:     text.String(),
		FileType: "pptx",
		Pages:    slideCount,
	}, nil
}

// extractPDF extracts text from a PDF's text layer.
// This is a basic implementation that handles simple text streams.
// For complex PDFs, Phase 2 will add a proper PDF library.
func extractPDF(data []byte) (*ExtractResult, error) {
	// Basic PDF text extraction: find text between BT/ET markers in streams
	content := string(data)
	var text strings.Builder

	// Look for text objects: BT ... Tj/TJ ... ET
	// This handles simple PDFs. Complex ones need a proper parser.
	inText := false
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "BT" {
			inText = true
			continue
		}
		if trimmed == "ET" {
			inText = false
			text.WriteString("\n")
			continue
		}
		if inText {
			// Extract text from Tj operator: (text) Tj
			extracted := extractPDFTextOperators(trimmed)
			if extracted != "" {
				text.WriteString(extracted)
				text.WriteString(" ")
			}
		}
	}

	result := text.String()
	if len(strings.TrimSpace(result)) == 0 {
		return &ExtractResult{
			Text:     "",
			FileType: "pdf",
			Error:    "no text layer found (scanned PDF requires OCR — Phase 2)",
		}, nil
	}

	return &ExtractResult{
		Text:     result,
		FileType: "pdf",
	}, nil
}

// extractPDFTextOperators extracts text from PDF text operators (Tj, TJ, ').
func extractPDFTextOperators(line string) string {
	var result strings.Builder

	// Handle (text) Tj
	for i := 0; i < len(line); i++ {
		if line[i] == '(' {
			depth := 1
			start := i + 1
			i++
			for i < len(line) && depth > 0 {
				if line[i] == '(' && (i == 0 || line[i-1] != '\\') {
					depth++
				} else if line[i] == ')' && (i == 0 || line[i-1] != '\\') {
					depth--
				}
				i++
			}
			if depth == 0 {
				text := line[start : i-1]
				// Unescape common PDF escapes
				text = strings.ReplaceAll(text, "\\(", "(")
				text = strings.ReplaceAll(text, "\\)", ")")
				text = strings.ReplaceAll(text, "\\\\", "\\")
				result.WriteString(text)
			}
		}
	}

	return result.String()
}

// extractImageMetadata checks image filename for confidential keywords.
// No OCR — just metadata-level inspection.
func extractImageMetadata(data []byte, filename string) (*ExtractResult, error) {
	// Build text from filename for keyword detection
	var text strings.Builder
	text.WriteString("[File: ")
	text.WriteString(filename)
	text.WriteString(fmt.Sprintf(" (%d bytes)]", len(data)))

	// Extract text from filename (remove extension, replace separators with spaces)
	nameOnly := strings.TrimSuffix(filename, filepath.Ext(filename))
	nameOnly = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(nameOnly)
	text.WriteString("\n")
	text.WriteString(nameOnly)

	return &ExtractResult{
		Text:     text.String(),
		FileType: "image",
	}, nil
}

// extractXMLText extracts text content from XML elements with the given local name.
func extractXMLText(content []byte, out *strings.Builder, localName string) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	var inTarget bool

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == localName {
				inTarget = true
			}
		case xml.CharData:
			if inTarget {
				out.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == localName {
				inTarget = false
				out.WriteString(" ")
			}
		}
	}
}
