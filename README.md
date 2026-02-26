# llm-usage

TUI for tracking your Claude, Codex, and Kimi rate limit usage. Built with [Charm](https://charm.sh).

![Go](https://img.shields.io/badge/Go-1.23-blue)

![demo](demo.gif)

## Install

```bash
go install github.com/tau/llm-usage@latest
```

Or build from source:

```bash
git clone git@github.com:tau/llm-usage.git
cd llm-usage
go install .
```

## Usage

```bash
llm-usage
```

That's it. It reads your OAuth token from the macOS Keychain automatically (requires being logged into [Claude Code](https://docs.anthropic.com/en/docs/claude-code)).

### Compact mode

For tmux statusbars or scripts:

```bash
llm-usage --compact
# claude:5h:45%,7d:29% codex:5h:12%,7d:8% tok:1.2M
```

```bash
# tmux example
set -g status-right '#(llm-usage --compact)'
```

### Environment variable

On Linux or if you want to use a specific token:

```bash
export CLAUDE_OAUTH_TOKEN="sk-ant-oat01-..."
llm-usage
```

## Keybindings

| Key | Action |
|-----|--------|
| `q` | Quit |
| `r` | Refresh |

Hover over a bar to see the exact utilization percentage.

## Requirements

- macOS (for Keychain auto-detection) or `CLAUDE_OAUTH_TOKEN` env var
- Logged into Claude Code (`claude`) for Claude rate limits
- [Codex CLI](https://github.com/openai/codex) installed for Codex rate limits  
- [Kimi Code CLI](https://github.com/MoonshotAI/kimi-cli) installed for Kimi token tracking (Kimi doesn't expose local rate limits)
