package tui

import "math/rand"

// phrases.go - Pre-defined corruption phrases for consistent branding
// Based on @whykusanagi/corrupted-theme translation-failure aesthetic
// NO leet speak - Pure Japanese/English/Romaji mixing

// LoadingPhrases - Status messages for loading states
var LoadingPhrases = struct {
	Standard string
	Data     string
	Process  string
	Progress string
	Waiting  string
}{
	Standard: "ロード loading 読み込み中...",
	Data:     "loaディング data...",
	Process:  "読み込み yomikomi プロセス...",
	Progress: "ロード rōdo in progress...",
	Waiting:  "待機 waiting taiki...",
}

// ProcessingPhrases - Status messages for processing states
var ProcessingPhrases = struct {
	Standard string
	Request  string
	Active   string
	Process  string
	Execute  string
}{
	Standard: "処理 processing purosesu...",
	Request:  "pro理cessing request...",
	Active:   "処理中 shori-chū...",
	Process:  "プロセス process active...",
	Execute:  "実行 executing jikkō...",
}

// AnalyzingPhrases - Status messages for analysis states
var AnalyzingPhrases = struct {
	Standard string
	Data     string
	Progress string
	Active   string
	Analysis string
}{
	Standard: "分析 analyzing bunseki...",
	Data:     "ana分lysing data...",
	Progress: "解析 kaiseki in progress...",
	Active:   "分析中 bunseki-chū...",
	Analysis: "データ data 解析 analysis...",
}

// CorruptingPhrases - Celeste-specific corruption messages
var CorruptingPhrases = struct {
	Standard string
	System   string
	Active   string
	Deep     string
}{
	Standard: "壊れ corrupting kowarete...",
	System:   "cor壊rupting system...",
	Active:   "corruption 壊れ active...",
	Deep:     "深淵 abyss 壊れ corruption...",
}

// ConnectingPhrases - Connection state messages
var ConnectingPhrases = struct {
	Standard    string
	Established string
	Active      string
	Link        string
	Network     string
}{
	Standard:    "接続 connecting setsuzoku...",
	Established: "con接nection established...",
	Active:      "接続中 setsuzoku-chū...",
	Link:        "link 接続 active...",
	Network:     "ネットワーク network 接続...",
}

// WatchingPhrases - Celeste watching/observing messages
var WatchingPhrases = struct {
	Standard  string
	System    string
	Active    string
	Observing string
	Watching  string
}{
	Standard:  "👁️ 監視 watching kanshi 👁️",
	System:    "wat監ching system...",
	Active:    "監視中 kanshi-chū...",
	Observing: "観察 observing kansatsu...",
	Watching:  "👁️ 見ている miteiru 👁️",
}

// DashboardHeaders - Main dashboard title headers
var DashboardHeaders = struct {
	Usage    string
	Session  string
	Stats    string
	Token    string
	Cost     string
	Provider string
}{
	Usage:    "👁️  USAGE 統計 ANALYTICS  👁️",
	Session:  "👁️  SESSION データ MANAGER  👁️",
	Stats:    "👁️  STATS 統計 DASHBOARD  👁️",
	Token:    "👁️  TOKEN 使用 TRACKER  👁️",
	Cost:     "👁️  COST 計算 CALCULATOR  👁️",
	Provider: "👁️  PROVIDER 分類 BREAKDOWN  👁️",
}

// DashboardSubtitles - Subtitle messages for dashboards
var DashboardSubtitles = struct {
	Corrupting string
	Processing string
	Analyzing  string
	Watching   string
	Generating string
	Loading    string
}{
	Corrupting: "⟨ 壊れ corrupting kowarete from the 虚空 void... ⟩",
	Processing: "⟨ 処理 processing purosesu data 深淵 abyss... ⟩",
	Analyzing:  "⟨ 分析 analyzing bunseki from 虚空 kokū... ⟩",
	Watching:   "⟨ 監視 watching kanshi the 深淵 shinnen... ⟩",
	Generating: "⟨ 生成 generating seisei 統計 statistics... ⟩",
	Loading:    "⟨ 読み込み loading yomikomi データ data... ⟩",
}

// SectionHeaders - Section header titles
var SectionHeaders = struct {
	Lifetime struct {
		Corruption string
		Usage      string
		Data       string
		Stats      string
		Records    string
	}
	Provider struct {
		Breakdown  string
		Top        string
		Usage      string
		Statistics string
		Analysis   string
	}
	Session struct {
		Data    string
		Active  string
		Recent  string
		Current string
		History string
	}
	Token struct {
		Usage     string
		Cost      string
		Spending  string
		Budget    string
		Breakdown string
	}
	Time struct {
		Today   string
		Week    string
		Recent  string
		Current string
		Latest  string
	}
}{
	Lifetime: struct {
		Corruption string
		Usage      string
		Data       string
		Stats      string
		Records    string
	}{
		Corruption: "█ LIFETIME 統計 CORRUPTION:",
		Usage:      "█ TOTAL 使用 USAGE:",
		Data:       "█ ALL-TIME データ DATA:",
		Stats:      "█ CUMULATIVE 統計 STATS:",
		Records:    "█ HISTORICAL 記録 RECORDS:",
	},
	Provider: struct {
		Breakdown  string
		Top        string
		Usage      string
		Statistics string
		Analysis   string
	}{
		Breakdown:  "█ PROVIDER 分類 BREAKDOWN:",
		Top:        "█ TOP プロバイダー PROVIDERS:",
		Usage:      "█ ENDPOINT 使用 USAGE:",
		Statistics: "█ API 統計 STATISTICS:",
		Analysis:   "█ SERVICE 分析 ANALYSIS:",
	},
	Session: struct {
		Data    string
		Active  string
		Recent  string
		Current string
		History string
	}{
		Data:    "█ SESSION データ DATA:",
		Active:  "█ ACTIVE セッション SESSIONS:",
		Recent:  "█ RECENT 活動 ACTIVITY:",
		Current: "█ CURRENT 状態 STATUS:",
		History: "█ HISTORY 履歴 RECORDS:",
	},
	Token: struct {
		Usage     string
		Cost      string
		Spending  string
		Budget    string
		Breakdown string
	}{
		Usage:     "█ TOKEN 使用 USAGE:",
		Cost:      "█ COST 計算 CALCULATION:",
		Spending:  "█ SPENDING 支出 ANALYSIS:",
		Budget:    "█ BUDGET 予算 TRACKER:",
		Breakdown: "█ EXPENSE 費用 BREAKDOWN:",
	},
	Time: struct {
		Today   string
		Week    string
		Recent  string
		Current string
		Latest  string
	}{
		Today:   "█ TODAY 今日 ACTIVITY:",
		Week:    "█ THIS WEEK 今週 STATS:",
		Recent:  "█ RECENT 最近 USAGE:",
		Current: "█ CURRENT 現在 STATUS:",
		Latest:  "█ LATEST 最新 DATA:",
	},
}

// DataLabels - Labels for data display
var DataLabels = struct {
	Session struct {
		Total  string
		Active string
		Count  string
		Number string
		Data   string
	}
	Token struct {
		Total  string
		Input  string
		Output string
		Usage  string
		Number string
	}
	Cost struct {
		Total      string
		Estimated  string
		PerSession string
		Calculated string
		Spending   string
	}
	Message struct {
		Total  string
		Number string
		Count  string
		Data   string
		Total2 string
	}
	Provider struct {
		Name       string
		Statistics string
		API        string
		Service    string
		Stats      string
	}
}{
	Session: struct {
		Total  string
		Active string
		Count  string
		Number string
		Data   string
	}{
		Total:  "Total セッション",
		Active: "Active セッション",
		Count:  "セッション count",
		Number: "session 数",
		Data:   "セッション data",
	},
	Token: struct {
		Total  string
		Input  string
		Output string
		Usage  string
		Number string
	}{
		Total:  "Total トークン",
		Input:  "Input トークン",
		Output: "Output トークン",
		Usage:  "トークン usage",
		Number: "token 数",
	},
	Cost: struct {
		Total      string
		Estimated  string
		PerSession string
		Calculated string
		Spending   string
	}{
		Total:      "Total コスト",
		Estimated:  "Estimated コスト",
		PerSession: "コスト per session",
		Calculated: "cost 計算",
		Spending:   "spending 支出",
	},
	Message: struct {
		Total  string
		Number string
		Count  string
		Data   string
		Total2 string
	}{
		Total:  "Total メッセージ",
		Number: "Message 数",
		Count:  "メッセージ count",
		Data:   "msg データ",
		Total2: "messages 合計",
	},
	Provider: struct {
		Name       string
		Statistics string
		API        string
		Service    string
		Stats      string
	}{
		Name:       "プロバイダー name",
		Statistics: "Provider 統計",
		API:        "API endpoint",
		Service:    "service 名",
		Stats:      "プロバイダー stats",
	},
}

// StatusMessages - Success/error/warning/info messages
var StatusMessages = struct {
	Success struct {
		Complete string
		Success  string
		Done     string
		Ready    string
		Saved    string
	}
	Error struct {
		Detected string
		Failed   string
		Occurred string
		Problem  string
		Bug      string
	}
	Warning struct {
		Warning   string
		Caution   string
		Alert     string
		Attention string
		Detected  string
	}
	Info struct {
		Info         string
		Notice       string
		Message      string
		Notification string
		Information  string
	}
}{
	Success: struct {
		Complete string
		Success  string
		Done     string
		Ready    string
		Saved    string
	}{
		Complete: "完了 complete kanryō ✓",
		Success:  "成功 success seikō ✓",
		Done:     "Done 完了 done",
		Ready:    "Ready 準備 junbi",
		Saved:    "Saved 保存 hozon",
	},
	Error: struct {
		Detected string
		Failed   string
		Occurred string
		Problem  string
		Bug      string
	}{
		Detected: "エラー error detected 検出",
		Failed:   "Failed 失敗 shippai",
		Occurred: "Error エラー occurred",
		Problem:  "問題 problem mondai",
		Bug:      "不具合 bug fuguai",
	},
	Warning: struct {
		Warning   string
		Caution   string
		Alert     string
		Attention string
		Detected  string
	}{
		Warning:   "警告 warning keikoku ⚠",
		Caution:   "Caution 注意 chūi",
		Alert:     "Alert 警報 keihou",
		Attention: "注意 attention required",
		Detected:  "Warning 警告 detected",
	},
	Info: struct {
		Info         string
		Notice       string
		Message      string
		Notification string
		Information  string
	}{
		Info:         "情報 info jōhō ℹ",
		Notice:       "Notice 通知 tsūchi",
		Message:      "Info 情報 message",
		Notification: "お知らせ notification",
		Information:  "情報 information",
	},
}

// FooterMessages - Footer/ending messages
var FooterMessages = struct {
	Report struct {
		End      string
		Complete string
		Ended    string
		Stats    string
		Analysis string
	}
	Void struct {
		Sinking   string
		Returning string
		Consumed  string
		Awaits    string
		Back      string
	}
}{
	Report: struct {
		End      string
		Complete string
		Ended    string
		Stats    string
		Analysis string
	}{
		End:      "⟨ 終わり end of report owari... ⟩",
		Complete: "⟨ 完了 complete kanryō... ⟩",
		Ended:    "⟨ レポート report 終了 ended... ⟩",
		Stats:    "⟨ 統計 stats 終わり owari... ⟩",
		Analysis: "⟨ 分析 analysis 完了 complete... ⟩",
	},
	Void: struct {
		Sinking   string
		Returning string
		Consumed  string
		Awaits    string
		Back      string
	}{
		Sinking:   "⟨ sinking into the 深淵 abyss shinnen... ⟩",
		Returning: "⟨ returning to the 虚空 void kokū... ⟩",
		Consumed:  "⟨ consumed by 闇 darkness yami... ⟩",
		Awaits:    "⟨ 深淵 shinnen awaits... ⟩",
		Back:      "⟨ back to the 虚空 kokū... ⟩",
	},
}

// ThematicPhrases - Void/Abyss/Corruption themed phrases
var ThematicPhrases = struct {
	Void struct {
		Deep       string
		Void       string
		Darkness   string
		Connected  string
		Corruption string
		From       string
		Into       string
		Consumed   string
	}
	Corruption struct {
		Standard string
		System   string
		Active   string
		Active2  string
		Data     string
		Deeply   string
	}
	Eye struct {
		Watching  string
		Watching2 string
		Observing string
		Under     string
		Always    string
		Celeste   string
	}
}{
	Void: struct {
		Deep       string
		Void       string
		Darkness   string
		Connected  string
		Corruption string
		From       string
		Into       string
		Consumed   string
	}{
		Deep:       "深淵 deep abyss shinnen",
		Void:       "虚空 void kokū",
		Darkness:   "闇 darkness yami",
		Connected:  "深淵 shinnen 接続 connected",
		Corruption: "虚空 kokū corruption 壊れ",
		From:       "from the 深淵 abyss...",
		Into:       "into the 虚空 void...",
		Consumed:   "consumed by 闇 yami...",
	},
	Corruption: struct {
		Standard string
		System   string
		Active   string
		Active2  string
		Data     string
		Deeply   string
	}{
		Standard: "壊れ corruption kowarete",
		System:   "cor壊rupting system...",
		Active:   "corruption 壊れ active",
		Active2:  "壊れている kowarete-iru",
		Data:     "data 壊れ corruption",
		Deeply:   "deeply 壊れ corrupted",
	},
	Eye: struct {
		Watching  string
		Watching2 string
		Observing string
		Under     string
		Always    string
		Celeste   string
	}{
		Watching:  "👁️ 監視 watching kanshi 👁️",
		Watching2: "👁️ 見ている miteiru 👁️",
		Observing: "👁️ 観察 observing kansatsu 👁️",
		Under:     "under 監視 surveillance...",
		Always:    "👁️ always watching...",
		Celeste:   "Celeste 監視 is watching...",
	},
}

// Helper function to get a random phrase from a slice
func GetRandomPhrase(phrases []string) string {
	if len(phrases) == 0 {
		return ""
	}
	return phrases[rand.Intn(len(phrases))]
}

// Example usage:
//
// In your dashboard code:
//   header := DashboardHeaders.Usage
//   subtitle := DashboardSubtitles.Processing
//   section := SectionHeaders.Lifetime.Corruption
//   label := DataLabels.Session.Total
//   footer := FooterMessages.Report.End
//
// For status updates:
//   status := ProcessingPhrases.Standard
//   loading := LoadingPhrases.Data
//   watching := WatchingPhrases.Standard
