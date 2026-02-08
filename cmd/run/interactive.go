package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.bytecodealliance.org/wit"

	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	funcStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#98FB98"))

	typeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#87CEEB"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4"))

	resultStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#90EE90"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))
)

type interactiveModel struct {
	err      error
	rt       *runtime.Runtime
	wasi     *preview2.WASI
	instance *runtime.Instance
	module   *runtime.Module
	filename string
	result   string
	funcs    []funcInfo
	inputs   []textinput.Model
	selected int
	focusIdx int
	state    modelState
}

type funcInfo struct {
	name       string
	resultType string
	params     []paramInfo
}

type paramInfo struct {
	name    string
	witType wit.Type
	typeStr string
}

type modelState int

const (
	stateSelectFunc modelState = iota
	stateInputArgs
	stateShowResult
)

func newInteractiveModel(filename string) *interactiveModel {
	return &interactiveModel{
		filename: filename,
		state:    stateSelectFunc,
	}
}

type loadedMsg struct {
	err   error
	rt    *runtime.Runtime
	wasi  *preview2.WASI
	mod   *runtime.Module
	funcs []funcInfo
}

type callResultMsg struct {
	err    error
	result string
}

func (m *interactiveModel) Init() tea.Cmd {
	return m.loadComponent
}

func (m *interactiveModel) loadComponent() tea.Msg {
	ctx := context.Background()

	data, err := os.ReadFile(m.filename)
	if err != nil {
		return loadedMsg{err: err}
	}

	validated, err := component.DecodeAndValidate(data)
	if err != nil {
		return loadedMsg{err: err}
	}

	resolver := component.NewTypeResolverWithInstances(
		validated.Raw.TypeIndexSpace,
		validated.Raw.InstanceTypes,
	)
	reg, err := component.NewCanonRegistry(validated.Raw, resolver)
	if err != nil {
		return loadedMsg{err: err}
	}

	var funcs []funcInfo
	for name, lift := range reg.Lifts {
		fi := funcInfo{name: name}
		for i, p := range lift.Params {
			pname := fmt.Sprintf("arg%d", i)
			if i < len(lift.ParamNames) && lift.ParamNames[i] != "" {
				pname = lift.ParamNames[i]
			}
			fi.params = append(fi.params, paramInfo{
				name:    pname,
				witType: p,
				typeStr: witTypeStr(p),
			})
		}
		if len(lift.Results) > 0 {
			fi.resultType = witTypeStr(lift.Results[0])
		}
		funcs = append(funcs, fi)
	}
	sort.Slice(funcs, func(i, j int) bool { return funcs[i].name < funcs[j].name })

	rt, err := runtime.New(ctx)
	if err != nil {
		return loadedMsg{err: err}
	}

	wasi := preview2.New()
	if err := rt.RegisterWASI(wasi); err != nil {
		rt.Close(ctx)
		wasi.Close()
		return loadedMsg{err: err}
	}

	mod, err := rt.LoadComponent(ctx, data)
	if err != nil {
		rt.Close(ctx)
		wasi.Close()
		return loadedMsg{err: err}
	}

	return loadedMsg{funcs: funcs, rt: rt, wasi: wasi, mod: mod}
}

func (m *interactiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			ctx := context.Background()
			if m.instance != nil {
				m.instance.Close(ctx)
			}
			if m.rt != nil {
				m.rt.Close(ctx)
			}
			if m.wasi != nil {
				m.wasi.Close()
			}
			return m, tea.Quit

		case "up", "k":
			if m.state == stateSelectFunc && m.selected > 0 {
				m.selected--
			}

		case "down", "j":
			if m.state == stateSelectFunc && m.selected < len(m.funcs)-1 {
				m.selected++
			}

		case "enter":
			switch m.state {
			case stateSelectFunc:
				m.prepareInputs()
				if len(m.inputs) == 0 {
					return m, m.callFunction
				}
				m.state = stateInputArgs

			case stateInputArgs:
				return m, m.callFunction

			case stateShowResult:
				m.state = stateSelectFunc
				m.result = ""
				m.err = nil
			}

		case "tab":
			if m.state == stateInputArgs && len(m.inputs) > 1 {
				m.inputs[m.focusIdx].Blur()
				m.focusIdx = (m.focusIdx + 1) % len(m.inputs)
				m.inputs[m.focusIdx].Focus()
			}

		case "esc":
			switch m.state {
			case stateInputArgs:
				m.state = stateSelectFunc
				m.inputs = nil
			case stateShowResult:
				m.state = stateSelectFunc
				m.result = ""
				m.err = nil
			}
		}

	case loadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.funcs = msg.funcs
		m.rt = msg.rt
		m.wasi = msg.wasi
		m.module = msg.mod

	case callResultMsg:
		m.result = msg.result
		m.err = msg.err
		m.state = stateShowResult
	}

	if m.state == stateInputArgs {
		var cmds []tea.Cmd
		for i := range m.inputs {
			var cmd tea.Cmd
			m.inputs[i], cmd = m.inputs[i].Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	return m, nil
}

func (m *interactiveModel) prepareInputs() {
	f := m.funcs[m.selected]
	m.inputs = make([]textinput.Model, len(f.params))
	for i, p := range f.params {
		ti := textinput.New()
		ti.Placeholder = p.typeStr
		ti.Prompt = p.name + ": "
		ti.Width = 40
		if i == 0 {
			ti.Focus()
		}
		m.inputs[i] = ti
	}
	m.focusIdx = 0
}

func (m *interactiveModel) callFunction() tea.Msg {
	ctx := context.Background()

	if m.instance == nil {
		if m.module == nil {
			return callResultMsg{err: fmt.Errorf("module not loaded")}
		}
		inst, err := m.module.Instantiate(ctx)
		if err != nil {
			return callResultMsg{err: err}
		}
		m.instance = inst
	}

	f := m.funcs[m.selected]
	args := make([]any, len(m.inputs))
	for i, input := range m.inputs {
		args[i] = convertArg(input.Value(), f.params[i].witType)
	}

	result, err := m.instance.Call(ctx, f.name, args...)
	if err != nil {
		return callResultMsg{err: err}
	}

	return callResultMsg{result: fmt.Sprintf("%v", result)}
}

func convertArg(value string, t wit.Type) any {
	switch t.(type) {
	case wit.String:
		return value
	case wit.U8, wit.U16, wit.U32:
		v, _ := strconv.ParseUint(value, 10, 32)
		return uint32(v)
	case wit.S8, wit.S16, wit.S32:
		v, _ := strconv.ParseInt(value, 10, 32)
		return int32(v)
	case wit.U64:
		v, _ := strconv.ParseUint(value, 10, 64)
		return v
	case wit.S64:
		v, _ := strconv.ParseInt(value, 10, 64)
		return v
	case wit.F32:
		v, _ := strconv.ParseFloat(value, 32)
		return float32(v)
	case wit.F64:
		v, _ := strconv.ParseFloat(value, 64)
		return v
	case wit.Bool:
		return value == "true" || value == "1"
	default:
		return value
	}
}

func (m *interactiveModel) View() string {
	if m.err != nil && m.state != stateShowResult {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err))
	}

	if len(m.funcs) == 0 {
		return "Loading component..."
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render("WASM Runner"))
	b.WriteString(" ")
	b.WriteString(m.filename)
	b.WriteString("\n\n")

	switch m.state {
	case stateSelectFunc:
		b.WriteString("Select a function to call:\n\n")
		for i, f := range m.funcs {
			cursor := "  "
			if i == m.selected {
				cursor = "> "
				b.WriteString(selectedStyle.Render(cursor + m.formatFunc(f)))
			} else {
				b.WriteString(cursor + m.formatFunc(f))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("↑/↓ select • enter call • q quit"))

	case stateInputArgs:
		f := m.funcs[m.selected]
		b.WriteString(fmt.Sprintf("Calling %s\n\n", funcStyle.Render(f.name)))
		for i, input := range m.inputs {
			b.WriteString(input.View())
			b.WriteString(" ")
			b.WriteString(typeStyle.Render(f.params[i].typeStr))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("tab next field • enter call • esc back"))

	case stateShowResult:
		f := m.funcs[m.selected]
		b.WriteString(fmt.Sprintf("Result of %s:\n\n", funcStyle.Render(f.name)))
		if m.err != nil {
			b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		} else {
			b.WriteString(resultStyle.Render(m.result))
		}
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("enter continue • q quit"))
	}

	return b.String()
}

func (m *interactiveModel) formatFunc(f funcInfo) string {
	var params []string
	for _, p := range f.params {
		params = append(params, p.name+": "+typeStyle.Render(p.typeStr))
	}
	result := ""
	if f.resultType != "" {
		result = " -> " + typeStyle.Render(f.resultType)
	}
	return funcStyle.Render(f.name) + "(" + strings.Join(params, ", ") + ")" + result
}

func witTypeStr(t wit.Type) string {
	switch v := t.(type) {
	case wit.Bool:
		return "bool"
	case wit.U8:
		return "u8"
	case wit.S8:
		return "s8"
	case wit.U16:
		return "u16"
	case wit.S16:
		return "s16"
	case wit.U32:
		return "u32"
	case wit.S32:
		return "s32"
	case wit.U64:
		return "u64"
	case wit.S64:
		return "s64"
	case wit.F32:
		return "f32"
	case wit.F64:
		return "f64"
	case wit.Char:
		return "char"
	case wit.String:
		return "string"
	case *wit.TypeDef:
		if v.Name != nil {
			return *v.Name
		}
		return "typedef"
	default:
		return fmt.Sprintf("%T", t)
	}
}

func runInteractive(filename string) error {
	p := tea.NewProgram(newInteractiveModel(filename), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
