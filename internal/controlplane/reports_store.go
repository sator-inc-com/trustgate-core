package controlplane

import (
	"fmt"
)

// AuditCSVRow represents a single row in the audit CSV export.
type AuditCSVRow struct {
	Date       string
	AgentID    string
	Department string
	Action     string
	Detector   string
	Count      int
}

// SummaryCSVRow represents a single row in the summary CSV export.
type SummaryCSVRow struct {
	Date       string
	Department string
	Action     string
	Detector   string
	Count      int
}

// RiskUserCSVRow represents a single row in the risk users CSV export.
type RiskUserCSVRow struct {
	UserID          string
	Department      string
	BlockCount      int
	WarnCount       int
	TotalViolations int
}

// GetAuditCSVRows returns audit_daily rows for CSV export, optionally filtered by department.
// Department filtering works by looking up agents in the given department and filtering by agent_id.
func (s *Store) GetAuditCSVRows(since, until, department string) ([]AuditCSVRow, error) {
	// Build agent -> department map for enrichment
	agentDeptMap, err := s.getAgentDepartmentMap()
	if err != nil {
		return nil, fmt.Errorf("get agent department map: %w", err)
	}

	query := `
		SELECT date, agent_id, action, detector, SUM(count)
		FROM audit_daily
		WHERE date >= ? AND date <= ?
		GROUP BY date, agent_id, action, detector
		ORDER BY date DESC, agent_id, action`

	rows, err := s.db.Query(query, since, until)
	if err != nil {
		return nil, fmt.Errorf("query audit CSV: %w", err)
	}
	defer rows.Close()

	var results []AuditCSVRow
	for rows.Next() {
		var r AuditCSVRow
		if err := rows.Scan(&r.Date, &r.AgentID, &r.Action, &r.Detector, &r.Count); err != nil {
			return nil, fmt.Errorf("scan audit CSV row: %w", err)
		}
		r.Department = agentDeptMap[r.AgentID]

		// Filter by department if specified
		if department != "" && r.Department != department {
			continue
		}

		results = append(results, r)
	}
	return results, rows.Err()
}

// GetSummaryCSVRows returns aggregated daily counts grouped by department, action, detector.
func (s *Store) GetSummaryCSVRows(since, until, department string) ([]SummaryCSVRow, error) {
	agentDeptMap, err := s.getAgentDepartmentMap()
	if err != nil {
		return nil, fmt.Errorf("get agent department map: %w", err)
	}

	query := `
		SELECT date, agent_id, action, detector, SUM(count)
		FROM audit_daily
		WHERE date >= ? AND date <= ?
		GROUP BY date, agent_id, action, detector
		ORDER BY date DESC`

	rows, err := s.db.Query(query, since, until)
	if err != nil {
		return nil, fmt.Errorf("query summary CSV: %w", err)
	}
	defer rows.Close()

	// Aggregate by (date, department, action, detector)
	type key struct {
		Date, Dept, Action, Detector string
	}
	agg := make(map[key]int)
	var orderedKeys []key

	for rows.Next() {
		var date, agentID, action, detector string
		var count int
		if err := rows.Scan(&date, &agentID, &action, &detector, &count); err != nil {
			return nil, fmt.Errorf("scan summary CSV row: %w", err)
		}
		dept := agentDeptMap[agentID]
		if department != "" && dept != department {
			continue
		}
		k := key{date, dept, action, detector}
		if _, exists := agg[k]; !exists {
			orderedKeys = append(orderedKeys, k)
		}
		agg[k] += count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var results []SummaryCSVRow
	for _, k := range orderedKeys {
		results = append(results, SummaryCSVRow{
			Date:       k.Date,
			Department: k.Dept,
			Action:     k.Action,
			Detector:   k.Detector,
			Count:      agg[k],
		})
	}
	return results, nil
}

// GetRiskUsersCSVRows returns per-user violation counts for the risk users CSV export.
func (s *Store) GetRiskUsersCSVRows(since, until string) ([]RiskUserCSVRow, error) {
	query := `
		SELECT user_id, action, SUM(count)
		FROM audit_user_daily
		WHERE date >= ? AND date <= ? AND action IN ('block', 'warn')
		GROUP BY user_id, action
		ORDER BY SUM(count) DESC`

	rows, err := s.db.Query(query, since, until)
	if err != nil {
		return nil, fmt.Errorf("query risk users CSV: %w", err)
	}
	defer rows.Close()

	// Aggregate per user
	type userCounts struct {
		BlockCount int
		WarnCount  int
	}
	userMap := make(map[string]*userCounts)
	var userOrder []string

	for rows.Next() {
		var userID, action string
		var count int
		if err := rows.Scan(&userID, &action, &count); err != nil {
			return nil, fmt.Errorf("scan risk user row: %w", err)
		}
		uc, exists := userMap[userID]
		if !exists {
			uc = &userCounts{}
			userMap[userID] = uc
			userOrder = append(userOrder, userID)
		}
		switch action {
		case "block":
			uc.BlockCount += count
		case "warn":
			uc.WarnCount += count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Try to resolve user -> department (best effort from audit_user_daily join with audit_daily)
	// Since audit_user_daily has agent_id, we can look up department from agent labels
	userDeptMap := s.resolveUserDepartments(since, until)

	var results []RiskUserCSVRow
	for _, uid := range userOrder {
		uc := userMap[uid]
		results = append(results, RiskUserCSVRow{
			UserID:          uid,
			Department:      userDeptMap[uid],
			BlockCount:      uc.BlockCount,
			WarnCount:       uc.WarnCount,
			TotalViolations: uc.BlockCount + uc.WarnCount,
		})
	}
	return results, nil
}

// getAgentDepartmentMap returns a map of agent_id -> department_id by parsing agent labels.
func (s *Store) getAgentDepartmentMap() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT agent_id, labels FROM agents WHERE labels != ''`)
	if err != nil {
		return nil, fmt.Errorf("query agent labels: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var agentID, labels string
		if err := rows.Scan(&agentID, &labels); err != nil {
			return nil, fmt.Errorf("scan agent labels: %w", err)
		}
		dept := extractDeptFromLabels(labels)
		if dept != "" {
			result[agentID] = dept
		}
	}
	return result, rows.Err()
}

// resolveUserDepartments attempts to map user_id -> department_id
// by finding which agent(s) reported activity for that user.
func (s *Store) resolveUserDepartments(since, until string) map[string]string {
	result := make(map[string]string)

	agentDeptMap, err := s.getAgentDepartmentMap()
	if err != nil {
		return result
	}

	rows, err := s.db.Query(`
		SELECT DISTINCT user_id, agent_id
		FROM audit_user_daily
		WHERE date >= ? AND date <= ?`, since, until)
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var userID, agentID string
		if err := rows.Scan(&userID, &agentID); err != nil {
			continue
		}
		if dept, ok := agentDeptMap[agentID]; ok {
			result[userID] = dept
		}
	}
	return result
}

// extractDeptFromLabels extracts the department value from a JSON labels string.
func extractDeptFromLabels(labels string) string {
	// Labels format: {"department":"keiri","key":"val"}
	const prefix = `"department":"`
	idx := len(labels)
	for i := 0; i <= len(labels)-len(prefix); i++ {
		if labels[i:i+len(prefix)] == prefix {
			idx = i + len(prefix)
			break
		}
	}
	if idx >= len(labels) {
		return ""
	}
	end := idx
	for end < len(labels) && labels[end] != '"' {
		end++
	}
	if end >= len(labels) {
		return ""
	}
	return labels[idx:end]
}
