package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/HasanMal1k/coder/internal/instance"
)

// execute carries out one tool call and returns what to record for it. It
// never returns an error: a tool that fails produces a result saying so,
// which the model reads and can act on.
func (s *Service) execute(ctx context.Context, ownerID, chatID string, call ToolCall) ToolPayload {
	result := ToolPayload{
		ToolCallID: call.ID,
		Name:       call.Function.Name,
	}

	content, machine, err := s.dispatch(ctx, ownerID, chatID, call)
	if err != nil {
		result.Content = "Error: " + err.Error()
		result.Failed = true
		return result
	}

	result.Content = truncate(content, maxToolOutput)
	result.Machine = machine

	return result
}

func (s *Service) dispatch(ctx context.Context, ownerID, chatID string, call ToolCall) (string, *Machine, error) {
	var args struct {
		Name    string `json:"name"`
		Size    string `json:"size"`
		DiskGB  int    `json:"disk_gb"`
		Stack   string `json:"stack"`
		Machine string `json:"machine"`
		Agent   string `json:"agent"`
		Task    string `json:"task"`
		// Background asks for the task to be started and not waited for.
		Background bool   `json:"background"`
		Port       int    `json:"port"`
		Label      string `json:"label"`
	}
	// A design is an object of its own shape, read where it is used.
	isDesign := call.Function.Name == toolUpdateArchitecture
	if raw := strings.TrimSpace(call.Function.Arguments); raw != "" && !isDesign {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "", nil, fmt.Errorf("the arguments were not valid JSON: %v", err)
		}
	}

	switch call.Function.Name {
	case toolListMachines:
		machines, err := s.machines.List(ctx, ownerID)
		if err != nil {
			return "", nil, err
		}
		if len(machines) == 0 {
			return "The user has no machines yet.", nil, nil
		}

		var b strings.Builder
		for _, m := range machines {
			fmt.Fprintf(&b, "- %s (%s; %s)\n", m.Name, m.Status, describeSize(m))
		}
		return b.String(), nil, nil

	case toolCreateMachine:
		spec := MachineSpec{
			Name:   strings.TrimSpace(args.Name),
			Size:   strings.TrimSpace(args.Size),
			DiskGB: args.DiskGB,
			Stack:  strings.TrimSpace(args.Stack),
		}

		// A machine the user agreed to in the design is created as the
		// design says, whatever the model asks for here: the size and the
		// stack are what the user saw and approved.
		if stored, design, err := s.design(ctx, chatID); err == nil && stored.Status == DesignApproved {
			for _, n := range design.Machines() {
				if n.Machine == spec.Name {
					spec.Size, spec.DiskGB, spec.Stack = n.Size, n.DiskGB, n.Stack
				}
			}
		}

		m, err := s.machines.Create(ctx, ownerID, spec, s.progress(ctx, chatID, call.ID, spec.Name))
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Machine %q (%s) is running and ready. Other machines can reach it at the hostname %q.", m.Name, describeSize(m), m.Name), &m, nil

	case toolStartMachine:
		m, err := s.machines.Start(ctx, ownerID, args.Name)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Machine %q is running.", m.Name), &m, nil

	case toolStopMachine:
		m, err := s.machines.Stop(ctx, ownerID, args.Name)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Machine %q is stopped. Its files are kept.", m.Name), &m, nil

	case toolCodeOnMachine:
		if strings.TrimSpace(args.Task) == "" {
			return "", nil, fmt.Errorf("the task is empty")
		}

		m, err := s.machines.Find(ctx, ownerID, args.Machine)
		if err != nil {
			return "", nil, err
		}

		report, err := s.assign(ctx, ownerID, chatID, call.ID, m, strings.TrimSpace(args.Agent), args.Task, args.Background)
		if err != nil {
			return "", nil, err
		}
		return report, &m, nil

	case toolListAgents:
		agents, err := s.agents.ListByChat(ctx, chatID)
		if err != nil {
			return "", nil, err
		}
		if len(agents) == 0 {
			return "This conversation has no agents yet. One is created the first time you give it a task.", nil, nil
		}

		now := time.Now()
		var b strings.Builder
		for _, agent := range agents {
			b.WriteString(describeAgent(agent, now) + "\n")
		}
		return b.String(), nil, nil

	case toolStopAgent:
		m, err := s.machines.Find(ctx, ownerID, args.Machine)
		if err != nil {
			return "", nil, err
		}

		name := strings.TrimSpace(args.Agent)
		if name == "" {
			name = defaultAgentName
		}

		agents, err := s.agents.ListByChat(ctx, chatID)
		if err != nil {
			return "", nil, err
		}
		for _, agent := range agents {
			if agent.InstanceID != m.ID || agent.Name != name {
				continue
			}
			if agent.Status != AgentWorking {
				return fmt.Sprintf("Agent %q on %q was not working on anything.", name, m.Name), &m, nil
			}
			// Not on the user's say-so: its task ends as stopped all the
			// same, and whoever was waiting on it is told.
			if !s.cancelAgent(agent.ID, false) {
				if err := s.agents.Finish(ctx, agent.ID, AgentStopped, "Stopped."); err != nil {
					return "", nil, err
				}
			}
			return fmt.Sprintf("Agent %q on %q has been told to stop. What it had done so far is still there.", name, m.Name), &m, nil
		}
		return "", nil, fmt.Errorf("there is no agent called %q on %q in this conversation; list_agents shows the ones there are", name, m.Name)

	case toolUpdateArchitecture:
		out, err := s.updateDesign(ctx, ownerID, chatID, call)
		return out, nil, err

	case toolShowPreview:
		if args.Port < 1 || args.Port > 65535 {
			return "", nil, fmt.Errorf("port must be between 1 and 65535")
		}

		m, err := s.machines.Find(ctx, ownerID, args.Machine)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("The user now has a button that opens port %d on %q.", args.Port, m.Name), &m, nil

	default:
		return "", nil, fmt.Errorf("there is no tool called %q", call.Function.Name)
	}
}

// progress returns a function that records what a tool is doing, as it does
// it, for the person watching. Notes are capped so that a very long task
// can't flood the chat.
func (s *Service) progress(ctx context.Context, chatID, callID, machine string) func(string) {
	var (
		mu    sync.Mutex
		notes int
	)

	return func(text string) {
		mu.Lock()
		notes++
		over := notes > maxActivitiesPerTask
		mu.Unlock()
		if over {
			return
		}

		s.append(context.WithoutCancel(ctx), chatID, KindActivity, ActivityPayload{
			ToolCallID: callID,
			Machine:    machine,
			Text:       text,
		})
	}
}

// describeSize says how big a machine is, in words.
func describeSize(m Machine) string {
	size := fmt.Sprintf("%d vCPU, %s memory, %d GB disk", m.CPUs, memoryText(m.MemoryMB), m.DiskGB)
	if stack, err := instance.StackOf(m.Stack); err == nil && m.Stack != "" {
		size += "; comes with " + stack.Has
	}

	return size
}

// truncate shortens s to at most max bytes, keeping the beginning and the
// end: a report's conclusion is usually at the bottom.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}

	const marker = "\n\n[… shortened …]\n\n"
	keep := (max - len(marker)) / 2

	return strings.ToValidUTF8(s[:keep], "") + marker + strings.ToValidUTF8(s[len(s)-keep:], "")
}
