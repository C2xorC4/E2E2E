package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	primaryColor   = lipgloss.Color("#7C3AED")
	secondaryColor = lipgloss.Color("#3B82F6")
	successColor   = lipgloss.Color("#10B981")
	warningColor   = lipgloss.Color("#F59E0B")
	errorColor     = lipgloss.Color("#EF4444")
	mutedColor     = lipgloss.Color("#6B7280")
	bgColor        = lipgloss.Color("#1F2937")
	fgColor        = lipgloss.Color("#F9FAFB")

	// App styles
	AppStyle = lipgloss.NewStyle().
			Padding(0, 1)

	// Header
	HeaderStyle = lipgloss.NewStyle().
			Foreground(fgColor).
			Background(primaryColor).
			Bold(true).
			Padding(0, 2).
			MarginBottom(1)

	// Chat list
	ChatListStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mutedColor).
			Padding(0, 1)

	ChatItemStyle = lipgloss.NewStyle().
			Padding(0, 1)

	ChatItemSelectedStyle = lipgloss.NewStyle().
				Foreground(fgColor).
				Background(primaryColor).
				Padding(0, 1)

	ChatNameStyle = lipgloss.NewStyle().
			Bold(true)

	ChatPreviewStyle = lipgloss.NewStyle().
				Foreground(mutedColor).
				MaxWidth(40)

	UnreadBadgeStyle = lipgloss.NewStyle().
				Foreground(fgColor).
				Background(successColor).
				Padding(0, 1).
				Bold(true)

	// Messages
	MessagePaneStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(mutedColor).
				Padding(0, 1)

	MessageStyle = lipgloss.NewStyle().
			Padding(0, 1).
			MarginBottom(1)

	OwnMessageStyle = lipgloss.NewStyle().
			Foreground(fgColor).
			Background(secondaryColor).
			Padding(0, 2).
			MarginLeft(4)

	OtherMessageStyle = lipgloss.NewStyle().
				Foreground(fgColor).
				Background(lipgloss.Color("#374151")).
				Padding(0, 2).
				MarginRight(4)

	MessageSenderStyle = lipgloss.NewStyle().
				Foreground(primaryColor).
				Bold(true)

	MessageTimeStyle = lipgloss.NewStyle().
				Foreground(mutedColor).
				Italic(true)

	// Input
	InputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 1)

	InputFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(successColor).
				Padding(0, 1)

	// Status bar
	StatusBarStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			MarginTop(1)

	OnlineStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)

	OfflineStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	// Help
	HelpStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true)

	// Dialogs
	DialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1, 2).
			Width(50)

	DialogTitleStyle = lipgloss.NewStyle().
				Foreground(primaryColor).
				Bold(true).
				MarginBottom(1)

	// Errors
	ErrorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)

	// Encryption indicator
	EncryptedStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)
)

func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
