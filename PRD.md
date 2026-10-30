# Product Requirement Document (PRD)

## Project Name: `prompt-engine-cli` (Working Title)
**Target Execution Language:** Go (Golang)  
**Primary Integration (MVP):** Claude Code CLI  
**Document Version:** 1.2.3  
**Last Updated:** 2026-10-30  
**Appendices:** [Appendix 1: Session Context and Automatic Enhancement](docs/PRD-APPENDIX-1.md) (Phase 4.5) · [Appendix 2: Rewrite Styles](docs/PRD-APPENDIX-2.md) (built)  

---

## 1. Executive Summary & Objective

`prompt-engine-cli` is a high-performance terminal wrapper and real-time prompt enhancement proxy designed for AI-driven command-line tools. Sitting directly between the developer's keyboard input and the underlying CLI session (starting with **Claude Code**), it dynamically analyzes, expands, and completes developer prompts in real time using an ultra-fast local LLM backend.

The primary goal is to lower developer friction and improve the quality of AI interactions inside terminal sessions without introducing noticeable typing latency.

---

## 2. Core Value Proposition & Principles

* **Zero-Latency UX:** Auto-suggestions and prompt inline completions must feel instantaneous (<100ms response window).
* **Local-First & Private:** Real-time prompt polishing happens entirely on-device via local LLM engines (e.g., Ollama).
* **Transparent Interception:** Seamless drop-in replacement for `claude` CLI via alias or sub-command execution.
* **Context-Aware Enhancement:** Automatically infers developer intent, fixing typos, expanding shorthand phrases, and optionally injecting relevant environment context or predefined guidelines.

---

## 3. High-Level Architecture

```
+---------------------------------------------------------------------------------------+
|                                  Developer Terminal                                   |
+---------------------------------------------------------------------------------------+
                                    |
                               (Keystrokes)
                                    v
+---------------------------------------------------------------------------------------+
|                         `prompt-engine-cli` (Go TUI Wrapper)                          |
|                                                                                       |
|  +------------------------+  +-------------------------------+  +-------------------+  |
|  |  Interactive PTY View  |  | Local LLM Daemon (Ollama)     |  | Side Diff Panel   |  |
|  |  & Input Buffer        |<---| (Qwen2.5-1.5B / Llama 3.2 1B)  |--->| (Raw vs Expanded  |  |
|  |  (Bubbletea/Lipgloss)  |  +-------------------------------+  | Visual Highlights)|  |
|  +------------------------+                                     +-------------------+  |
|                                                                                       |
+---------------------------------------------------------------------------------------+
                                    |
                        (Enriched / Final Prompt)
                                    v
+---------------------------------------------------------------------------------------+
|                       Claude Code CLI Process                                         |
+---------------------------------------------------------------------------------------+
```

---

## 4. MVP Feature Requirements

### 4.1. Terminal Wrapper & Process Management
* **FR-1.1:** Execute and manage the official `claude` binary in a child standard PTY (pseudoterminal) session.
* **FR-1.2:** Intercept user keystrokes in raw terminal mode before passing them down to the underlying `claude` process.
* **FR-1.3:** Provide a seamless fallback: if `prompt-engine-cli` encounters an error, typing passes directly to `claude` without blocking the user.

### 4.2. Local AI Suggestion Engine
* **FR-2.1:** Connect to a local Ollama instance (or HTTP endpoint for local inference engines like `llama.cpp`).
* **FR-2.2:** Send asynchronous, debounced prompt drafts (e.g., after 150ms of typing inactivity) to the local model.
* **FR-2.3:** Provide "Ghost Text" inline auto-completion (dimmed grey text ahead of the cursor).
* **FR-2.4:** Support keybindings:
  * `Tab` or `Right Arrow`: Accept current inline suggestion.
  * `Ctrl+E`: Trigger full prompt expansion (turns shorthand commands into structured prompts).
  * `Esc`: Dismiss suggestion.

### 4.3. Context Injection (Rule-Based Heuristics)
* **FR-3.1:** Automatically detect git context (e.g., current branch, staged diff summaries) and inject relevant metadata into expanded prompts upon explicit trigger (`Ctrl+E`).

### 4.4. Configuration & Custom System Rules
* **FR-4.1:** Read user settings from `~/.config/prompt-engine/config.yaml` on startup (following XDG Base Directory standards).
* **FR-4.2:** Support static prompt suffix/prefix injection from local template files (e.g., `templates/append.txt` for global guidelines such as "DO" and "DON'T" rules).
* **FR-4.3:** Allow dynamic hot-reloading of configuration files without restarting the session.

### 4.5. Side-Panel Prompt Diff & Learning UI
* **FR-5.1:** Render a side panel (toggleable on the right or left side) showing the original raw prompt versus the enhanced/expanded prompt submitted to Claude Code.
* **FR-5.2:** Highlight additions, corrections, and injected context/rules using visual diff formatting (e.g., green for additions, red for deletions via Lipgloss).
* **FR-5.3:** Support keybindings to toggle panel visibility (e.g., `Ctrl+P` or `Ctrl+D`) and adjust panel width dynamically.
* **FR-5.4:** Log diff metrics to help developers learn effective prompt patterns over time.

---

## 5. Non-Functional Requirements

* **Performance:** Inline ghost-text render latency must be under **100ms**.
* **Binary Distribution:** Single self-contained Go executable (`prompt-engine`).
* **Memory Footprint:** The Go process idle footprint should remain < 30 MB (excluding the local Ollama instance).
* **Cross-Platform Compatibility:** Initial targeting for macOS (Apple Silicon optimized) and Linux.

---

## 6. Target Technology Stack

| Component | Selected Tech | Justification |
| :--- | :--- | :--- |
| **Language** | Go (1.22+) | Native performance, instant startup, fast concurrency model. |
| **TUI / Terminal Controls** | `charmbracelet/bubbletea`, `charmbracelet/lipgloss` | Gold standard for Go terminal UIs and styling. |
| **PTY Management** | `creack/pty` | Robust pseudo-terminal handling in Go. |
| **Local LLM Engine** | Ollama (`qwen2.5:1.5b` / `llama3.2:1b`) | Optimized local inference leveraging Metal / GPU acceleration. |

---

## 7. Configuration Directory Structure

```text
~/.config/prompt-engine/
├── config.yaml          # Core runtime configuration
└── templates/
    ├── system.txt       # System prompt instructions for the local model
    └── append.txt       # Static rules/guidelines automatically appended to final prompts
```

---

## 8. MVP Roadmap & Phase Scope

### Phase 1: Core PTY Wrapper & Interception
* Implement basic PTY spawning of `claude` CLI inside Go.
* Capture keystrokes and render basic custom input buffers.

### Phase 2: Local LLM Integration & Ghost Text
* Wire Go async worker to local Ollama API.
* Render dimmed ghost text completions with `Tab` acceptance.

### Phase 3: Config & Rule Injection
* Implement `~/.config/prompt-engine/` configuration loader (`config.yaml` and `append.txt`).
* Automatically append global rules/guidelines to submittable prompts.

### Phase 4: Prompt Enhancement Shortcuts
* Add `Ctrl+E` trigger to transform shorthand inputs into rich prompts using template system prompts and git heuristics, and render the split UI pane with the Prompt Diff Panel.

### Phase 4.5: Session Context & Automatic Enhancement
* Give the local model session context, and optionally apply enhancement automatically on Enter. See [PRD Appendix 1](docs/PRD-APPENDIX-1.md).
* Then (built): choose who the rewrite's wording is tuned for (the model, people, or both). See [PRD Appendix 2](docs/PRD-APPENDIX-2.md).

### Phase 5 (Post-MVP): Multi-Tool Support
* Abstract integration layers to add support for Gemini CLI, GitHub Copilot CLI, and Aider.