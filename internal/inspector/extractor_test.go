package inspector

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/trustgate/trustgate/internal/config"
	"github.com/trustgate/trustgate/internal/detector"
)

func TestExtractDOCX(t *testing.T) {
	// Create a minimal .docx in memory
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Add word/document.xml
	w, _ := zw.Create("word/document.xml")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>田中太郎のメールは tanaka@example.com です。</w:t></w:r></w:p>
    <w:p><w:r><w:t>電話番号は 090-1234-5678 です。</w:t></w:r></w:p>
  </w:body>
</w:document>`))
	zw.Close()

	result, err := ExtractText(buf.Bytes(), "test.docx")
	if err != nil {
		t.Fatalf("ExtractText error: %v", err)
	}
	if result.FileType != "docx" {
		t.Errorf("expected fileType=docx, got %s", result.FileType)
	}
	if !strings.Contains(result.Text, "tanaka@example.com") {
		t.Errorf("expected email in extracted text, got: %s", result.Text)
	}
	if !strings.Contains(result.Text, "090-1234-5678") {
		t.Errorf("expected phone in extracted text, got: %s", result.Text)
	}
}

func TestExtractPPTX(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	w, _ := zw.Create("ppt/slides/slide1.xml")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
       xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld><p:spTree>
    <p:sp><p:txBody><a:p><a:r><a:t>社外秘資料</a:t></a:r></a:p></p:txBody></p:sp>
    <p:sp><p:txBody><a:p><a:r><a:t>売上報告 2026年Q1</a:t></a:r></a:p></p:txBody></p:sp>
  </p:spTree></p:cSld>
</p:sld>`))
	zw.Close()

	result, err := ExtractText(buf.Bytes(), "report.pptx")
	if err != nil {
		t.Fatalf("ExtractText error: %v", err)
	}
	if result.FileType != "pptx" {
		t.Errorf("expected fileType=pptx, got %s", result.FileType)
	}
	if !strings.Contains(result.Text, "社外秘資料") {
		t.Errorf("expected confidential keyword in text, got: %s", result.Text)
	}
	if result.Pages != 1 {
		t.Errorf("expected 1 slide, got %d", result.Pages)
	}
}

func TestExtractImageMetadata(t *testing.T) {
	// Fake image data
	data := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header

	tests := []struct {
		filename string
		wantText string
	}{
		{"機密_決算報告.png", "機密 決算報告"},
		{"screenshot_2026.jpg", "screenshot 2026"},
		{"社外秘_人事評価.webp", "社外秘 人事評価"},
	}

	for _, tt := range tests {
		result, err := ExtractText(data, tt.filename)
		if err != nil {
			t.Fatalf("ExtractText(%s) error: %v", tt.filename, err)
		}
		if result.FileType != "image" {
			t.Errorf("expected fileType=image, got %s", result.FileType)
		}
		if !strings.Contains(result.Text, tt.wantText) {
			t.Errorf("filename=%s: expected text to contain %q, got: %s", tt.filename, tt.wantText, result.Text)
		}
	}
}

func TestExtractUnsupported(t *testing.T) {
	result, err := ExtractText([]byte("data"), "file.zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.FileType != "unknown" {
		t.Errorf("expected unknown, got %s", result.FileType)
	}
	if result.Error == "" {
		t.Error("expected error for unsupported type")
	}
}

func TestExtractXLSX(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Shared strings
	w, _ := zw.Create("xl/sharedStrings.xml")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>社員番号</t></si>
  <si><t>氏名</t></si>
  <si><t>田中太郎</t></si>
</sst>`))

	// Sheet1
	w2, _ := zw.Create("xl/worksheets/sheet1.xml")
	w2.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1">
      <c r="A1" t="s"><v>0</v></c>
      <c r="B1" t="s"><v>1</v></c>
    </row>
    <row r="2">
      <c r="A2"><v>1001</v></c>
      <c r="B2" t="s"><v>2</v></c>
    </row>
  </sheetData>
</worksheet>`))
	zw.Close()

	result, err := ExtractText(buf.Bytes(), "employees.xlsx")
	if err != nil {
		t.Fatalf("ExtractText error: %v", err)
	}
	if result.FileType != "xlsx" {
		t.Errorf("expected fileType=xlsx, got %s", result.FileType)
	}
	if !strings.Contains(result.Text, "社員番号") {
		t.Errorf("expected shared string in text, got: %s", result.Text)
	}
	if !strings.Contains(result.Text, "田中太郎") {
		t.Errorf("expected name in text, got: %s", result.Text)
	}
	if result.Pages != 1 {
		t.Errorf("expected 1 sheet, got %d", result.Pages)
	}
}

// --- ImageInspector tests ---

func TestImageInspectorKeywords(t *testing.T) {
	cfg := config.ContentInspectionConfig{
		ImageKeywords: []string{"機密", "社外秘", "CONFIDENTIAL"},
		MaxImageSize:  50 * 1024 * 1024,
	}
	ii := NewImageInspector(cfg)

	tests := []struct {
		filename  string
		wantMatch bool
	}{
		{"機密_決算報告.png", true},
		{"社外秘_人事評価.jpg", true},
		{"CONFIDENTIAL_report.png", true},
		{"confidential_data.webp", true},   // case-insensitive
		{"screenshot_2026.jpg", false},     // no keyword
		{"vacation_photo.png", false},
	}

	for _, tt := range tests {
		findings := ii.InspectImage(tt.filename, 1024)
		hasKeyword := false
		for _, f := range findings {
			if f.Category == "file_keyword" {
				hasKeyword = true
			}
		}
		if hasKeyword != tt.wantMatch {
			t.Errorf("filename=%s: wantMatch=%v, got=%v (findings=%+v)",
				tt.filename, tt.wantMatch, hasKeyword, findings)
		}
	}
}

func TestImageInspectorSizeLimit(t *testing.T) {
	cfg := config.ContentInspectionConfig{
		MaxImageSize: 1024, // 1KB limit for testing
	}
	ii := NewImageInspector(cfg)

	// Under limit
	findings := ii.InspectImage("small.png", 512)
	for _, f := range findings {
		if f.Category == "file_size" {
			t.Error("unexpected file_size finding for small file")
		}
	}

	// Over limit
	findings = ii.InspectImage("large.png", 2048)
	hasSizeFinding := false
	for _, f := range findings {
		if f.Category == "file_size" {
			hasSizeFinding = true
		}
	}
	if !hasSizeFinding {
		t.Error("expected file_size finding for oversized file")
	}
}

func TestIsImageFile(t *testing.T) {
	imageExts := []string{"test.png", "photo.jpg", "pic.jpeg", "anim.gif", "icon.bmp", "hero.webp"}
	for _, f := range imageExts {
		if !IsImageFile(f) {
			t.Errorf("expected %s to be image", f)
		}
	}
	nonImage := []string{"doc.pdf", "sheet.xlsx", "slide.pptx", "readme.txt"}
	for _, f := range nonImage {
		if IsImageFile(f) {
			t.Errorf("expected %s to NOT be image", f)
		}
	}
}

// --- Queue tests ---

// stubDetector is a minimal detector for testing.
type stubDetector struct {
	findings []detector.Finding
}

func (s *stubDetector) Name() string { return "stub" }
func (s *stubDetector) Detect(input string) []detector.Finding {
	if strings.Contains(input, "secret") {
		return s.findings
	}
	return nil
}

func TestQueueSubmitAndPoll(t *testing.T) {
	cfg := config.ContentInspectionConfig{
		Enabled:     true,
		MaxFileSize: 10 * 1024 * 1024,
		MaxQueue:    2,
		ImageKeywords: []string{"機密"},
	}
	dcfg := config.DetectorConfig{
		PII:       config.PIIConfig{Enabled: false},
		Injection: config.InjectionConfig{Enabled: false},
	}
	registry := detector.NewRegistry(dcfg)
	registry.Register(&stubDetector{
		findings: []detector.Finding{
			{Detector: "stub", Category: "secret", Severity: "high", Confidence: 0.9, Description: "found secret"},
		},
	})

	q := NewQueue(cfg, registry)
	defer q.Stop()

	// Create a minimal docx with "secret" content
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body><w:p><w:r><w:t>This contains secret data.</w:t></w:r></w:p></w:body>
</w:document>`))
	zw.Close()

	job := InspectionJob{
		ID:        "test-job-1",
		Filename:  "secret.docx",
		FileType:  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		FileSize:  int64(buf.Len()),
		UserID:    "test-user",
		CreatedAt: time.Now(),
		Data:      buf.Bytes(),
	}

	q.Submit(job)

	// Poll until complete (with timeout)
	deadline := time.After(5 * time.Second)
	for {
		result := q.GetResult("test-job-1")
		if result == nil {
			t.Fatal("result not found")
		}
		if result.Status == "completed" || result.Status == "error" {
			if result.Status == "error" {
				t.Fatalf("job failed: %s", result.Error)
			}
			if len(result.Findings) == 0 {
				t.Error("expected findings from stub detector")
			}
			if result.Action != "warn" {
				t.Errorf("expected action=warn for high severity, got %s", result.Action)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for job completion, status=%s", result.Status)
		case <-time.After(50 * time.Millisecond):
			// poll again
		}
	}
}

func TestQueueFileTooLarge(t *testing.T) {
	cfg := config.ContentInspectionConfig{
		MaxFileSize: 100, // 100 bytes limit
		MaxQueue:    2,
	}
	dcfg := config.DetectorConfig{}
	registry := detector.NewRegistry(dcfg)
	q := NewQueue(cfg, registry)
	defer q.Stop()

	job := InspectionJob{
		ID:        "big-file",
		Filename:  "huge.pdf",
		FileSize:  1024,
		CreatedAt: time.Now(),
		Data:      make([]byte, 1024),
	}
	q.Submit(job)

	result := q.GetResult("big-file")
	if result == nil {
		t.Fatal("result not found")
	}
	if result.Status != "error" {
		t.Errorf("expected error status, got %s", result.Status)
	}
	if result.Error != "file too large" {
		t.Errorf("expected 'file too large' error, got %s", result.Error)
	}
}

func TestQueueCleanOld(t *testing.T) {
	cfg := config.ContentInspectionConfig{
		MaxFileSize: 10 * 1024 * 1024,
		MaxQueue:    2,
	}
	dcfg := config.DetectorConfig{}
	registry := detector.NewRegistry(dcfg)
	q := NewQueue(cfg, registry)
	defer q.Stop()

	// Manually insert an old result
	old := time.Now().Add(-10 * time.Minute)
	q.mu.Lock()
	q.results["old-job"] = &InspectionResult{
		ID:        "old-job",
		Status:    "completed",
		CreatedAt: old,
	}
	q.results["new-job"] = &InspectionResult{
		ID:        "new-job",
		Status:    "completed",
		CreatedAt: time.Now(),
	}
	q.mu.Unlock()

	q.CleanOld(5 * time.Minute)

	if q.GetResult("old-job") != nil {
		t.Error("expected old-job to be cleaned up")
	}
	if q.GetResult("new-job") == nil {
		t.Error("expected new-job to still exist")
	}
}
