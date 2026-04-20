package setup

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Syfra3/ancora/internal/embed"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WizardState represents the current screen in the setup wizard.
type WizardState int

const (
	StateWelcome WizardState = iota
	StateModeChoice
	StateModelChoice
	StateDownloading
	StateLlamaCppCheck
	StateInstallLlamaCpp
	StatePluginChoice
	StateSuccess
	StateError
)

// WizardModel is the Bubble Tea model for the setup wizard.
type WizardModel struct {
	state             WizardState
	err               error
	downloader        *Downloader
	progress          progress.Model
	downloaded        int64
	total             int64
	speed             float64
	quitting          bool
	llamaCppPath      string
	llamaCppSkipped   bool
	installingLlama   bool
	pluginChoice      string // "opencode", "claude-code", "skip"
	pluginChoiceIndex int    // 0=opencode, 1=claude-code, 2=skip
	integrationMode   string
	modeChoiceIndex   int    // 0=ancora-only, 1=ancora+vela
	menuChoiceIndex   int    // For main menu when integrated with TUI
	version           string // Version string to display
}

// NewWizard creates a new setup wizard model.
func NewWizard() WizardModel {
	return WizardModel{
		state:           StateWelcome,
		progress:        progress.New(progress.WithDefaultGradient()),
		integrationMode: ModeAncoraOnly,
	}
}

// NewWizardWithVersion creates a new setup wizard model with version info.
func NewWizardWithVersion(ver string) WizardModel {
	return WizardModel{
		state:           StateWelcome,
		progress:        progress.New(progress.WithDefaultGradient()),
		integrationMode: ModeAncoraOnly,
		version:         ver,
	}
}

// Init initializes the wizard (Bubble Tea lifecycle).
func (m WizardModel) Init() tea.Cmd {
	return nil
}

// progressMsg is sent when download progress updates.
type progressMsg DownloadProgress

// downloadCompleteMsg is sent when download finishes successfully.
type downloadCompleteMsg struct{}

// downloadErrorMsg is sent when download fails.
type downloadErrorMsg struct{ err error }

// llamaCppCheckMsg is sent after checking for llama.cpp.
type llamaCppCheckMsg struct {
	found bool
	path  string
}

// llamaCppInstallMsg is sent after attempting llama.cpp installation.
type llamaCppInstallMsg struct {
	success bool
	path    string
	err     error
}

// pluginInstallMsg is sent after plugin installation.
type pluginInstallMsg struct {
	success bool
	agent   string
	err     error
}

// Update handles messages and state transitions.
func (m WizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "enter", " ":
			switch m.state {
			case StateWelcome:
				m.state = StateModeChoice
				return m, nil

			case StateModeChoice:
				if m.modeChoiceIndex == 0 {
					m.integrationMode = ModeAncoraOnly
				} else {
					m.integrationMode = ModeAncoraVela
				}
				if err := SaveIntegrationState(IntegrationState{Mode: m.integrationMode, UpdatedBy: "ancora"}); err != nil {
					m.err = err
					m.state = StateError
					return m, nil
				}
				m.state = StateModelChoice
				return m, nil

			case StateModelChoice:
				// User chose to install — start download
				m.state = StateDownloading
				destPath := filepath.Join(embed.ModelInstallPath(), embed.ModelFileName)
				m.downloader = NewDownloader(destPath)
				return m, tea.Batch(
					m.listenForProgress(),
					m.startDownload(),
				)

			case StateLlamaCppCheck:
				// User chose to install llama.cpp
				m.state = StateInstallLlamaCpp
				return m, m.installLlamaCpp()

			case StatePluginChoice:
				// Move to success
				m.state = StateSuccess
				return m, nil

			case StateSuccess:
				// Done — exit
				return m, tea.Quit
			}

		case "s", "n":
			switch m.state {
			case StateModelChoice:
				// Skip model, go to llama.cpp check
				m.state = StateLlamaCppCheck
				return m, m.checkLlamaCpp()

			case StateLlamaCppCheck:
				// Skip llama.cpp install, go to plugin choice
				m.llamaCppSkipped = true
				m.state = StatePluginChoice
				return m, nil

			case StatePluginChoice:
				// Skip plugins, go to success
				m.state = StateSuccess
				return m, nil
			}

		case "up", "down":
			if m.state == StateModeChoice {
				switch msg.String() {
				case "up":
					m.modeChoiceIndex = (m.modeChoiceIndex + 1) % 2
				case "down":
					m.modeChoiceIndex = (m.modeChoiceIndex + 1) % 2
				}
				return m, nil
			}
			if m.state == StatePluginChoice {
				switch msg.String() {
				case "up":
					m.pluginChoiceIndex = (m.pluginChoiceIndex + 2) % 3
				case "down":
					m.pluginChoiceIndex = (m.pluginChoiceIndex + 1) % 3
				}
				return m, nil
			}

		case "1", "2", "3":
			if m.state == StatePluginChoice {
				m.pluginChoiceIndex = int(msg.String()[0] - '1')
				switch m.pluginChoiceIndex {
				case 0:
					m.pluginChoice = "opencode"
					return m, m.installPlugin("opencode")
				case 1:
					m.pluginChoice = "claude-code"
					return m, m.installPlugin("claude-code")
				case 2:
					m.pluginChoice = "skip"
					m.state = StateSuccess
					return m, nil
				}
			}
		}

	case progressMsg:
		if msg.Error != nil {
			m.err = msg.Error
			m.state = StateError
			return m, nil
		}
		m.downloaded = msg.BytesDownloaded
		m.total = msg.TotalBytes
		m.speed = msg.Speed
		if msg.Complete {
			// Model download complete, check llama.cpp next
			m.state = StateLlamaCppCheck
			return m, m.checkLlamaCpp()
		}
		return m, m.listenForProgress()

	case llamaCppCheckMsg:
		if msg.found {
			m.llamaCppPath = msg.path
			m.state = StatePluginChoice
			return m, nil
		}
		// Not found, offer to install
		m.state = StateLlamaCppCheck
		return m, nil

	case llamaCppInstallMsg:
		if msg.success {
			m.llamaCppPath = msg.path
			m.state = StatePluginChoice
			return m, nil
		}
		// Installation failed
		m.err = msg.err
		m.state = StateError
		return m, nil

	case pluginInstallMsg:
		if msg.success {
			m.state = StateSuccess
			return m, nil
		}
		// Non-fatal: plugin install failed but we can continue
		m.err = msg.err
		m.state = StateSuccess
		return m, nil

	case tea.WindowSizeMsg:
		m.progress.Width = msg.Width - 4
		if m.progress.Width > 100 {
			m.progress.Width = 100
		}
		return m, nil
	}

	return m, nil
}

// listenForProgress waits for the next progress update from the downloader.
func (m WizardModel) listenForProgress() tea.Cmd {
	if m.downloader == nil {
		return nil
	}
	return func() tea.Msg {
		p, ok := <-m.downloader.Progress
		if !ok {
			return nil
		}
		return progressMsg(p)
	}
}

// startDownload begins the download in a goroutine.
func (m WizardModel) startDownload() tea.Cmd {
	return func() tea.Msg {
		if m.downloader == nil {
			return downloadErrorMsg{err: fmt.Errorf("downloader not initialized")}
		}
		if err := m.downloader.Download(); err != nil {
			return downloadErrorMsg{err: err}
		}
		return downloadCompleteMsg{}
	}
}

// checkLlamaCpp checks if llama-embedding CLI is available.
func (m WizardModel) checkLlamaCpp() tea.Cmd {
	return func() tea.Msg {
		found, path := CheckLlamaCpp()
		return llamaCppCheckMsg{
			found: found,
			path:  path,
		}
	}
}

// installLlamaCpp attempts to install llama.cpp.
func (m WizardModel) installLlamaCpp() tea.Cmd {
	return func() tea.Msg {
		path, err := InstallLlamaCpp()
		if err != nil {
			return llamaCppInstallMsg{
				success: false,
				err:     err,
			}
		}
		return llamaCppInstallMsg{
			success: true,
			path:    path,
		}
	}
}

// installPlugin installs the specified agent plugin.
func (m WizardModel) installPlugin(agent string) tea.Cmd {
	return func() tea.Msg {
		result, err := Install(agent)
		if err != nil {
			return pluginInstallMsg{
				success: false,
				agent:   agent,
				err:     err,
			}
		}
		return pluginInstallMsg{
			success: true,
			agent:   result.Agent,
		}
	}
}

// View renders the current state.
func (m WizardModel) View() string {
	if m.quitting && m.state != StateSuccess {
		return "Setup cancelled.\n"
	}

	switch m.state {
	case StateWelcome:
		return m.viewWelcome()
	case StateModeChoice:
		return m.viewModeChoice()
	case StateModelChoice:
		return m.viewModelChoice()
	case StateDownloading:
		return m.viewDownloading()
	case StateLlamaCppCheck:
		return m.viewLlamaCppCheck()
	case StateInstallLlamaCpp:
		return m.viewInstallLlamaCpp()
	case StatePluginChoice:
		return m.viewPluginChoice()
	case StateSuccess:
		return m.viewSuccess()
	case StateError:
		return m.viewError()
	}

	return ""
}

var (
	// Syfra/Ancora color palette (matching main TUI)
	colorLavender = lipgloss.Color("#C8B6FF")
	colorMint     = lipgloss.Color("#B4FFDD")
	colorMintMid  = lipgloss.Color("#CEE7F0")
	colorSubtext  = lipgloss.Color("#8a8a8e")
	colorText     = lipgloss.Color("#e0e0e2")
	colorRed      = lipgloss.Color("#FF9EB8")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorLavender).
			Padding(0, 1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			Padding(0, 1)

	boxStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Width(60)

	successStyle = lipgloss.NewStyle().
			Foreground(colorMint).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	hintStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			Italic(true)
)

// renderAsciiLogo renders the ASCII art with gradient colors (matching main TUI)
func renderAsciiLogo() string {
	logoText := []string{
		` █████╗ ███╗   ██╗ ██████╗ ██████╗ ██████╗  █████╗ `,
		`██╔══██╗████╗  ██║██╔════╝██╔═══██╗██╔══██╗██╔══██╗`,
		`███████║██╔██╗ ██║██║     ██║   ██║██████╔╝███████║`,
		`██╔══██║██║╚██╗██║██║     ██║   ██║██╔══██╗██╔══██║`,
		`██║  ██║██║ ╚████║╚██████╗╚██████╔╝██║  ██║██║  ██║`,
		`╚═╝  ╚═╝╚═╝  ╚═══╝ ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝`,
	}

	// Gradient colors (Syfra palette: Lavender to Mint)
	colors := []lipgloss.Color{
		colorLavender, colorLavender, colorMintMid,
		colorMint, colorMint, colorMint,
	}

	var b strings.Builder
	for i, line := range logoText {
		b.WriteString(lipgloss.NewStyle().Foreground(colors[i]).Bold(true).Render(line) + "\n")
	}

	return b.String()
}

func (m WizardModel) viewWelcome() string {
	var b strings.Builder

	versionStr := ""
	if m.version != "" {
		versionStr = " — " + m.version
	}

	b.WriteString(boxStyle.Render(
		renderAsciiLogo() + "\n" +
			titleStyle.Render("Ancora Setup"+versionStr) + "\n\n" +
			"Welcome to Ancora Memory System\n\n" +
			"This wizard will configure:\n" +
			"• Database (already set up ✓)\n" +
			"• Embedding model\n" +
			"• llama.cpp CLI\n" +
			"• AI agent plugin\n\n" +
			hintStyle.Render("[press Enter to continue, q to quit]"),
	))

	return b.String()
}

func (m WizardModel) viewModelChoice() string {
	var b strings.Builder

	b.WriteString(boxStyle.Render(
		titleStyle.Render("Embedding Model") + "\n\n" +
			"Ancora uses two search methods:\n\n" +
			"1. Keyword (FTS5) - Always available\n" +
			"2. Semantic (embeddings) - Finds similar\n" +
			"   concepts even with different words\n\n" +
			"Example: searching \"auth\" finds \"JWT login\"\n\n" +
			"Install nomic-embed-text-v1.5? (~180 MB)\n\n" +
			hintStyle.Render("[Enter = install, s/n = skip, q = quit]"),
	))

	return b.String()
}

func (m WizardModel) viewModeChoice() string {
	var b strings.Builder
	items := []string{"Ancora only", "Ancora + Vela"}

	var itemsStr strings.Builder
	for i, item := range items {
		if i == m.modeChoiceIndex {
			itemsStr.WriteString("  ▶ " + item + "\n")
		} else {
			itemsStr.WriteString("    " + item + "\n")
		}
	}

	content := titleStyle.Render("Integration Mode") + "\n\n" +
		"Choose how Ancora should expose retrieval:\n\n" +
		itemsStr.String() +
		"Ancora only keeps memory tools only.\n" +
		"Ancora + Vela forwards vela_* retrieval tools through Ancora MCP.\n\n" +
		hintStyle.Render("[↑/↓ to select, Enter to confirm, q = quit]")

	b.WriteString(boxStyle.Render(content))
	return b.String()
}

func (m WizardModel) viewDownloading() string {
	var b strings.Builder

	percentage := 0.0
	if m.total > 0 {
		percentage = float64(m.downloaded) / float64(m.total)
	}

	speedStr := ""
	if m.speed > 0 {
		speedStr = fmt.Sprintf("  Speed: %s", FormatSpeed(m.speed))
	}

	eta := ""
	if m.speed > 0 && m.total > 0 {
		remaining := float64(m.total - m.downloaded)
		etaSeconds := remaining / m.speed
		eta = fmt.Sprintf("  ETA: %s", formatDuration(time.Duration(etaSeconds)*time.Second))
	}

	content := titleStyle.Render("Downloading") + "\n\n" +
		"nomic-embed-text-v1.5.Q4_K_M.gguf\n\n" +
		m.progress.ViewAs(percentage) + "\n" +
		fmt.Sprintf("%s / %s", FormatBytes(m.downloaded), FormatBytes(m.total)) +
		speedStr + eta

	b.WriteString(boxStyle.Render(content))

	return b.String()
}

func (m WizardModel) viewLlamaCppCheck() string {
	var b strings.Builder

	content := titleStyle.Render("llama.cpp CLI") + "\n\n" +
		"llama-embedding not found in PATH.\n\n" +
		"This CLI is required to generate embeddings\n" +
		"for semantic search.\n\n" +
		"Install via Homebrew? (macOS/Linux only)\n\n" +
		hintStyle.Render("[Enter = install, s/n = skip, q = quit]")

	b.WriteString(boxStyle.Render(content))
	return b.String()
}

func (m WizardModel) viewInstallLlamaCpp() string {
	var b strings.Builder

	content := titleStyle.Render("Installing llama.cpp") + "\n\n" +
		"Running: brew install llama.cpp\n\n" +
		"This may take a few minutes...\n\n" +
		hintStyle.Render("[please wait]")

	b.WriteString(boxStyle.Render(content))
	return b.String()
}

func (m WizardModel) viewPluginChoice() string {
	var b strings.Builder

	items := []string{"OpenCode", "Claude Code", "Skip"}
	selected := m.pluginChoiceIndex

	var itemsStr strings.Builder
	for i, item := range items {
		if i == selected {
			itemsStr.WriteString("  ▶ " + item + "\n")
		} else {
			itemsStr.WriteString("    " + item + "\n")
		}
	}

	content := titleStyle.Render("Plugin Installation") + "\n\n" +
		"Install Ancora plugin for your AI agent?\n\n" +
		itemsStr.String() +
		"This enables persistent memory across sessions.\n\n" +
		hintStyle.Render("[↑/↓ to select, Enter to confirm, q = quit]")

	b.WriteString(boxStyle.Render(content))
	return b.String()
}

func (m WizardModel) viewSuccess() string {
	var b strings.Builder

	// Build summary of what was installed
	var summary strings.Builder
	summary.WriteString(successStyle.Render("✓ Setup Complete") + "\n\n")

	// Model status
	summary.WriteString(fmt.Sprintf("✓ Integration mode: %s\n\n", m.integrationMode))

	if m.downloaded > 0 {
		destPath := filepath.Join(embed.ModelInstallPath(), embed.ModelFileName)
		summary.WriteString("✓ Embedding model installed\n")
		summary.WriteString(fmt.Sprintf("  %s\n\n", destPath))
	} else {
		summary.WriteString("• Embedding model: skipped\n\n")
	}

	// llama.cpp status
	if m.llamaCppPath != "" {
		summary.WriteString("✓ llama.cpp installed\n")
		summary.WriteString(fmt.Sprintf("  %s\n\n", m.llamaCppPath))
	} else if m.llamaCppSkipped {
		summary.WriteString("• llama.cpp: skipped\n\n")
	}

	// Plugin status
	if m.pluginChoice != "" && m.pluginChoice != "skip" {
		summary.WriteString(fmt.Sprintf("✓ %s plugin installed\n\n", m.pluginChoice))
	} else {
		summary.WriteString("• Plugin: skipped\n\n")
	}

	summary.WriteString("Run `ancora doctor` to verify.\n\n")
	summary.WriteString(hintStyle.Render("[press Enter or q to exit]"))

	b.WriteString(boxStyle.Render(summary.String()))
	return b.String()
}

func (m WizardModel) viewError() string {
	var b strings.Builder

	errMsg := "unknown error"
	if m.err != nil {
		errMsg = m.err.Error()
	}

	b.WriteString(boxStyle.Render(
		errorStyle.Render("✗ Setup Failed") + "\n\n" +
			"Error:\n" +
			errMsg + "\n\n" +
			hintStyle.Render("[press q to exit]"),
	))

	return b.String()
}

// formatDuration formats a duration as human-readable (e.g. "5m 20s", "45s").
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "< 1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", minutes, seconds)
}
