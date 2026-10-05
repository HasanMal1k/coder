package instance

import (
	"errors"
	"slices"
	"strings"
)

var (
	// ErrInvalidStack is returned when a machine is asked for on a stack
	// that doesn't exist.
	ErrInvalidStack = errors.New("invalid machine stack")
	// ErrStackUnavailable is returned when a stack exists but its image has
	// not been built on this server.
	ErrStackUnavailable = errors.New("the image for that stack has not been built")
)

// Stack is a toolchain a machine can come with already installed. Every
// stack is the same machine otherwise: Ubuntu, the editor, the coding agent,
// Python and Node.js.
type Stack struct {
	// Name is what the API and the assistant call it.
	Name string
	// Has says what is installed on top of the common machine.
	Has string
}

// DefaultStack is the machine with nothing added.
const DefaultStack = "base"

// stacks are the ones on offer. Each has an image built from
// images/workspace/Dockerfile with STACK set to its name.
var stacks = []Stack{
	{Name: DefaultStack, Has: "Python 3 and Node.js"},
	{Name: "go", Has: "Go"},
	{Name: "cpp", Has: "C++: GCC, Clang, CMake, Ninja, GDB and common libraries"},
	{Name: "dotnet", Has: ".NET SDK, for C# and ASP.NET Core"},
	{Name: "data", Has: "PostgreSQL and Redis, installed but not running"},
}

// Stacks returns the stacks a machine can be created on.
func Stacks() []Stack {
	return slices.Clone(stacks)
}

// StackOf returns the stack with the given name. An empty name means the
// default.
func StackOf(name string) (Stack, error) {
	if name == "" {
		name = DefaultStack
	}

	i := slices.IndexFunc(stacks, func(s Stack) bool { return s.Name == name })
	if i < 0 {
		return Stack{}, ErrInvalidStack
	}

	return stacks[i], nil
}

// imageFor returns the image for a stack, given the image of the default
// one: "coder-workspace:dev" becomes "coder-workspace-go:dev".
func imageFor(base, stack string) string {
	if stack == DefaultStack {
		return base
	}

	// A tag follows the last colon, unless that colon is a registry's port
	// ("localhost:5000/coder-workspace").
	if i := strings.LastIndex(base, ":"); i >= 0 && !strings.Contains(base[i:], "/") {
		return base[:i] + "-" + stack + base[i:]
	}

	return base + "-" + stack
}
