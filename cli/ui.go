package main

import (
	_ "embed"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/gen2brain/beeep"
)

//go:embed logo.png
var icon []byte

type model struct {
	stateChan    chan UIState
	currentState UIState
	tickInc      uint32
	timeStarted  time.Time
}

const timerLength = time.Minute * 10

func (m model) listenForUpdate() tea.Cmd {
	return func() tea.Msg {
		msg := <-m.stateChan
		return msg
	}
}

type UpdateSpinnerMsg time.Time

func doSpinnerTick() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg { return UpdateSpinnerMsg(t) })
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.listenForUpdate(), doSpinnerTick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}

	case UIState:
		m.currentState = msg

		if m.currentState.status == "error" {
			// race condition hack, just make sure we render a frame
			return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
				return tea.Quit()
			})
		} else if m.currentState.status == "timerExpired" {
			return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
				return tea.Quit()
			})
		} else if m.currentState.status == "flashing" {
			m.timeStarted = time.Now()

		} else if m.currentState.status == "flashSuccess" {

			// ignore error
			_ = beeep.Notify("Flash success!", lipgloss.Sprintf("UART forwarding to %s", m.currentState.ptyName), icon)

		}

		return m, m.listenForUpdate()
	case UpdateSpinnerMsg:
		m.tickInc++

		return m, doSpinnerTick()
	}

	return m, nil
}

func (m model) View() tea.View {
	s := ""

	switch m.currentState.status {
	case "error":
		container := lipgloss.NewStyle().
			Padding(1).
			Foreground(lipgloss.Color("#f38ba8")).
			Background(lipgloss.Color("#11111b"))

		errorBody := container.Render(m.currentState.errorMessage)

		errorLabel := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#11111b")).
			Background(lipgloss.Color("#f38ba8")).
			Bold(true).
			Width(lipgloss.Width(errorBody))

		s += lipgloss.Sprintf(errorLabel.Render(" error:"))
		s += "\n"
		s += lipgloss.Sprintf(errorBody)
		s += "\n"
	case "warning":
		fallthrough
	case "flashSuccess":
		check := lipgloss.NewStyle().
			Background(lipgloss.Color("#a6e3a1")).
			Foreground(lipgloss.Color("#11111b"))
		text := lipgloss.NewStyle()

		s += lipgloss.Sprintf(check.Render(" :-) "))
		s += lipgloss.Sprintf(text.Render(" flashed bitstream "))

		s += "\n"

		info := lipgloss.NewStyle().
			Background(lipgloss.Color("#94e2d5")).
			Foreground(lipgloss.Color("#11111b"))

		s += lipgloss.Sprintf(info.Render(" !!! "))
		s += lipgloss.Sprintf(" FPGA uart forwarding to %s", m.currentState.ptyName)

		s += "\n"

		timeLeft := timerLength - time.Now().Sub(m.timeStarted)

		if timeLeft > 0 {
			s += lipgloss.Sprintf("time remaining: %02v:%02v", int32(timeLeft.Minutes()), int32(timeLeft.Seconds())%60)
		} else {
			timeLeft = timeLeft.Abs()
			s += lipgloss.Sprintf("overtime lasted for: %02v:%02v", int32(timeLeft.Minutes()), int32(timeLeft.Seconds())%60)
		}

		if m.currentState.status == "warning" {

			s += "\n"

			info := lipgloss.NewStyle().
				Background(lipgloss.Color("#f9e2af")).
				Foreground(lipgloss.Color("#11111b"))

			s += lipgloss.Sprintf(info.Render(" !!! "))
			s += lipgloss.Sprintf(" warning: %s", m.currentState.errorMessage)
		}

		s += "\n"

	case "flashing":
		load := lipgloss.NewStyle().
			Background(lipgloss.Color("#f9e2af")).
			Foreground(lipgloss.Color("#11111b"))
		text := lipgloss.NewStyle()

		spinner := []string{"-", "\\", "|", "/"}
		s += lipgloss.Sprintf(load.Render(" ", spinner[m.tickInc%uint32(len(spinner))], " "))
		s += lipgloss.Sprintf(text.Render(" flashing bitstream "))
	case "timerExpired":
		check := lipgloss.NewStyle().
			Background(lipgloss.Color("#a6e3a1")).
			Foreground(lipgloss.Color("#11111b"))
		text := lipgloss.NewStyle()

		s += lipgloss.Sprintf(check.Render(" :-) "))
		s += lipgloss.Sprintf(text.Render(" flashed bitstream "))

		s += "\n"

		info := lipgloss.NewStyle().
			Background(lipgloss.Color("#94e2d5")).
			Foreground(lipgloss.Color("#11111b"))

		s += lipgloss.Sprintf(info.Render(" !!! "))
		s += lipgloss.Sprintf(" FPGA uart forwarded to %s", m.currentState.ptyName)

		s += "\n"

		s += lipgloss.Sprintf(check.Render(" :-) "))
		s += lipgloss.Sprintf(text.Render(" connection succesfully expired"))

		s += "\n"

	case "waiting":
		container := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#fff")).
			Background(lipgloss.Color("#11111b"))

		spinner := []string{"...", " ..", "  .", "   ", ".  ", ".. "}
		s += lipgloss.Sprintf(container.Render("waiting%s currently #%d out of %d"), spinner[m.tickInc%uint32(len(spinner))], m.currentState.currentPlace, m.currentState.connectedClients)

		s += "\n"

	default:
		s += m.currentState.status
	}

	return tea.NewView(s)
}
