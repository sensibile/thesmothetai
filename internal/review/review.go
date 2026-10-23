// Package review is the terminal UI a person uses to approve, reject or
// send back interpretations. It holds no decision rules of its own; every
// verdict goes through store.Decide.
package review

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sensibile/thesmothetai/internal/decision"
	"github.com/sensibile/thesmothetai/internal/evidence"
	"github.com/sensibile/thesmothetai/internal/interpretation"
	"github.com/sensibile/thesmothetai/internal/store"
	"github.com/sensibile/thesmothetai/internal/textdiff"
)

// Run starts the review UI and blocks until the user quits.
func Run(ctx context.Context, st *store.Store, actor string) error {
	p := tea.NewProgram(newModel(ctx, st, actor), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

type mode int

const (
	modeList mode = iota
	modeDetail
	modeReason
)

type tab int

const (
	tabInterpretation tab = iota
	tabText
	tabDiff
	tabCount
)

var tabNames = [tabCount]string{"Interpretation", "License text", "Diff vs previous"}

var (
	styleTitle   = lipgloss.NewStyle().Bold(true)
	styleDim     = lipgloss.NewStyle().Faint(true)
	styleTab     = lipgloss.NewStyle().Padding(0, 1)
	styleTabOn   = styleTab.Reverse(true)
	styleCursor  = lipgloss.NewStyle().Reverse(true)
	styleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleBad     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleGood    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleSection = lipgloss.NewStyle().Bold(true).Underline(true)
)

type detail struct {
	it        interpretation.Interpretation
	text      string
	evidence  []evidence.Evidence
	previous  *store.PreviousApproved
	decisions []decision.Decision
}

type model struct {
	ctx   context.Context
	st    *store.Store
	actor string

	items  []interpretation.Interpretation
	cursor int

	mode   mode
	tab    tab
	cur    *detail
	ack    bool
	action decision.Action

	input textinput.Model
	vp    viewport.Model

	width, height int
	flash         string
}

type (
	listMsg    []interpretation.Interpretation
	detailMsg  *detail
	decidedMsg struct {
		action decision.Action
		id     string
	}
	errMsg       struct{ err error }
	pagerDoneMsg struct{ err error }
)

func newModel(ctx context.Context, st *store.Store, actor string) model {
	in := textinput.New()
	in.Placeholder = "reason (required)"
	in.CharLimit = 1000
	return model{ctx: ctx, st: st, actor: actor, input: in, vp: viewport.New(80, 20)}
}

func (m model) Init() tea.Cmd { return m.loadList() }

func (m model) loadList() tea.Cmd {
	return func() tea.Msg {
		items, err := m.st.Interpretations(m.ctx, interpretation.StatusReviewRequired, interpretation.StatusPending)
		if err != nil {
			return errMsg{err}
		}
		return listMsg(items)
	}
}

func (m model) loadDetail(id string) tea.Cmd {
	return func() tea.Msg {
		it, err := m.st.Interpretation(m.ctx, id)
		if err != nil {
			return errMsg{err}
		}
		d := &detail{it: it}
		if d.text, err = m.st.LicenseText(m.ctx, it.SourceHash); err != nil {
			return errMsg{err}
		}
		if d.evidence, err = m.st.EvidenceByHash(m.ctx, it.SourceHash); err != nil {
			return errMsg{err}
		}
		if d.previous, err = m.st.PreviousApproved(m.ctx, it.SourceHash); err != nil {
			return errMsg{err}
		}
		if d.decisions, err = m.st.Decisions(m.ctx, id); err != nil {
			return errMsg{err}
		}
		return detailMsg(d)
	}
}

func (m model) decide(reason string) tea.Cmd {
	req := decision.Request{
		InterpretationID:          m.cur.it.ID,
		Action:                    m.action,
		Reason:                    reason,
		Actor:                     m.actor,
		UncertaintiesAcknowledged: m.ack,
	}
	return func() tea.Msg {
		if _, err := m.st.Decide(m.ctx, req); err != nil {
			return errMsg{err}
		}
		return decidedMsg{req.Action, req.InterpretationID}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.Width = msg.Width
		m.vp.Height = max(msg.Height-5, 3)
		m.input.Width = max(msg.Width-4, 10)
		m.refresh()
		return m, nil
	case listMsg:
		m.items = msg
		m.cursor = min(m.cursor, max(len(m.items)-1, 0))
		return m, nil
	case detailMsg:
		m.cur, m.mode, m.tab, m.ack = msg, modeDetail, tabInterpretation, false
		m.refresh()
		m.vp.GotoTop()
		return m, nil
	case decidedMsg:
		m.flash = styleGood.Render(fmt.Sprintf("%s: %s", msg.id, msg.action))
		m.mode, m.cur = modeList, nil
		m.input.Reset()
		return m, m.loadList()
	case errMsg:
		m.flash = styleBad.Render("error: " + msg.err.Error())
		if m.mode == modeReason {
			m.mode = modeDetail
			m.input.Blur()
		}
		return m, nil
	case pagerDoneMsg:
		if msg.err != nil {
			m.flash = styleBad.Render("pager: " + msg.err.Error())
		}
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeDetail:
			return m.updateDetail(msg)
		case modeReason:
			return m.updateReason(msg)
		}
	}
	return m, nil
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.flash = ""
	switch msg.String() {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
	case "down", "j":
		m.cursor = min(m.cursor+1, max(len(m.items)-1, 0))
	case "R":
		return m, m.loadList()
	case "enter":
		if len(m.items) > 0 {
			return m, m.loadDetail(m.items[m.cursor].ID)
		}
	}
	return m, nil
}

func (m model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.mode, m.cur, m.flash = modeList, nil, ""
		return m, nil
	case "tab", "right", "l":
		m.tab = (m.tab + 1) % tabCount
		m.refresh()
		m.vp.GotoTop()
		return m, nil
	case "shift+tab", "left", "h":
		m.tab = (m.tab + tabCount - 1) % tabCount
		m.refresh()
		m.vp.GotoTop()
		return m, nil
	case "u":
		if len(m.cur.it.Uncertainties) > 0 {
			m.ack = !m.ack
			m.refresh()
		}
		return m, nil
	case "a":
		if len(m.cur.it.Uncertainties) > 0 && !m.ack {
			m.flash = styleWarn.Render("review the uncertainties and press u before approving")
			return m, nil
		}
		return m.startReason(decision.ActionApprove)
	case "x":
		return m.startReason(decision.ActionReject)
	case "c":
		return m.startReason(decision.ActionRequestChanges)
	case "o":
		return m, m.openPager()
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m model) startReason(a decision.Action) (tea.Model, tea.Cmd) {
	m.action, m.mode, m.flash = a, modeReason, ""
	m.input.Reset()
	return m, m.input.Focus()
}

func (m model) updateReason(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeDetail
		m.input.Blur()
		return m, nil
	case "enter":
		reason := strings.TrimSpace(m.input.Value())
		if reason == "" {
			m.flash = styleWarn.Render("a reason is required")
			return m, nil
		}
		return m, m.decide(reason)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// openPager shows the raw content of the current tab in $PAGER.
func (m model) openPager() tea.Cmd {
	content := m.cur.text
	if m.tab == tabDiff && m.cur.previous != nil {
		content = plainDiff(m.cur.previous.Text, m.cur.text)
	}
	f, err := os.CreateTemp("", "thesmo-*.txt")
	if err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}
	path := f.Name()
	cmd := exec.Command("sh", "-c", pager+` "$1"`, "sh", path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		os.Remove(path)
		return pagerDoneMsg{err}
	})
}

// --- Rendering --------------------------------------------------------------

func (m *model) refresh() {
	if m.cur == nil {
		return
	}
	width := max(m.width-2, 20)
	var s string
	switch m.tab {
	case tabInterpretation:
		s = m.renderInterpretation()
	case tabText:
		s = m.cur.text
	case tabDiff:
		s = m.renderDiff()
	}
	m.vp.SetContent(lipgloss.NewStyle().Width(width).Render(s))
}

func (m model) View() string {
	switch m.mode {
	case modeList:
		return m.viewList()
	default:
		return m.viewDetail()
	}
}

func (m model) viewList() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("thesmo review") + styleDim.Render("  ·  "+m.actor) + "\n\n")
	if len(m.items) == 0 {
		b.WriteString(styleDim.Render("Nothing awaiting review.") + "\n")
	}
	for i, it := range m.items {
		status := string(it.Status)
		if it.Status == interpretation.StatusReviewRequired {
			status = styleWarn.Render(status)
		}
		flags := ""
		if n := len(it.Uncertainties); n > 0 {
			flags = styleWarn.Render(fmt.Sprintf(" ?%d", n))
		}
		line := fmt.Sprintf("%-18s %-24s %-8s %-15s %s%s",
			it.ID, it.License, it.Provenance.Provider, status, shortHash(it.SourceHash), flags)
		if i == m.cursor {
			line = styleCursor.Render(line)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + m.flash + "\n")
	b.WriteString(styleDim.Render("↑/↓ move · enter open · R reload · q quit"))
	return b.String()
}

func (m model) viewDetail() string {
	var tabs []string
	for i, name := range tabNames {
		if tab(i) == m.tab {
			tabs = append(tabs, styleTabOn.Render(name))
		} else {
			tabs = append(tabs, styleTab.Render(name))
		}
	}
	header := styleTitle.Render(m.cur.it.ID) + "  " + strings.Join(tabs, "")

	var footer string
	switch m.mode {
	case modeReason:
		footer = styleTitle.Render(string(m.action)+": ") + m.input.View() + "\n" +
			styleDim.Render("enter submit · esc cancel")
	default:
		ack := ""
		if len(m.cur.it.Uncertainties) > 0 {
			if m.ack {
				ack = styleGood.Render("[x] uncertainties reviewed") + " · "
			} else {
				ack = styleWarn.Render("[ ] uncertainties reviewed (u)") + " · "
			}
		}
		footer = ack + styleDim.Render("tab switch · a approve · x reject · c request changes · o pager · esc back") +
			"\n" + m.flash
	}
	return header + "\n" + m.vp.View() + "\n" + footer
}

func (m model) renderInterpretation() string {
	it := m.cur.it
	var b strings.Builder
	field := func(k, v string) { fmt.Fprintf(&b, "%s %s\n", styleDim.Render(fmt.Sprintf("%-10s", k)), v) }
	field("status", string(it.Status))
	field("license", it.License)
	field("source", it.SourceHash)
	field("provider", fmt.Sprintf("%s · %s · %s@%s", it.Provenance.Provider, it.Provenance.Model,
		it.Provenance.Skill, it.Provenance.SkillVersion))
	field("submitted", it.SubmittedAt.Local().Format("2006-01-02 15:04"))

	if it.ReviewNote != "" {
		b.WriteString("\n" + styleSection.Render("Review note") + "\n")
		b.WriteString(styleWarn.Render(it.ReviewNote) + "\n")
	}

	b.WriteString("\n" + styleSection.Render("Summary") + "\n" + it.Summary + "\n")

	fmt.Fprintf(&b, "\n%s\n", styleSection.Render(fmt.Sprintf("Obligations (%d)", len(it.Obligations))))
	for _, o := range it.Obligations {
		when := "always"
		if len(o.Triggers) > 0 {
			when = "when " + strings.Join(o.Triggers, " ∧ ")
		}
		fmt.Fprintf(&b, "• %s  %s\n", styleTitle.Render(o.ID), styleDim.Render(when))
		fmt.Fprintf(&b, "  clause: %s\n", o.Clause)
		if o.Quote != "" {
			fmt.Fprintf(&b, "  %s\n", styleDim.Render("“"+o.Quote+"”"))
		}
	}

	if n := len(it.Uncertainties); n > 0 {
		fmt.Fprintf(&b, "\n%s\n", styleWarn.Render(styleSection.Render(fmt.Sprintf("Uncertainties (%d)", n))))
		for _, u := range it.Uncertainties {
			fmt.Fprintf(&b, "• %s — %s\n", styleTitle.Render(u.Clause), u.Reason)
		}
	}

	fmt.Fprintf(&b, "\n%s\n", styleSection.Render(fmt.Sprintf("Evidence (%d)", len(m.cur.evidence))))
	for _, e := range m.cur.evidence {
		fmt.Fprintf(&b, "• %s  %s  %s\n", e.Component.PURL, e.SPDXExpression, styleDim.Render(e.ID+" · "+e.Source))
	}

	if p := m.cur.previous; p != nil {
		b.WriteString("\n" + styleSection.Render("Previous version") + "\n")
		fmt.Fprintf(&b, "%s was approved as %s (%s). See the Diff tab.\n", p.PURL, p.Interpretation.ID, p.Interpretation.License)
	}

	if len(m.cur.decisions) > 0 {
		b.WriteString("\n" + styleSection.Render("Decisions") + "\n")
		for _, d := range m.cur.decisions {
			fmt.Fprintf(&b, "• %s %s %s: %s\n", d.DecidedAt.Local().Format("2006-01-02 15:04"), d.Actor, d.Action, d.Reason)
		}
	}
	return b.String()
}

func (m model) renderDiff() string {
	p := m.cur.previous
	if p == nil {
		return styleDim.Render("No approved interpretation from another version of this package.")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s → %s\n\n", styleDim.Render("previous: "+p.PURL), shortHash(p.SourceHash), shortHash(m.cur.it.SourceHash))

	b.WriteString(styleSection.Render("Obligation changes vs previous approval") + "\n")
	if diffs := interpretation.Compare(p.Interpretation.Submission, m.cur.it.Submission); len(diffs) == 0 {
		b.WriteString(styleGood.Render("none") + "\n")
	} else {
		for _, d := range diffs {
			b.WriteString(styleWarn.Render("• "+d) + "\n")
		}
	}

	b.WriteString("\n" + styleSection.Render("Text") + "\n")
	for _, l := range textdiff.Lines(p.Text, m.cur.text) {
		line := string(l.Op) + " " + l.Text
		switch l.Op {
		case textdiff.Insert:
			line = styleGood.Render(line)
		case textdiff.Delete:
			line = styleBad.Render(line)
		default:
			line = styleDim.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func plainDiff(a, b string) string {
	var sb strings.Builder
	for _, l := range textdiff.Lines(a, b) {
		sb.WriteByte(byte(l.Op))
		sb.WriteString(" " + l.Text + "\n")
	}
	return sb.String()
}

func shortHash(h string) string {
	if len(h) > 19 {
		return h[:19]
	}
	return h
}
