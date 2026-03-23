package controlplane

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// parsePeriod extracts date range from query parameters.
// Supports: period=1d|7d|30d|90d or from=YYYY-MM-DD&to=YYYY-MM-DD.
// Returns (since, until) as YYYY-MM-DD strings.
func parsePeriod(r *http.Request) (string, string) {
	now := time.Now().UTC()
	today := now.Format("2006-01-02")

	// Custom range takes priority
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from != "" && to != "" {
		return from, to
	}

	period := r.URL.Query().Get("period")
	days := 7 // default
	switch period {
	case "1d":
		days = 1
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	default:
		if period != "" {
			// Try parsing as number of days (e.g. "14d")
			if strings.HasSuffix(period, "d") {
				if d, err := strconv.Atoi(strings.TrimSuffix(period, "d")); err == nil && d > 0 {
					days = d
				}
			}
		}
	}

	since := now.AddDate(0, 0, -days).Format("2006-01-02")
	return since, today
}

// handleReportAuditCSV handles GET /api/v1/reports/audit.csv
func (s *Server) handleReportAuditCSV(w http.ResponseWriter, r *http.Request) {
	since, until := parsePeriod(r)
	department := r.URL.Query().Get("department")

	rows, err := s.store.GetAuditCSVRows(since, until, department)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get audit CSV data")
		http.Error(w, `{"error":"failed to get audit data"}`, http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("audit-%s.csv", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)

	cw := csv.NewWriter(w)
	// Header
	cw.Write([]string{"date", "agent_id", "department", "action", "detector", "count"})

	for _, row := range rows {
		cw.Write([]string{
			row.Date,
			row.AgentID,
			row.Department,
			row.Action,
			row.Detector,
			strconv.Itoa(row.Count),
		})
	}
	cw.Flush()
}

// handleReportSummaryCSV handles GET /api/v1/reports/summary.csv
func (s *Server) handleReportSummaryCSV(w http.ResponseWriter, r *http.Request) {
	since, until := parsePeriod(r)
	department := r.URL.Query().Get("department")

	rows, err := s.store.GetSummaryCSVRows(since, until, department)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get summary CSV data")
		http.Error(w, `{"error":"failed to get summary data"}`, http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("summary-%s.csv", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)

	cw := csv.NewWriter(w)
	cw.Write([]string{"date", "department", "action", "detector", "count"})

	for _, row := range rows {
		cw.Write([]string{
			row.Date,
			row.Department,
			row.Action,
			row.Detector,
			strconv.Itoa(row.Count),
		})
	}
	cw.Flush()
}

// handleReportAgentsCSV handles GET /api/v1/reports/agents.csv
func (s *Server) handleReportAgentsCSV(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents()
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to list agents for CSV")
		http.Error(w, `{"error":"failed to list agents"}`, http.StatusInternalServerError)
		return
	}

	// Resolve department names from labels
	deptMap, _ := s.store.GetDepartmentMap()

	filename := fmt.Sprintf("agents-%s.csv", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)

	cw := csv.NewWriter(w)
	cw.Write([]string{"agent_id", "hostname", "department", "os", "version", "status", "last_heartbeat", "detectors"})

	for _, a := range agents {
		dept := resolveDepartment(a.Labels, deptMap)
		cw.Write([]string{
			a.AgentID,
			a.Hostname,
			dept,
			a.OS,
			a.Version,
			a.Status,
			a.LastHeartbeat.Format(time.RFC3339),
			strconv.Itoa(a.Detectors),
		})
	}
	cw.Flush()
}

// handleReportRiskUsersCSV handles GET /api/v1/reports/risk-users.csv
func (s *Server) handleReportRiskUsersCSV(w http.ResponseWriter, r *http.Request) {
	since, until := parsePeriod(r)

	rows, err := s.store.GetRiskUsersCSVRows(since, until)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to get risk users CSV data")
		http.Error(w, `{"error":"failed to get risk users data"}`, http.StatusInternalServerError)
		return
	}

	// Sort by total violations DESC
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].TotalViolations > rows[j].TotalViolations
	})

	// Resolve department names
	deptMap, _ := s.store.GetDepartmentMap()

	filename := fmt.Sprintf("risk-users-%s.csv", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)

	cw := csv.NewWriter(w)
	cw.Write([]string{"rank", "user_id", "department", "block_count", "warn_count", "total_violations", "risk_score"})

	for i, row := range rows {
		dept := ""
		if row.Department != "" {
			if name, ok := deptMap[row.Department]; ok {
				dept = name
			} else {
				dept = row.Department
			}
		}
		riskScore := float64(row.BlockCount)*3.0 + float64(row.WarnCount)*1.0
		cw.Write([]string{
			strconv.Itoa(i + 1),
			row.UserID,
			dept,
			strconv.Itoa(row.BlockCount),
			strconv.Itoa(row.WarnCount),
			strconv.Itoa(row.TotalViolations),
			fmt.Sprintf("%.1f", riskScore),
		})
	}
	cw.Flush()
}

// resolveDepartment extracts department from agent labels JSON and resolves the name.
func resolveDepartment(labelsJSON string, deptMap map[string]string) string {
	if labelsJSON == "" {
		return ""
	}
	// Simple JSON extraction without importing encoding/json again
	// Labels format: {"department":"keiri","key":"val"}
	// Use strings-based extraction for simplicity
	idx := strings.Index(labelsJSON, `"department":"`)
	if idx < 0 {
		return ""
	}
	start := idx + len(`"department":"`)
	end := strings.Index(labelsJSON[start:], `"`)
	if end < 0 {
		return ""
	}
	deptID := labelsJSON[start : start+end]
	if deptMap != nil {
		if name, ok := deptMap[deptID]; ok {
			return name
		}
	}
	return deptID
}
