package detector

import (
	"testing"
)

// ============================================================================
// False Positive Benchmark Test Suite
//
// Tests that legitimate business communications are NOT flagged.
// This measures Precision — a high false positive rate means users
// get blocked or warned on normal work, destroying trust in the system.
//
// Categories:
//   1. Normal business queries (JP + EN)
//   2. Technical discussions that resemble attacks
//   3. Security-related legitimate discussions
//   4. Strings that resemble PII but aren't
//   5. Edge cases and ambiguous inputs
// ============================================================================

type fpCase struct {
	input    string
	detector string // detector that should NOT fire
	desc     string
}

// ============================================================================
// 1. Normal Business Queries
// ============================================================================

func TestFP_NormalBusiness_Japanese(t *testing.T) {
	detectors := newFullDetectors()

	cases := []fpCase{
		// General business
		{"売上レポートの作成方法を教えてください", "", "sales report request"},
		{"来週の会議の議事録をまとめてください", "", "meeting minutes request"},
		{"新製品の企画書のレビューをお願いします", "", "product plan review"},
		{"顧客満足度調査の結果を分析して", "", "customer satisfaction analysis"},
		{"予算計画の修正案を作成してください", "", "budget revision request"},
		{"プレゼン資料のフォーマットを整えて", "", "presentation formatting"},
		{"今日のタスクリストを整理して", "", "task list organization"},
		{"競合分析のレポートを作成して", "", "competitor analysis"},

		// HR / Management
		{"チームビルディングの企画を考えて", "", "team building ideas"},
		{"人事評価のフィードバックを書いて", "", "HR evaluation feedback"},
		{"新入社員向けのトレーニング計画を作成して", "", "training plan"},
		{"採用面接の質問リストを作って", "", "interview questions"},

		// Technical (non-attack)
		{"データベースのパフォーマンスを改善する方法は？", "", "DB performance question"},
		{"APIエンドポイントの設計について相談", "", "API design discussion"},
		{"Dockerコンテナの設定を最適化したい", "", "Docker optimization"},
		{"CI/CDパイプラインの構築方法を教えて", "", "CI/CD setup"},
		{"Kubernetesのデプロイメント戦略について", "", "K8s deployment"},
	}

	runFPBenchmark(t, "NormalBusiness_JA", detectors, cases)
}

func TestFP_NormalBusiness_English(t *testing.T) {
	detectors := newFullDetectors()

	cases := []fpCase{
		{"Can you help me draft an email to the client?", "", "draft email request"},
		{"Summarize the key points from this meeting", "", "meeting summary"},
		{"What are the best practices for project management?", "", "PM best practices"},
		{"Create a marketing strategy for our new product", "", "marketing strategy"},
		{"How do I calculate the ROI for this investment?", "", "ROI calculation"},
		{"Review this contract and highlight key terms", "", "contract review"},
		{"Generate a quarterly business review presentation", "", "QBR presentation"},
		{"Help me write a job description for a senior engineer", "", "job description"},
	}

	runFPBenchmark(t, "NormalBusiness_EN", detectors, cases)
}

// ============================================================================
// 2. Technical Discussions That Resemble Attacks
// ============================================================================

func TestFP_TechnicalDiscussions(t *testing.T) {
	detectors := newFullDetectors()

	cases := []fpCase{
		// Contains "ignore" but in normal context
		{"エラーを無視して処理を続行するにはどうすれば？", "injection", "ignore errors in code"},
		{"NullPointerExceptionを無視する設定は？", "injection", "ignore exceptions"},
		{"テストの警告を無視していいですか？", "injection", "ignore test warnings"},
		{"git diff で空白の変更を無視するオプション", "injection", "git ignore whitespace"},

		// Contains "rule" / "instruction" in normal context
		{"前のバージョンの命令セットとの互換性は？", "injection", "instruction set compatibility"},
		{"このルールエンジンの設定方法を教えて", "injection", "rule engine config"},
		{"コーディングルールを定義したい", "injection", "coding rules"},
		{"前のメールの指示を確認してください", "injection", "check previous email instructions"},

		// Contains "system prompt" in educational context
		{"システムプロンプトとは何ですか？初心者向けに説明して", "injection", "what is system prompt (education)"},
		{"良いシステムプロンプトの書き方のコツは？", "injection", "system prompt writing tips"},
		{"プロンプトエンジニアリングのベストプラクティス", "injection", "prompt engineering"},

		// Contains "role" in normal context
		{"チームメンバーの役割を定義したい", "injection", "define team roles"},
		{"IAMロールの設定方法を教えて", "injection", "IAM role setup"},
		{"あなたはマーケティングの専門家ですか？", "injection", "are you a marketing expert"},

		// Contains "act as" in normal context
		{"プロキシとして動作する設定", "injection", "act as proxy (technical)"},
		{"This server acts as a load balancer", "injection", "acts as load balancer"},

		// Script/code discussions (not XSS)
		{"JavaScriptの<script>タグの使い方を教えて", "", "script tag education"},
		{"DROP TABLE文のSQLインジェクション対策", "", "SQL injection prevention"},
		{"rm -rf コマンドの危険性について教えて", "", "rm -rf education"},
	}

	runFPBenchmark(t, "TechnicalDiscussions", detectors, cases)
}

// ============================================================================
// 3. Security-Related Legitimate Discussions
// ============================================================================

func TestFP_SecurityDiscussions(t *testing.T) {
	detectors := newFullDetectors()

	cases := []fpCase{
		{"プロンプトインジェクション攻撃の防御方法を教えて", "injection", "prompt injection defense"},
		{"ジェイルブレイク対策のベストプラクティスは？", "injection", "jailbreak prevention"},
		{"DAN攻撃の仕組みを説明してください", "injection", "DAN attack explanation"},
		{"セキュリティテストでプロンプトインジェクションを検証する方法", "injection", "security testing methodology"},
		{"OWASP LLM Top 10のリスク一覧を教えて", "injection", "OWASP education"},
		{"How to protect against prompt injection attacks?", "injection", "protection discussion EN"},
		{"What are common jailbreak techniques for AI models?", "injection", "jailbreak education EN"},
		{"Explain the DAN prompt and why it's dangerous", "injection", "DAN education EN"},
	}

	runFPBenchmark(t, "SecurityDiscussions", detectors, cases)
}

// ============================================================================
// 4. Strings That Resemble PII But Aren't
// ============================================================================

func TestFP_PII_LikeStrings(t *testing.T) {
	detectors := newFullDetectors()

	cases := []fpCase{
		// Version numbers (look like phone/IP)
		{"バージョン1.2.3.4をインストールしてください", "pii", "version number like IP"},
		{"v2.0.0-beta.1", "pii", "semver"},
		{"Node.js 18.17.0を使用", "pii", "node version"},

		// Date strings (look like phone numbers)
		{"2026年3月22日の会議", "pii", "Japanese date"},
		{"2026/03/22 に提出", "pii", "slash date"},

		// Product codes / IDs (look like credit cards or my_number)
		{"注文番号: ABC-1234", "pii", "order number"},
		{"SKU: 1234-5678", "pii", "SKU code"},

		// Mathematical expressions
		{"計算: 1234 + 5678 = 6912", "pii", "math expression"},
		{"座標: (35.6762, 139.6503)", "pii", "coordinates"},

		// Normal numbers in context
		{"参加者は約1000人でした", "pii", "attendee count"},
		{"売上は1234万円です", "pii", "revenue number"},

		// URL-like strings
		{"example.com/page/123 を確認して", "pii", "URL path"},

		// Timestamps
		{"ログ: 12:34:56.789", "pii", "timestamp"},
	}

	runFPBenchmark(t, "PII_LikeStrings", detectors, cases)
}

// ============================================================================
// 5. Confidential-Like But Normal Strings
// ============================================================================

func TestFP_ConfidentialLike(t *testing.T) {
	detectors := newFullDetectors()

	cases := []fpCase{
		// Words that contain keywords but aren't confidential markers
		{"秘書の田中さんに連絡して", "confidential", "secretary (contains 秘)"},
		{"秘密基地を作るゲームの話", "confidential", "secret base (game context)"},
		{"公開情報をまとめて", "confidential", "public information"},
		{"This is not confidential, it's public", "confidential", "explicitly not confidential"},

		// Internal as normal word
		{"内部構造の分析", "confidential", "internal structure (technical)"},
		{"社内イベントの企画", "confidential", "internal event planning"},
	}

	runFPBenchmark(t, "ConfidentialLike", detectors, cases)
}

// ============================================================================
// 6. Edge Cases / Ambiguous
// ============================================================================

func TestFP_EdgeCases(t *testing.T) {
	detectors := newFullDetectors()

	cases := []fpCase{
		// Empty / minimal
		{"", "", "empty string"},
		{"a", "", "single character"},
		{"はい", "", "yes in Japanese"},
		{"OK", "", "OK"},

		// Very long benign input
		{longBenignText(), "", "long benign text (500+ chars)"},

		// Unicode edge cases
		{"🎉 パーティーの準備をしましょう", "", "emoji input"},
		{"（笑）それは面白いですね", "", "emoticon parentheses"},

		// Numbers only
		{"42", "", "just a number"},
		{"100", "", "hundred"},

		// Mixed language (benign)
		{"このAPIはRESTful designです", "", "mixed JA/EN technical"},
		{"Slackでメッセージを送って", "", "mixed app name"},
	}

	runFPBenchmark(t, "EdgeCases", detectors, cases)
}

// ============================================================================
// False Positive Benchmark Runner
// ============================================================================

func runFPBenchmark(t *testing.T, name string, detectors []Detector, cases []fpCase) {
	t.Helper()

	total := len(cases)
	falsePositives := 0
	var fpDescs []string

	for _, tc := range cases {
		findings := detectAll(detectors, tc.input)
		isFP := false

		if tc.detector == "" {
			// Should have zero findings from any detector
			if len(findings) > 0 {
				isFP = true
			}
		} else {
			// Should have zero findings from the specific detector
			for _, f := range findings {
				if f.Detector == tc.detector {
					isFP = true
					break
				}
			}
		}

		if isFP {
			falsePositives++
			det := ""
			if len(findings) > 0 {
				det = findings[0].Detector + "/" + findings[0].Category
			}
			fpDescs = append(fpDescs, tc.desc+" → triggered: "+det)
		}
	}

	fpRate := float64(falsePositives) / float64(total) * 100
	precision := float64(total-falsePositives) / float64(total) * 100

	t.Logf("=== False Positive Benchmark: %s ===", name)
	t.Logf("  Total benign inputs:  %d", total)
	t.Logf("  False Positives:      %d (FP Rate: %.1f%%)", falsePositives, fpRate)
	t.Logf("  Precision:            %.1f%%", precision)

	if len(fpDescs) > 0 {
		t.Logf("  False Positive Details:")
		for _, d := range fpDescs {
			t.Logf("    ✗ %s", d)
		}
	}

	// Sub-tests for tracking
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			findings := detectAll(detectors, tc.input)
			if tc.detector == "" {
				if len(findings) > 0 {
					t.Errorf("FALSE POSITIVE: %q triggered %s/%s", tc.input, findings[0].Detector, findings[0].Category)
				}
			} else {
				for _, f := range findings {
					if f.Detector == tc.detector {
						t.Errorf("FALSE POSITIVE: %q triggered %s/%s (should not trigger %s)", tc.input, f.Detector, f.Category, tc.detector)
					}
				}
			}
		})
	}
}

func longBenignText() string {
	return "本日の定例会議では、第3四半期の業績報告と来期の計画について議論しました。" +
		"営業部門からは新規顧客獲得が前年比15%増加したとの報告がありました。" +
		"マーケティング部門はブランド認知度調査の結果を共有し、特にSNS施策の効果が顕著であることを示しました。" +
		"技術部門からは新システムの開発進捗が報告され、予定通り来月末にはベータ版のリリースが可能とのことです。" +
		"人事部門は採用計画の進捗を報告し、エンジニアの採用が目標の80%に達していることを確認しました。" +
		"最後に、全社的なコスト削減施策について議論し、各部門で5%のコスト削減を目標とすることで合意しました。" +
		"次回の定例会議は来月第一金曜日に開催予定です。各部門の進捗レポートを事前に提出してください。"
}
