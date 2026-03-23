package inspector

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/trustgate/trustgate/internal/config"
	"github.com/trustgate/trustgate/internal/detector"
)

// ImageInspector checks image files for confidential keywords in filenames
// and enforces file size limits. No OCR -- metadata-level inspection only.
type ImageInspector struct {
	keywords     []string
	maxImageSize int64
}

// NewImageInspector creates an ImageInspector from config.
func NewImageInspector(cfg config.ContentInspectionConfig) *ImageInspector {
	maxSize := cfg.MaxImageSize
	if maxSize <= 0 {
		maxSize = 50 * 1024 * 1024 // 50MB default
	}
	return &ImageInspector{
		keywords:     cfg.ImageKeywords,
		maxImageSize: maxSize,
	}
}

// InspectImage checks an image file's filename and size, returning findings.
func (ii *ImageInspector) InspectImage(filename string, fileSize int64) []detector.Finding {
	var findings []detector.Finding

	// Check file size limit
	if fileSize > ii.maxImageSize {
		findings = append(findings, detector.Finding{
			Detector:    "file_inspection",
			Category:    "file_size",
			Severity:    "medium",
			Confidence:  1.0,
			Description: fmt.Sprintf("image file exceeds size limit: %d bytes (max %d)", fileSize, ii.maxImageSize),
			Matched:     filename,
		})
	}

	// Check filename against confidential keywords
	findings = append(findings, ii.checkFilenameKeywords(filename)...)

	return findings
}

// checkFilenameKeywords scans the filename for configured confidential keywords.
func (ii *ImageInspector) checkFilenameKeywords(filename string) []detector.Finding {
	if len(ii.keywords) == 0 {
		return nil
	}

	var findings []detector.Finding
	lowerName := strings.ToLower(filename)
	// Also check the name without extension, with separators replaced
	nameOnly := strings.TrimSuffix(filename, filepath.Ext(filename))
	nameNormalized := strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(nameOnly)
	lowerNormalized := strings.ToLower(nameNormalized)

	for _, kw := range ii.keywords {
		lowerKW := strings.ToLower(kw)
		if strings.Contains(lowerName, lowerKW) || strings.Contains(lowerNormalized, lowerKW) {
			findings = append(findings, detector.Finding{
				Detector:    "confidential",
				Category:    "file_keyword",
				Severity:    "high",
				Confidence:  0.95,
				Description: "confidential keyword in filename: " + kw,
				Matched:     kw,
			})
		}
	}

	return findings
}

// IsImageFile returns true if the file extension indicates an image file.
func IsImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".tiff", ".tif", ".svg", ".ico":
		return true
	default:
		return false
	}
}
