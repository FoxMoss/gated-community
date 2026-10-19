package main

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type model struct {
	stateChan    chan UIState
	currentState UIState
	tickInc      uint32
}

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

		active := lipgloss.NewStyle().
			Background(lipgloss.Color("#b4befe")).
			Foreground(lipgloss.Color("#11111b"))

		spinner := []string{".  ", ".. ", "...", " ..", "  .", "   "}
		s += lipgloss.Sprintf(active.Render("", spinner[m.tickInc%uint32(len(spinner))], ""))
		s += lipgloss.Sprintf(text.Render(" forwarding data \n"))
	case "flashing":
		load := lipgloss.NewStyle().
			Background(lipgloss.Color("#f9e2af")).
			Foreground(lipgloss.Color("#11111b"))
		text := lipgloss.NewStyle()

		spinner := []string{"-", "\\", "|", "/"}
		s += lipgloss.Sprintf(load.Render(" ", spinner[m.tickInc%uint32(len(spinner))], " "))
		s += lipgloss.Sprintf(text.Render(" flashing bitstream "))
	default:
		s += m.currentState.status
	}

	return tea.NewView(s)
}
