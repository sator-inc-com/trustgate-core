package controlplane

import (
	"fmt"
	"time"
)

// StatsPushRequest is the JSON body for POST /api/v1/audit/stats.
type StatsPushRequest struct {
	AgentID  string                       `json:"agent_id"`
	Period   string                       `json:"period"` // YYYY-MM-DD
	ByAction map[string]map[string]int    `json:"by_action"` // action -> detector -> count
	ByUser   []UserActionCount            `json:"by_user"`
	ByPolicy []PolicyTriggerCount         `json:"by_policy"`
}

// UserActionCount represents a user's action count.
type UserActionCount struct {
	UserID string `json:"user_id"`
	Action string `json:"action"`
	Count  int    `json:"count"`
}

// PolicyTriggerCount represents a policy's trigger and false positive counts.
type PolicyTriggerCount struct {
	PolicyName         string `json:"policy_name"`
	TriggerCount       int    `json:"trigger_count"`
	FalsePositiveCount int    `json:"false_positive_count"`
}

// KPISummary holds aggregated KPI data for the dashboard.
type KPISummary struct {
	TotalRequests int     `json:"total_requests"`
	BlockCount    int     `json:"block_count"`
	WarnCount     int     `json:"warn_count"`
	AllowCount    int     `json:"allow_count"`
	BlockRate     float64 `json:"block_rate"`
	ActiveAgents  int     `json:"active_agents"`
	TotalAgents   int     `json:"total_agents"`
	FPRate        float64 `json:"fp_rate"`
	TopDetector   string  `json:"top_detector"`
	TopUser       string  `json:"top_user"`
}

// UpsertActionStats upserts action x detector daily stats.
func (s *Store) UpsertActionStats(date, agentID, action, detector string, count int) error {
	_, err := s.db.Exec(`
		INSERT INTO audit_daily (date, agent_id, action, detector, count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (date, agent_id, action, detector)
		DO UPDATE SET count = count + excluded.count`,
		date, agentID, action, detector, count,
	)
	if err != nil {
		return fmt.Errorf("upsert action stats: %w", err)
	}
	return nil
}

// UpsertUserStats upserts user x action daily stats.
func (s *Store) UpsertUserStats(date, agentID, userID, action string, count int) error {
	_, err := s.db.Exec(`
		INSERT INTO audit_user_daily (date, agent_id, user_id, action, count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (date, agent_id, user_id, action)
		DO UPDATE SET count = count + excluded.count`,
		date, agentID, userID, action, count,
	)
	if err != nil {
		return fmt.Errorf("upsert user stats: %w", err)
	}
	return nil
}

// UpsertPolicyStats upserts policy trigger/FP daily stats.
func (s *Store) UpsertPolicyStats(date, agentID, policyName string, triggerCount, fpCount int) error {
	_, err := s.db.Exec(`
		INSERT INTO audit_policy_daily (date, agent_id, policy_name, trigger_count, false_positive_count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (date, agent_id, policy_name)
		DO UPDATE SET trigger_count = trigger_count + excluded.trigger_count,
		              false_positive_count = false_positive_count + excluded.false_positive_count`,
		date, agentID, policyName, triggerCount, fpCount,
	)
	if err != nil {
		return fmt.Errorf("upsert policy stats: %w", err)
	}
	return nil
}

// GetKPISummary returns aggregated KPI data for the specified number of days.
func (s *Store) GetKPISummary(days int) (*KPISummary, error) {
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")

	kpi := &KPISummary{}

	// Total requests and action counts
	rows, err := s.db.Query(`
		SELECT action, SUM(count) FROM audit_daily
		WHERE date >= ? GROUP BY action`, since)
	if err != nil {
		return nil, fmt.Errorf("query action counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var action string
		var count int
		if err := rows.Scan(&action, &count); err != nil {
			return nil, fmt.Errorf("scan action count: %w", err)
		}
		kpi.TotalRequests += count
		switch action {
		case "block":
			kpi.BlockCount = count
		case "warn":
			kpi.WarnCount = count
		case "allow":
			kpi.AllowCount = count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if kpi.TotalRequests > 0 {
		kpi.BlockRate = float64(kpi.BlockCount) / float64(kpi.TotalRequests) * 100
	}

	// Active agents (heartbeat within 5 minutes)
	kpi.TotalAgents, kpi.ActiveAgents, err = s.CountAgents()
	if err != nil {
		return nil, fmt.Errorf("count agents: %w", err)
	}

	// FP rate
	var totalTriggers, totalFP int
	err = s.db.QueryRow(`
		SELECT COALESCE(SUM(trigger_count), 0), COALESCE(SUM(false_positive_count), 0)
		FROM audit_policy_daily WHERE date >= ?`, since).Scan(&totalTriggers, &totalFP)
	if err != nil {
		return nil, fmt.Errorf("query FP rate: %w", err)
	}
	if totalTriggers > 0 {
		kpi.FPRate = float64(totalFP) / float64(totalTriggers) * 100
	}

	// Top detector
	err = s.db.QueryRow(`
		SELECT detector FROM audit_daily
		WHERE date >= ? AND action != 'allow'
		GROUP BY detector ORDER BY SUM(count) DESC LIMIT 1`, since).Scan(&kpi.TopDetector)
	if err != nil {
		kpi.TopDetector = "-"
	}

	// Top user (by non-allow actions)
	err = s.db.QueryRow(`
		SELECT user_id FROM audit_user_daily
		WHERE date >= ? AND action != 'allow'
		GROUP BY user_id ORDER BY SUM(count) DESC LIMIT 1`, since).Scan(&kpi.TopUser)
	if err != nil {
		kpi.TopUser = "-"
	}

	return kpi, nil
}

// DailyActionCount holds a single day's action count for charts.
type DailyActionCount struct {
	Date   string `json:"date"`
	Action string `json:"action"`
	Count  int    `json:"count"`
}

// GetDailyActionCounts returns daily action counts for charts.
func (s *Store) GetDailyActionCounts(days int) ([]DailyActionCount, error) {
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")

	rows, err := s.db.Query(`
		SELECT date, action, SUM(count) FROM audit_daily
		WHERE date >= ?
		GROUP BY date, action ORDER BY date`, since)
	if err != nil {
		return nil, fmt.Errorf("query daily action counts: %w", err)
	}
	defer rows.Close()

	var results []DailyActionCount
	for rows.Next() {
		var d DailyActionCount
		if err := rows.Scan(&d.Date, &d.Action, &d.Count); err != nil {
			return nil, fmt.Errorf("scan daily action count: %w", err)
		}
		results = append(results, d)
	}
	return results, rows.Err()
}

// TopPolicyViolation holds a policy's aggregated violation data.
type TopPolicyViolation struct {
	PolicyName    string `json:"policy_name"`
	TriggerCount  int    `json:"trigger_count"`
	FPCount       int    `json:"fp_count"`
	LastTriggered string `json:"last_triggered"`
}

// GetTopPolicyViolations returns the most-triggered policies within the given days window.
func (s *Store) GetTopPolicyViolations(days, limit int) ([]TopPolicyViolation, error) {
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")

	rows, err := s.db.Query(`
		SELECT policy_name,
		       SUM(trigger_count) AS total_triggers,
		       SUM(false_positive_count) AS total_fp,
		       MAX(date) AS last_date
		FROM audit_policy_daily
		WHERE date >= ?
		GROUP BY policy_name
		ORDER BY total_triggers DESC
		LIMIT ?`, since, limit)
	if err != nil {
		return nil, fmt.Errorf("query top policy violations: %w", err)
	}
	defer rows.Close()

	var results []TopPolicyViolation
	for rows.Next() {
		var v TopPolicyViolation
		if err := rows.Scan(&v.PolicyName, &v.TriggerCount, &v.FPCount, &v.LastTriggered); err != nil {
			return nil, fmt.Errorf("scan top policy violation: %w", err)
		}
		results = append(results, v)
	}
	return results, rows.Err()
}

// AgentActivity holds per-agent inspection and block counts.
type AgentActivity struct {
	AgentID    string `json:"agent_id"`
	TotalCount int    `json:"total_count"`
	BlockCount int    `json:"block_count"`
	WarnCount  int    `json:"warn_count"`
	AllowCount int    `json:"allow_count"`
}

// GetAgentActivity returns per-agent activity within the given days window.
func (s *Store) GetAgentActivity(days int) ([]AgentActivity, error) {
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")

	rows, err := s.db.Query(`
		SELECT agent_id,
		       SUM(count) AS total,
		       SUM(CASE WHEN action = 'block' THEN count ELSE 0 END) AS blocks,
		       SUM(CASE WHEN action = 'warn' THEN count ELSE 0 END) AS warns,
		       SUM(CASE WHEN action = 'allow' THEN count ELSE 0 END) AS allows
		FROM audit_daily
		WHERE date >= ?
		GROUP BY agent_id
		ORDER BY total DESC`, since)
	if err != nil {
		return nil, fmt.Errorf("query agent activity: %w", err)
	}
	defer rows.Close()

	var results []AgentActivity
	for rows.Next() {
		var a AgentActivity
		if err := rows.Scan(&a.AgentID, &a.TotalCount, &a.BlockCount, &a.WarnCount, &a.AllowCount); err != nil {
			return nil, fmt.Errorf("scan agent activity: %w", err)
		}
		results = append(results, a)
	}
	return results, rows.Err()
}

// GetViolationsToday returns the count of non-allow actions for today.
func (s *Store) GetViolationsToday() (int, error) {
	today := time.Now().UTC().Format("2006-01-02")
	var count int
	err := s.db.QueryRow(`
		SELECT COALESCE(SUM(count), 0) FROM audit_daily
		WHERE date = ? AND action != 'allow'`, today).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("query violations today: %w", err)
	}
	return count, nil
}
