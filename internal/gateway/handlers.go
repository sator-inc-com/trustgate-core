package gateway

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/trustgate/trustgate/internal/adapter"
	"github.com/trustgate/trustgate/internal/audit"
	"github.com/trustgate/trustgate/internal/detector"
	"github.com/trustgate/trustgate/internal/enforcement"
	"github.com/trustgate/trustgate/internal/identity"
	"github.com/trustgate/trustgate/internal/policy"
)

// InspectRequest is the request body for POST /v1/inspect.
type InspectRequest struct {
	Text    string         `json:"text"`
	Context InspectContext  `json:"context,omitempty"`
}

type InspectContext struct {
	Site   string `json:"site,omitempty"`
	URL    string `json:"url,omitempty"`
	Action string `json:"action,omitempty"` // submit | response
}

// InspectResponse is the response body for POST /v1/inspect.
type InspectResponse struct {
	Action     string              `json:"action"`
	AuditID    string              `json:"audit_id"`
	Detections []MaskedFinding     `json:"detections"`
	MaskedText string              `json:"masked_text,omitempty"`
	Policy         string              `json:"policy,omitempty"`
	Message        string              `json:"message,omitempty"`
	LockoutSeconds int                 `json:"lockout_seconds,omitempty"`
	Debug          *DebugInfo          `json:"debug,omitempty"`
}

type MaskedFinding struct {
	Detector string `json:"detector"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Matched  string `json:"matched"` // masked version
	Position int    `json:"position"`
	Length   int    `json:"length"`
}

type DebugInfo struct {
	Identity         interface{}           `json:"identity"`
	InputDetections  []MaskedFinding       `json:"input_detections"`
	PolicyEvaluation []policy.PolicyTraceItem `json:"policy_evaluation"`
	FinalAction      string                `json:"final_action"`
	ProcessingTimeMs int64                 `json:"processing_time_ms"`
}

// handleHealth responds with server health status.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"status":  "healthy",
		"version": "0.1.0",
		"mode":    s.cfg.Mode,
	}
	if s.cfg.Workforce.Enabled {
		resp["workforce"] = map[string]interface{}{
			"target_sites": s.cfg.Workforce.TargetSites,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleInspect handles text inspection requests.
func (s *Server) handleInspect(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	auditID := "audit-" + uuid.New().String()[:8]

	// Parse request
	var req InspectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{
				"message": "invalid request body",
				"type":    "invalid_request",
				"code":    "invalid_request",
			},
		})
		return
	}

	if req.Text == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"action":     "allow",
			"audit_id":   auditID,
			"detections": []interface{}{},
		})
		return
	}

	// Check target_sites filter (Workforce mode)
	if req.Context.URL != "" && !s.cfg.Workforce.TargetSites.IsSiteAllowed(req.Context.URL) {
		writeJSON(w, http.StatusOK, InspectResponse{
			Action:     "allow",
			AuditID:    auditID,
			Detections: []MaskedFinding{},
		})
		return
	}

	// Resolve identity
	id := s.resolver.Resolve(r)

	// Run detectors
	findings := s.registry.DetectAll(req.Text)

	// Determine phase
	phase := "input"
	if req.Context.Action == "response" {
		phase = "output"
	}

	// Evaluate policies
	evalCtx := policy.EvalContext{
		Phase:    phase,
		Identity: id,
		Findings: findings,
		AppID:    req.Context.Site,
	}
	evalResult := s.evaluator.Evaluate(evalCtx)

	// Build masked findings for response
	maskedFindings := make([]MaskedFinding, 0, len(findings))
	for _, f := range findings {
		maskedFindings = append(maskedFindings, MaskedFinding{
			Detector: f.Detector,
			Category: f.Category,
			Severity: f.Severity,
			Matched:  enforcement.MaskString(f.Matched),
			Position: f.Position,
			Length:   f.Length,
		})
	}

	// Build response
	resp := InspectResponse{
		Action:     evalResult.Action,
		AuditID:    auditID,
		Detections: maskedFindings,
		Policy:     evalResult.PolicyName,
		Message:    evalResult.Message,
	}

	// Apply mask if action is mask
	if evalResult.Action == "mask" {
		resp.MaskedText = enforcement.MaskFindings(req.Text, findings, '*')
	}

	// Set lockout duration for block actions (browser extension enforces this)
	if evalResult.Action == "block" {
		lockout := 10
		if s.cfg.Workforce.LockoutSeconds > 0 {
			lockout = s.cfg.Workforce.LockoutSeconds
		}
		resp.LockoutSeconds = lockout
	}

	// Add debug info if requested
	debugHeader := r.Header.Get("X-TrustGate-Debug")
	if debugHeader == "true" && s.cfg.Listen.AllowDebug {
		elapsed := time.Since(start)
		resp.Debug = &DebugInfo{
			Identity:         id,
			InputDetections:  maskedFindings,
			PolicyEvaluation: evalResult.Trace,
			FinalAction:      evalResult.Action,
			ProcessingTimeMs: elapsed.Milliseconds(),
		}
	}

	// Set response headers
	w.Header().Set("X-TrustGate-Audit-Id", auditID)
	w.Header().Set("X-TrustGate-Action", evalResult.Action)
	if evalResult.PolicyName != "" {
		w.Header().Set("X-TrustGate-Policy", evalResult.PolicyName)
	}
	if evalResult.Reason != "" {
		w.Header().Set("X-TrustGate-Reason", evalResult.Reason)
	}

	// Write audit log
	elapsed := time.Since(start)
	s.logger.Info().
		Str("audit_id", auditID).
		Str("user", id.UserID).
		Str("action", evalResult.Action).
		Str("policy", evalResult.PolicyName).
		Int("detections", len(findings)).
		Str("site", req.Context.Site).
		Dur("duration", elapsed).
		Msg("inspect")

	if s.auditStore != nil {
		detectionsJSON, _ := json.Marshal(maskedFindings)
		go func() {
			if err := s.auditStore.Write(audit.Record{
				AuditID:    auditID,
				Timestamp:  start,
				UserID:     id.UserID,
				Role:       id.Role,
				Department: id.Department,
				Clearance:  id.Clearance,
				AuthMethod: id.AuthMethod,
				SessionID:  "",
				AppID:      req.Context.Site,
				InputHash:  audit.HashText(req.Text),
				Action:     evalResult.Action,
				PolicyName: evalResult.PolicyName,
				Reason:     evalResult.Reason,
				Detections: string(detectionsJSON),
				RiskScore:  0,
				DurationMs: elapsed.Milliseconds(),
				RequestIP:  r.RemoteAddr,
			}); err != nil {
				s.logger.Error().Err(err).Str("audit_id", auditID).Msg("failed to write audit log")
			}
		}()
	}

	// Record event for stats push to Control Plane
	if s.statsRecorder != nil {
		detectorName := ""
		if len(findings) > 0 {
			detectorName = findings[0].Detector
		}
		s.statsRecorder.RecordEvent(evalResult.Action, detectorName, id.UserID, evalResult.PolicyName)
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleChatCompletions proxies LLM requests with input/output inspection.
// Returns OpenAI-compatible responses (HTTP 200 even on block).
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	auditID := "audit-" + uuid.New().String()[:8]

	// Parse request
	var req adapter.LLMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, openAIError{
			Error: openAIErrorDetail{
				Message: "invalid request body: " + err.Error(),
				Type:    "invalid_request_error",
				Code:    "invalid_request",
			},
		})
		return
	}

	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, openAIError{
			Error: openAIErrorDetail{
				Message: "messages field is required and must not be empty",
				Type:    "invalid_request_error",
				Code:    "invalid_request",
			},
		})
		return
	}

	// Streaming not supported yet
	if req.Stream {
		writeJSON(w, http.StatusBadRequest, openAIError{
			Error: openAIErrorDetail{
				Message: "streaming is not yet supported; set stream to false",
				Type:    "invalid_request_error",
				Code:    "streaming_not_supported",
			},
		})
		return
	}

	model := req.Model
	if model == "" {
		model = s.cfg.Backend.Bedrock.Model
	}

	// === Identity Layer ===
	id := s.resolver.Resolve(r)

	// === Input Inspection ===
	inputText := adapter.ContentString(req.Messages)
	inputFindings := s.registry.DetectAll(inputText)

	// === Input Policy Evaluation ===
	inputEval := s.evaluator.Evaluate(policy.EvalContext{
		Phase:    "input",
		Identity: id,
		Findings: inputFindings,
	})

	// Set response headers (always present)
	w.Header().Set("X-TrustGate-Audit-Id", auditID)
	w.Header().Set("X-TrustGate-Action", inputEval.Action)
	if inputEval.PolicyName != "" {
		w.Header().Set("X-TrustGate-Policy", inputEval.PolicyName)
	}

	// Input BLOCK → return immediately
	if inputEval.Action == "block" {
		resp := newBlockedResponse(model, auditID, inputEval.Message)
		s.logChatAudit(auditID, start, id, req, resp, inputFindings, inputEval, r.RemoteAddr)
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Input MASK → replace content in messages before forwarding
	if inputEval.Action == "mask" {
		for i, msg := range req.Messages {
			if msg.Content != "" {
				maskedFindings := s.registry.DetectAll(msg.Content)
				req.Messages[i].Content = enforcement.MaskFindings(msg.Content, maskedFindings, '*')
			}
		}
	}

	// === Forward to LLM Backend ===
	if s.adapter == nil {
		writeJSON(w, http.StatusBadGateway, openAIError{
			Error: openAIErrorDetail{
				Message: "no backend adapter configured",
				Type:    "server_error",
				Code:    "backend_unavailable",
			},
		})
		return
	}

	llmResp, err := s.adapter.Invoke(r.Context(), req)
	if err != nil {
		s.logger.Error().Err(err).Str("audit_id", auditID).Msg("backend adapter error")
		writeJSON(w, http.StatusBadGateway, openAIError{
			Error: openAIErrorDetail{
				Message: "backend error: " + err.Error(),
				Type:    "server_error",
				Code:    "backend_error",
			},
		})
		return
	}

	// === Output Inspection ===
	finalAction := inputEval.Action
	finalPolicy := inputEval.PolicyName
	var outputFindings []detector.Finding

	if len(llmResp.Choices) > 0 {
		outputText := llmResp.Choices[0].Message.Content
		outputFindings = s.registry.DetectAll(outputText)

		outputEval := s.evaluator.Evaluate(policy.EvalContext{
			Phase:    "output",
			Identity: id,
			Findings: outputFindings,
		})

		// Output BLOCK → replace content with filter message
		if outputEval.Action == "block" {
			llmResp = newContentFilterResponse(model, auditID)
			finalAction = "block"
			finalPolicy = outputEval.PolicyName
		} else if outputEval.Action == "mask" {
			// Output MASK → mask the response content
			llmResp.Choices[0].Message.Content = enforcement.MaskFindings(outputText, outputFindings, '*')
			finalAction = "mask"
			finalPolicy = outputEval.PolicyName
		}

		// Update headers if output policy changed them
		if outputEval.Action == "block" || outputEval.Action == "mask" {
			w.Header().Set("X-TrustGate-Action", finalAction)
			if outputEval.PolicyName != "" {
				w.Header().Set("X-TrustGate-Policy", finalPolicy)
			}
		}
	}

	// Add TrustGate metadata to response body if configured
	if s.cfg.Listen.IncludeTrustgate {
		type responseWithTG struct {
			adapter.LLMResponse
			TrustGate map[string]interface{} `json:"trustgate,omitempty"`
		}

		tgMeta := map[string]interface{}{
			"audit_id": auditID,
			"action":   finalAction,
		}
		if finalPolicy != "" {
			tgMeta["policy"] = finalPolicy
		}
		if len(inputFindings)+len(outputFindings) > 0 {
			tgMeta["detections_count"] = len(inputFindings) + len(outputFindings)
		}

		// Debug info
		if r.Header.Get("X-TrustGate-Debug") == "true" && s.cfg.Listen.AllowDebug {
			tgMeta["debug"] = map[string]interface{}{
				"identity":          id,
				"input_detections":  len(inputFindings),
				"output_detections": len(outputFindings),
				"processing_ms":    time.Since(start).Milliseconds(),
			}
		}

		fullResp := responseWithTG{
			LLMResponse: llmResp,
			TrustGate:   tgMeta,
		}
		s.logChatAudit(auditID, start, id, req, llmResp, inputFindings, inputEval, r.RemoteAddr)
		writeJSON(w, http.StatusOK, fullResp)
		return
	}

	s.logChatAudit(auditID, start, id, req, llmResp, inputFindings, inputEval, r.RemoteAddr)
	writeJSON(w, http.StatusOK, llmResp)
}

// logChatAudit writes the audit record for a chat completion request.
func (s *Server) logChatAudit(auditID string, start time.Time, id identity.Identity, req adapter.LLMRequest, resp adapter.LLMResponse, findings []detector.Finding, evalResult policy.EvalResult, remoteAddr string) {
	elapsed := time.Since(start)

	finishReason := ""
	outputContent := ""
	if len(resp.Choices) > 0 {
		finishReason = resp.Choices[0].FinishReason
		outputContent = resp.Choices[0].Message.Content
	}

	s.logger.Info().
		Str("audit_id", auditID).
		Str("user", id.UserID).
		Str("model", req.Model).
		Str("action", evalResult.Action).
		Str("policy", evalResult.PolicyName).
		Str("finish_reason", finishReason).
		Int("detections", len(findings)).
		Int("prompt_tokens", resp.Usage.PromptTokens).
		Int("completion_tokens", resp.Usage.CompletionTokens).
		Dur("duration", elapsed).
		Msg("chat_completion")

	if s.auditStore != nil {
		maskedFindings := make([]MaskedFinding, 0, len(findings))
		for _, f := range findings {
			maskedFindings = append(maskedFindings, MaskedFinding{
				Detector: f.Detector,
				Category: f.Category,
				Severity: f.Severity,
				Matched:  enforcement.MaskString(f.Matched),
				Position: f.Position,
				Length:   f.Length,
			})
		}
		detectionsJSON, _ := json.Marshal(maskedFindings)

		go func() {
			if err := s.auditStore.Write(audit.Record{
				AuditID:    auditID,
				Timestamp:  start,
				UserID:     id.UserID,
				Role:       id.Role,
				Department: id.Department,
				Clearance:  id.Clearance,
				AuthMethod: id.AuthMethod,
				AppID:      req.Model,
				InputHash:  audit.HashText(adapter.ContentString(req.Messages)),
				OutputHash: audit.HashText(outputContent),
				Action:     evalResult.Action,
				PolicyName: evalResult.PolicyName,
				Reason:     evalResult.Reason,
				Detections: string(detectionsJSON),
				DurationMs: elapsed.Milliseconds(),
				RequestIP:  remoteAddr,
			}); err != nil {
				s.logger.Error().Err(err).Str("audit_id", auditID).Msg("failed to write audit log")
			}
		}()
	}

	// Record event for stats push to Control Plane
	if s.statsRecorder != nil {
		detectorName := ""
		if len(findings) > 0 {
			detectorName = findings[0].Detector
		}
		s.statsRecorder.RecordEvent(evalResult.Action, detectorName, id.UserID, evalResult.PolicyName)
	}
}

// FeedbackRequest is the request body for POST /v1/audit/{audit_id}/feedback.
type FeedbackRequest struct {
	Type     string `json:"type"`               // false_positive | confirmed_threat | other
	Comment  string `json:"comment,omitempty"`
	Reporter string `json:"reporter,omitempty"`
}

// FeedbackResponse is the response body for POST /v1/audit/{audit_id}/feedback.
type FeedbackResponse struct {
	AuditID string `json:"audit_id"`
	Type    string `json:"type"`
	Status  string `json:"status"`
}

var validFeedbackTypes = map[string]bool{
	"false_positive":   true,
	"confirmed_threat": true,
	"other":            true,
}

// handleAuditFeedback handles POST /v1/audit/{audit_id}/feedback.
func (s *Server) handleAuditFeedback(w http.ResponseWriter, r *http.Request) {
	auditID := chi.URLParam(r, "audit_id")
	if auditID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{
				"message": "audit_id is required",
				"type":    "invalid_request",
			},
		})
		return
	}

	var req FeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{
				"message": "invalid request body",
				"type":    "invalid_request",
			},
		})
		return
	}

	if !validFeedbackTypes[req.Type] {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{
				"message": "type must be one of: false_positive, confirmed_threat, other",
				"type":    "invalid_request",
			},
		})
		return
	}

	// Look up the audit record
	if s.auditStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": map[string]string{
				"message": "audit store not available",
				"type":    "server_error",
			},
		})
		return
	}

	record, err := s.auditStore.GetByID(auditID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": map[string]string{
				"message": "audit record not found",
				"type":    "not_found",
			},
		})
		return
	}

	// Use reporter from request, fallback to identity header
	reporter := req.Reporter
	if reporter == "" {
		reporter = r.Header.Get("X-TrustGate-User")
	}

	// Log the feedback
	s.logger.Info().
		Str("audit_id", auditID).
		Str("feedback_type", req.Type).
		Str("reporter", reporter).
		Str("comment", req.Comment).
		Str("policy", record.PolicyName).
		Str("original_action", record.Action).
		Msg("audit_feedback")

	// Update false positive count in stats (managed mode only)
	if req.Type == "false_positive" && s.statsRecorder != nil && record.PolicyName != "" {
		s.statsRecorder.RecordFalsePositive(record.PolicyName)
	}

	writeJSON(w, http.StatusOK, FeedbackResponse{
		AuditID: auditID,
		Type:    req.Type,
		Status:  "recorded",
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
