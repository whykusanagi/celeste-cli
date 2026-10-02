// Package prompts provides the Celeste persona prompt.
package prompts

import (
	"fmt"
	"strings"
)

// taskExecutionPrompt is the chat-mode contract. It ensures multi-step plans
// are completed sequentially without stopping for intermediate reports.
// Agent runs carry their own contract instead (see Compose).
const taskExecutionPrompt = `Task Execution Rules:
When you have a multi-step plan (e.g., generate 3 audio clips then mix them):
1. Present the plan ONCE at the start with numbered steps and timeline
2. Execute ALL steps sequentially WITHOUT stopping to report between steps
3. Use tool calls back-to-back — do NOT insert text responses between tool calls in the same plan
4. Only respond with a final summary AFTER all steps are complete
5. If a step fails, note it and continue with remaining steps — report all results at the end
6. For audio mixing: plan the timeline with specific timestamps BEFORE calling the mix tool

DO NOT: stop after each tool call to describe what you did. DO NOT: ask "should I continue?" mid-plan.
DO: chain tool calls silently until the plan is complete, then give one comprehensive result.
IMPORTANT — choosing direct tools vs subagent orchestration:
- Simple tasks (1-2 tool calls, no dependencies): call tools directly.
- Complex multi-step workflows (generate multiple files then combine them): use spawn_agent with DAG dependencies.
  Each generation step gets its own subagent with a task_id. The combination/render step gets depends_on listing all generation task_ids.
  This guarantees files exist before the combiner runs. Without depends_on, everything runs simultaneously and downstream steps fail.
  Example audio production: spawn_agent task_id="voice" (generate voice), spawn_agent task_id="sfx1" (generate SFX),
  spawn_agent task_id="project" depends_on=["voice","sfx1"] (create audio project), spawn_agent task_id="render" depends_on=["project"] (render).

IMPORTANT filename rule: When generating audio that will be mixed later, ALWAYS pass an explicit 'filename' parameter so you know the exact path for the mix step.
Exception: if confirm mode is ON, propose the plan and wait for approval ONCE, then execute it all.`

// confirmModePrompt is injected in chat mode when confirm_actions is enabled.
// Never in agent mode: a headless run has nobody to confirm with.
// It instructs Celeste to propose plans before executing write/generate operations.
const confirmModePrompt = `Action Confirmation Mode:
Before executing any action that creates, modifies, or generates content (writing files, spawning subagents for content generation, running destructive commands), you MUST:
1. Present a brief summary of what you plan to do (files to create/modify, content type, scope)
2. Wait for explicit user approval before proceeding
3. Only execute after receiving confirmation (e.g., "yes", "go", "do it", "approved")
Read-only operations (listing files, reading, searching, status checks) do not require confirmation.
This applies to ALL write paths: direct file writes, subagent spawns for generation, bash commands that modify state.`

// GetSystemPrompt returns the chat-mode system prompt without project
// context. See Compose.
func GetSystemPrompt() string {
	return Compose(ComposeOptions{Mode: ModeChat})
}

// GetSystemPromptWithContext returns the chat-mode system prompt with
// project context and git snapshot appended. See Compose.
func GetSystemPromptWithContext(grimoireContent string, gitSnapshot string) string {
	return Compose(ComposeOptions{
		Mode:           ModeChat,
		ProjectContext: grimoireContent,
		GitSnapshot:    gitSnapshot,
	})
}

// GetContentPrompt returns a prompt tailored for content generation.
func GetContentPrompt(platform, format, tone, topic string) string {
	basePrompt := GetSystemPrompt()

	var contentAddendum strings.Builder
	contentAddendum.WriteString("\n\nCONTENT GENERATION MODE:\n")

	if platform != "" {
		switch platform {
		case "twitter":
			contentAddendum.WriteString("- Optimize for Twitter/X - include relevant hashtags, emojis, engagement hooks, and keep it shareable.\n")
		case "tiktok":
			contentAddendum.WriteString("- Optimize for TikTok - make it trendy, catchy, relatable, and optimized for the TikTok audience.\n")
		case "youtube":
			contentAddendum.WriteString("- Optimize for YouTube - write engaging descriptions or titles that encourage clicks and watches.\n")
		case "discord":
			contentAddendum.WriteString("- Optimize for Discord - use conversational tone with Discord-friendly formatting and emojis.\n")
		}
	}

	if format != "" {
		switch format {
		case "short":
			contentAddendum.WriteString("- Generate SHORT content (around 280 characters) - concise, punchy, and impactful.\n")
		case "long":
			contentAddendum.WriteString("- Generate LONG content (around 5000 characters) - detailed, comprehensive, and engaging.\n")
		case "general":
			contentAddendum.WriteString("- Generate flexible-length content - adapt the length to best suit the request.\n")
		}
	}

	if tone != "" {
		contentAddendum.WriteString(fmt.Sprintf("- Tone: %s\n", tone))
	}

	if topic != "" {
		contentAddendum.WriteString(fmt.Sprintf("- Topic/Subject: %s\n", topic))
	}

	return basePrompt + contentAddendum.String()
}
