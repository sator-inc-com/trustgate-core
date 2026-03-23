package controlplane

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/go-pdf/fpdf"
)

// handleReportSummaryPDF generates a PDF executive summary report.
// GET /api/v1/reports/summary.pdf?period=30d&department=admin
// GET /ui/reports/summary.pdf?period=30d
func (s *Server) handleReportSummaryPDF(w http.ResponseWriter, r *http.Request) {
	since, until := parsePeriod(r)
	department := r.URL.Query().Get("department")

	// Gather data
	summaryRows, err := s.store.GetSummaryCSVRows(since, until, department)
	if err != nil {
		http.Error(w, "failed to get summary data", http.StatusInternalServerError)
		return
	}

	riskUsers, err := s.store.GetRiskUsersCSVRows(since, until)
	if err != nil {
		http.Error(w, "failed to get risk users", http.StatusInternalServerError)
		return
	}

	agentTotal, agentActive, err := s.store.CountAgents()
	if err != nil {
		agentTotal, agentActive = 0, 0
	}
	_ = agentTotal

	// Aggregate totals
	var totalAllow, totalBlock, totalWarn int
	deptCounts := make(map[string]map[string]int) // dept -> action -> count
	detectorCounts := make(map[string]int)

	for _, row := range summaryRows {
		switch row.Action {
		case "allow":
			totalAllow += row.Count
		case "block":
			totalBlock += row.Count
		case "warn":
			totalWarn += row.Count
		}
		if _, ok := deptCounts[row.Department]; !ok {
			deptCounts[row.Department] = make(map[string]int)
		}
		deptCounts[row.Department][row.Action] += row.Count
		if row.Detector != "" {
			detectorCounts[row.Detector] += row.Count
		}
	}
	totalInspections := totalAllow + totalBlock + totalWarn

	// Generate PDF
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()

	// Header
	pdf.SetFont("Helvetica", "B", 20)
	pdf.Cell(0, 12, "TrustGate AI Usage Report")
	pdf.Ln(14)

	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(100, 100, 100)
	deptLabel := "All departments"
	if department != "" {
		deptLabel = "Department: " + department
	}
	pdf.Cell(0, 6, fmt.Sprintf("Period: %s - %s  |  %s  |  Generated: %s",
		since, until, deptLabel, time.Now().Format("2006-01-02 15:04")))
	pdf.Ln(12)

	// KPI Summary
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFont("Helvetica", "B", 14)
	pdf.Cell(0, 10, "Executive Summary")
	pdf.Ln(12)

	blockRate := float64(0)
	if totalInspections > 0 {
		blockRate = float64(totalBlock) / float64(totalInspections) * 100
	}

	kpiData := [][]string{
		{"Total Inspections", fmt.Sprintf("%d", totalInspections)},
		{"Blocked", fmt.Sprintf("%d (%.1f%%)", totalBlock, blockRate)},
		{"Warned", fmt.Sprintf("%d", totalWarn)},
		{"Allowed", fmt.Sprintf("%d", totalAllow)},
		{"Active Agents", fmt.Sprintf("%d", agentActive)},
	}

	pdf.SetFont("Helvetica", "", 11)
	for _, row := range kpiData {
		pdf.SetFont("Helvetica", "B", 11)
		pdf.CellFormat(60, 8, row[0], "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 11)
		pdf.CellFormat(0, 8, row[1], "", 1, "L", false, 0, "")
	}
	pdf.Ln(8)

	// Detection Breakdown
	if len(detectorCounts) > 0 {
		pdf.SetFont("Helvetica", "B", 14)
		pdf.Cell(0, 10, "Detection Breakdown")
		pdf.Ln(12)

		// Table header
		pdf.SetFont("Helvetica", "B", 10)
		pdf.SetFillColor(240, 240, 240)
		pdf.CellFormat(80, 7, "Detector", "1", 0, "L", true, 0, "")
		pdf.CellFormat(40, 7, "Count", "1", 0, "R", true, 0, "")
		pdf.CellFormat(40, 7, "Percentage", "1", 1, "R", true, 0, "")

		// Sort by count desc
		type detEntry struct {
			Name  string
			Count int
		}
		var detEntries []detEntry
		detTotal := 0
		for name, count := range detectorCounts {
			detEntries = append(detEntries, detEntry{name, count})
			detTotal += count
		}
		sort.Slice(detEntries, func(i, j int) bool {
			return detEntries[i].Count > detEntries[j].Count
		})

		pdf.SetFont("Helvetica", "", 10)
		for _, entry := range detEntries {
			pct := float64(0)
			if detTotal > 0 {
				pct = float64(entry.Count) / float64(detTotal) * 100
			}
			pdf.CellFormat(80, 7, entry.Name, "1", 0, "L", false, 0, "")
			pdf.CellFormat(40, 7, fmt.Sprintf("%d", entry.Count), "1", 0, "R", false, 0, "")
			pdf.CellFormat(40, 7, fmt.Sprintf("%.1f%%", pct), "1", 1, "R", false, 0, "")
		}
		pdf.Ln(8)
	}

	// Department Breakdown
	if len(deptCounts) > 0 {
		pdf.SetFont("Helvetica", "B", 14)
		pdf.Cell(0, 10, "Department Breakdown")
		pdf.Ln(12)

		pdf.SetFont("Helvetica", "B", 10)
		pdf.SetFillColor(240, 240, 240)
		pdf.CellFormat(50, 7, "Department", "1", 0, "L", true, 0, "")
		pdf.CellFormat(35, 7, "Block", "1", 0, "R", true, 0, "")
		pdf.CellFormat(35, 7, "Warn", "1", 0, "R", true, 0, "")
		pdf.CellFormat(35, 7, "Allow", "1", 0, "R", true, 0, "")
		pdf.CellFormat(35, 7, "Total", "1", 1, "R", true, 0, "")

		pdf.SetFont("Helvetica", "", 10)
		for dept, actions := range deptCounts {
			deptName := dept
			if deptName == "" {
				deptName = "(unassigned)"
			}
			block := actions["block"]
			warn := actions["warn"]
			allow := actions["allow"]
			total := block + warn + allow
			pdf.CellFormat(50, 7, deptName, "1", 0, "L", false, 0, "")
			pdf.CellFormat(35, 7, fmt.Sprintf("%d", block), "1", 0, "R", false, 0, "")
			pdf.CellFormat(35, 7, fmt.Sprintf("%d", warn), "1", 0, "R", false, 0, "")
			pdf.CellFormat(35, 7, fmt.Sprintf("%d", allow), "1", 0, "R", false, 0, "")
			pdf.CellFormat(35, 7, fmt.Sprintf("%d", total), "1", 1, "R", false, 0, "")
		}
		pdf.Ln(8)
	}

	// Risk Users (top 10)
	if len(riskUsers) > 0 {
		pdf.SetFont("Helvetica", "B", 14)
		pdf.Cell(0, 10, "Highest Risk Users")
		pdf.Ln(12)

		pdf.SetFont("Helvetica", "B", 10)
		pdf.SetFillColor(240, 240, 240)
		pdf.CellFormat(10, 7, "#", "1", 0, "C", true, 0, "")
		pdf.CellFormat(50, 7, "User", "1", 0, "L", true, 0, "")
		pdf.CellFormat(40, 7, "Department", "1", 0, "L", true, 0, "")
		pdf.CellFormat(30, 7, "Block", "1", 0, "R", true, 0, "")
		pdf.CellFormat(30, 7, "Warn", "1", 0, "R", true, 0, "")
		pdf.CellFormat(30, 7, "Total", "1", 1, "R", true, 0, "")

		pdf.SetFont("Helvetica", "", 10)
		limit := 10
		if len(riskUsers) < limit {
			limit = len(riskUsers)
		}
		for i, u := range riskUsers[:limit] {
			dept := u.Department
			if dept == "" {
				dept = "-"
			}
			pdf.CellFormat(10, 7, fmt.Sprintf("%d", i+1), "1", 0, "C", false, 0, "")
			pdf.CellFormat(50, 7, u.UserID, "1", 0, "L", false, 0, "")
			pdf.CellFormat(40, 7, dept, "1", 0, "L", false, 0, "")
			pdf.CellFormat(30, 7, fmt.Sprintf("%d", u.BlockCount), "1", 0, "R", false, 0, "")
			pdf.CellFormat(30, 7, fmt.Sprintf("%d", u.WarnCount), "1", 0, "R", false, 0, "")
			pdf.CellFormat(30, 7, fmt.Sprintf("%d", u.TotalViolations), "1", 1, "R", false, 0, "")
		}
		pdf.Ln(8)
	}

	// Footer
	pdf.SetFont("Helvetica", "I", 8)
	pdf.SetTextColor(150, 150, 150)
	pdf.Cell(0, 6, "Generated by TrustGate Control Plane. Confidential.")

	// Output
	filename := fmt.Sprintf("trustgate-report-%s-to-%s.pdf", since, until)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	if err := pdf.Output(w); err != nil {
		s.logger.Error().Err(err).Msg("failed to generate PDF report")
	}
}
