package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	textStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render
)

// https://gist.github.com/sevkin/9798d67b2cb9d07cb05f89f14ba682f8#file-url_open-go-L1

func openURL(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd.exe"
		args = []string{"/c", "rundll32", "url.dll,FileProtocolHandler", strings.ReplaceAll(url, "&", "^&")}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		if isWSL() {
			cmd = "cmd.exe"
			args = []string{"start", url}
		} else {
			cmd = "xdg-open"
			args = []string{url}
		}
	}

	e := exec.Command(cmd, args...)
	err := e.Start()
	if err != nil {
		return err
	}
	err = e.Wait()
	if err != nil {
		return err
	}

	return nil
}

// isWSL checks if the Go program is running inside Windows Subsystem for Linux
func isWSL() bool {
	releaseData, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(releaseData)), "microsoft")
}

type ScreenState int

const (
	ScreenLogin ScreenState = iota
	ScreenWaitingForAuth
	ScreenIdle
	ScreenRunning
)

type model struct {
	Screen  ScreenState
	spinner spinner.Model
}

func initialModel() model {
	var ret = model{
		Screen: ScreenLogin,
	}

	ret.spinner = spinner.New()
	ret.spinner.Style = spinnerStyle
	ret.spinner.Spinner = spinner.Points

	return ret
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}

		switch m.Screen {
		case ScreenLogin:
			switch msg.String() {
			case "enter":
				openURL("https://auth.hackclub.com/oauth/authorize?client_id=55e72dce07e7645a7d66e11a8b18a88d&redirect_uri=http%3A%2F%2Flocalhost%3A8080%2Fauth&response_type=code&scope=openid+name+birthdate")

				m.Screen = ScreenWaitingForAuth

				return m, tea.Batch(m.spinner.Tick)
			}
		}

	case spinner.TickMsg:
		switch m.Screen {
		case ScreenWaitingForAuth:
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	}

	return m, nil
}
func (m model) View() tea.View {
	s := "Gated Community TUI"

	switch m.Screen {
	case ScreenLogin:
		s += "\n\nPress Enter HCA auth in a browser\n"
	case ScreenWaitingForAuth:
		s += fmt.Sprintf("\n\n %s%s\n\n", m.spinner.View(), textStyle(" Waiting for auth..."))
	}

	return tea.NewView(s)
}

func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("error: %v", err)
		os.Exit(1)
	}
}
