package assistant

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/HasanMal1k/coder/internal/guestapi"
	"github.com/HasanMal1k/coder/internal/store"
)

// How long a newly registered service gets to start answering on its port,
// and how often that is checked: the machine has to hear of it, start it,
// and the program has to come up. Variables so that tests need not wait.
var (
	serviceStartTimeout = 45 * time.Second
	servicePoll         = time.Second
)

const (
	maxServiceCommand = 2000

	// machineHome is the folder a relative directory is taken from.
	machineHome = "/home/coder"
	// serviceLogs is where a machine keeps its services' output.
	serviceLogs = "~/.coder/logs"
)

// Mistake is something an agent got wrong in what it asked for, worded for
// the agent to read and put right. It is not a failure of the platform.
type Mistake string

func (m Mistake) Error() string { return string(m) }

// Services is the persistence services need. *store.ServiceRepository
// implements it.
type Services interface {
	Save(ctx context.Context, svc *store.MachineService) error
	Delete(ctx context.Context, instanceID, name string) (bool, error)
	ListByInstance(ctx context.Context, instanceID string) ([]*store.MachineService, error)
	ListByOwner(ctx context.Context, ownerID string) ([]*store.MachineService, error)
}

// GuestService is a service as a machine is told to run it.
type GuestService struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Directory string `json:"directory"`
	Port      int    `json:"port,omitempty"`
	// Env is added to the program's environment: the addresses of the
	// components this machine's are connected to, and the revision.
	Env map[string]string `json:"env,omitempty"`
	// Revision goes up each time the service is registered. A machine
	// starts the program afresh when it changes.
	Revision int64 `json:"revision"`
}

// guestState is what a machine said the last time it asked for its record.
type guestState struct {
	askedAt time.Time
	// started is the revision of each service the machine has started, by
	// name. It is nil for a machine whose daemon predates saying so.
	started map[string]int64
}

// NoteGuest records that a machine has just asked for its record, and which
// services it said it has started (nil if it did not say). Registering a
// service waits on this to know the program that answers is the one just
// asked for.
func (s *Service) NoteGuest(instanceID string, started map[string]int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.guests == nil {
		s.guests = make(map[string]guestState)
	}
	s.guests[instanceID] = guestState{askedAt: time.Now(), started: started}
}

func (s *Service) guestState(instanceID string) (guestState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.guests[instanceID]
	return state, ok
}

// RunService registers a program for the platform to keep running on a
// machine: it is started within seconds, restarted if it exits, and started
// again whenever the machine starts. If it has a port, RunService waits for
// it to answer and says whether it did. A backend is also asked for the
// description of its API, and its agent is told if that is missing.
//
// Registering a service that is already registered starts it afresh, which
// is how an agent gets code it has changed to run.
//
// This is what makes something an agent built still be there after the
// machine has been stopped and started, which a program left running from a
// shell is not.
func (s *Service) RunService(ctx context.Context, machine *store.Instance, name, command, directory string, port int) (string, error) {
	name, command, directory = strings.TrimSpace(name), strings.TrimSpace(command), strings.TrimSpace(directory)

	if !validAgentName.MatchString(name) {
		return "", Mistake(`A service needs a name of 1-40 lowercase letters, digits or hyphens, such as "api".`)
	}
	if command == "" || len(command) > maxServiceCommand {
		return "", Mistake(fmt.Sprintf("A service needs a command to run, of at most %d characters. Put anything longer in a script and run that.", maxServiceCommand))
	}
	if port < 0 || port > 65535 {
		return "", Mistake("The port must be between 1 and 65535, or left out if the program listens on none.")
	}

	directory = strings.TrimPrefix(directory, "~/")
	switch {
	case directory == "" || directory == "~":
		directory = machineHome
	case !path.IsAbs(directory):
		directory = path.Join(machineHome, directory)
	}
	if strings.ContainsAny(directory, "\x00\n") {
		return "", Mistake("The directory is not a usable path.")
	}

	registered := time.Now()
	saved := &store.MachineService{
		InstanceID: machine.ID,
		Name:       name,
		Command:    command,
		Directory:  path.Clean(directory),
		Port:       port,
	}
	if err := s.services.Save(ctx, saved); err != nil {
		return "", err
	}

	kept := fmt.Sprintf("The platform now keeps %q running on %q: it is started afresh now, restarted if it exits, and started whenever the machine starts. Its output is in %s/%s.log.", name, machine.Name, serviceLogs, name)
	if port == 0 {
		return kept + " It should start within a few seconds; check its log to see that it has.", nil
	}

	up := func() string {
		return fmt.Sprintf("%s It is up and answering on port %d.%s", kept, port, s.apiDescriptionNote(ctx, machine, port))
	}

	// The machine hears of the registration the next time it asks for its
	// record, starts the program, and the program takes however long it
	// takes to listen. Until the machine has started this registration, an
	// answer on the port may be from the program it replaces, and proves
	// nothing.
	deadline := time.Now().Add(serviceStartTimeout)
	var heardAt time.Time
	for {
		state, ok := s.guestState(machine.ID)
		if heardAt.IsZero() && ok && state.askedAt.After(registered) {
			heardAt = time.Now()
		}

		started := false
		switch {
		case heardAt.IsZero():
		case state.started != nil:
			started = state.started[name] >= saved.Revision
		default:
			// A daemon that doesn't say what it has started: give it a
			// moment to have replaced the old program.
			started = time.Since(heardAt) >= servicePoll
		}

		if started && s.machines.PortOpen(ctx, machine.ID, port) {
			return up(), nil
		}
		if time.Now().After(deadline) {
			// A machine that was never heard from is still judged by its
			// port, as the only thing there is to go on.
			if heardAt.IsZero() && s.machines.PortOpen(ctx, machine.ID, port) {
				return up(), nil
			}
			return fmt.Sprintf("%s But nothing is answering on port %d after %s. Read the log to see why, fix it, and register the service again.", kept, port, serviceStartTimeout), nil
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(servicePoll):
		}
	}
}

// StopService stops the platform keeping a program running on a machine.
func (s *Service) StopService(ctx context.Context, machine *store.Instance, name string) (string, error) {
	removed, err := s.services.Delete(ctx, machine.ID, strings.TrimSpace(name))
	if err != nil {
		return "", err
	}
	if !removed {
		return "", Mistake(fmt.Sprintf("There is no service called %q on %q.", name, machine.Name))
	}

	return fmt.Sprintf("Service %q on %q is stopped, within a few seconds, and will not be started again.", name, machine.Name), nil
}

// MachineRecord is what a machine keeps on its own disk and what it is told
// to run: the team's record as files, and its services.
type MachineRecord struct {
	Files    map[string]string `json:"files"`
	Services []GuestService    `json:"services"`
}

// MachineRecord returns the record for one machine.
func (s *Service) MachineRecord(ctx context.Context, machine *store.Instance) (*MachineRecord, error) {
	team, err := s.Team(ctx, machine.OwnerID)
	if err != nil {
		return nil, err
	}

	registered, err := s.services.ListByInstance(ctx, machine.ID)
	if err != nil {
		return nil, err
	}

	connections := team.connectionsFor(machine.Name)

	record := &MachineRecord{
		Files:    team.Files(machine.Name),
		Services: make([]GuestService, 0, len(registered)),
	}
	for _, svc := range registered {
		// The revision is in the environment too: a daemon from before
		// revisions restarts a program whose environment has changed, so
		// registering again restarts the program on those as well.
		env := map[string]string{guestapi.ServiceRevisionEnv: strconv.FormatInt(svc.Revision, 10)}
		for _, c := range connections {
			env[c.Env] = c.Value
		}

		record.Services = append(record.Services, GuestService{
			Name:      svc.Name,
			Command:   svc.Command,
			Directory: svc.Directory,
			Port:      svc.Port,
			Env:       env,
			Revision:  svc.Revision,
		})
	}

	return record, nil
}
