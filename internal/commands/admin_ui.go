package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Chang-LL/agent-root-broker/internal/broker"
)

type adminPalette struct {
	heading string
	label   string
	command string
	muted   string
	warning string
	once    string
	broader string
	deny    string
	prompt  string
	error   string
}

type adminRenderer struct {
	config  adminUIConfig
	palette adminPalette
	ansi    bool
	width   int
}

func newAdminRenderer(config adminUIConfig, ansi bool, width int) adminRenderer {
	if width < 48 {
		width = 48
	}
	return adminRenderer{config: config, palette: adminPaletteFor(config.Theme), ansi: ansi, width: width}
}

func adminPaletteFor(theme string) adminPalette {
	switch theme {
	case adminThemeMono:
		return adminPalette{
			heading: "\x1b[1m", label: "\x1b[1m", command: "\x1b[1m", muted: "\x1b[2m",
			warning: "\x1b[1m", once: "\x1b[1m", broader: "\x1b[1m", deny: "\x1b[1m",
			prompt: "\x1b[1m", error: "\x1b[1m",
		}
	case adminThemeHighContrast:
		return adminPalette{
			heading: "\x1b[1;96m", label: "\x1b[1;97m", command: "\x1b[1;97m", muted: "\x1b[90m",
			warning: "\x1b[1;93m", once: "\x1b[1;92m", broader: "\x1b[1;93m", deny: "\x1b[1;91m",
			prompt: "\x1b[1;97m", error: "\x1b[1;91m",
		}
	default:
		return adminPalette{
			heading: "\x1b[1;34m", label: "\x1b[1m", command: "\x1b[1m", muted: "\x1b[2m",
			warning: "\x1b[1;33m", once: "\x1b[1;32m", broader: "\x1b[1;33m", deny: "\x1b[1;31m",
			prompt: "\x1b[1m", error: "\x1b[1;31m",
		}
	}
}

func (renderer adminRenderer) style(style, value string) string {
	if !renderer.ansi || style == "" {
		return value
	}
	return style + value + "\x1b[0m"
}

func (renderer adminRenderer) pending(item broker.PendingView) string {
	quoted := make([]string, len(item.Command.Argv))
	for index, arg := range item.Command.Argv {
		quoted[index] = shellQuote(arg)
	}

	var output strings.Builder
	fmt.Fprintf(&output, "%s  %s\n", renderer.style(renderer.palette.heading, "==> Root request"), renderer.style(renderer.palette.muted, item.ID))
	if renderer.config.Density == adminDensityComfortable {
		output.WriteByte('\n')
	}
	output.WriteString(renderer.style(renderer.palette.label, "Command"))
	output.WriteByte('\n')
	for _, line := range wrapAdminCommand(quoted, renderer.width, renderer.config.WrapCommand) {
		output.WriteString(renderer.style(renderer.palette.command, line))
		output.WriteByte('\n')
	}
	if renderer.config.Density == adminDensityComfortable {
		output.WriteByte('\n')
	}
	if len(item.Command.Risks) == 0 {
		output.WriteString(renderer.style(renderer.palette.muted, "Risk hints: none detected"))
		output.WriteByte('\n')
	} else {
		for _, risk := range item.Command.Risks {
			output.WriteString(renderer.style(renderer.palette.warning, "Warning: "+strings.ReplaceAll(risk, "-", " ")))
			output.WriteByte('\n')
		}
	}
	if renderer.config.Density == adminDensityComfortable {
		output.WriteByte('\n')
	}
	output.WriteString(renderer.style(renderer.palette.label, "Context"))
	output.WriteByte('\n')
	renderer.contextLine(&output, "session", item.SessionID)
	renderer.contextLine(&output, "turn", strconv.Itoa(item.Turn))
	renderer.contextLine(&output, "cwd", item.Command.CWD)
	renderer.contextLine(&output, "timeout", strconv.Itoa(item.Command.TimeoutSeconds)+"s")
	if renderer.config.ShowHash {
		renderer.contextLine(&output, "hash", item.Command.Hash)
	}
	return strings.TrimRight(output.String(), "\n")
}

func (renderer adminRenderer) contextLine(output *strings.Builder, label, value string) {
	const labelWidth = 9
	padding := labelWidth - len(label)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(output, "  %s %s\n", renderer.style(renderer.palette.muted, label+strings.Repeat(" ", padding)), value)
}

func wrapAdminCommand(argv []string, width int, enabled bool) []string {
	const firstIndent = "  "
	const continuationIndent = "    "
	if len(argv) == 0 {
		return []string{firstIndent}
	}
	if !enabled {
		return []string{firstIndent + strings.Join(argv, " ")}
	}
	lines := make([]string, 0, 2)
	line := firstIndent
	indent := firstIndent
	for _, arg := range argv {
		separator := ""
		if len(line) > len(indent) {
			separator = " "
		}
		if len(line)+len(separator)+len(arg) > width && len(line) > len(indent) {
			lines = append(lines, line)
			indent = continuationIndent
			line = indent + arg
			continue
		}
		line += separator + arg
	}
	return append(lines, line)
}

func (renderer adminRenderer) approvalPrompt(turn int, full bool) string {
	if !full {
		return renderer.style(renderer.palette.prompt, "Choice: ")
	}
	if renderer.config.Density == adminDensityCompact {
		return fmt.Sprintf("Approve %s once, %s message (turn %d), %s session; %s deny; [l] later; [q] quit? ",
			renderer.style(renderer.palette.once, "[c]"), renderer.style(renderer.palette.broader, "[m]"), turn,
			renderer.style(renderer.palette.broader, "[s]"), renderer.style(renderer.palette.deny, "[d]"))
	}
	return renderer.style(renderer.palette.label, "Approve") + "\n" +
		renderer.choiceLine("[c] once", "only this command", renderer.palette.once) +
		renderer.choiceLine("[m] message", fmt.Sprintf("remaining requests in turn %d", turn), renderer.palette.broader) +
		renderer.choiceLine("[s] session", "remaining requests in this session", renderer.palette.broader) +
		renderer.choiceLine("[d] deny", "reject this request", renderer.palette.deny) +
		"  [l] later          leave it pending\n" +
		"  [q] quit           stop watching\n\n" +
		renderer.style(renderer.palette.prompt, "Choice: ")
}

func (renderer adminRenderer) choiceLine(label, description, style string) string {
	const labelWidth = 18
	padding := labelWidth - len(label)
	if padding < 1 {
		padding = 1
	}
	return "  " + renderer.style(style, label) + strings.Repeat(" ", padding) + description + "\n"
}

func (renderer adminRenderer) invalidChoice() string {
	return renderer.style(renderer.palette.error, "Invalid choice.") + " Enter c, m, s, d, l, or q."
}

func (renderer adminRenderer) approved(requestID, scope string) string {
	return renderer.style(renderer.palette.once, "Approved") + fmt.Sprintf(" %s for %s scope.", requestID, scope)
}

func (renderer adminRenderer) denied(requestID string) string {
	return renderer.style(renderer.palette.deny, "Denied") + " " + requestID + "."
}
