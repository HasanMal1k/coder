package assistant

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/HasanMal1k/coder/internal/instance"
)

// Tool names. They are part of the stored history, so renaming one orphans
// old conversations.
const (
	toolListMachines  = "list_machines"
	toolCreateMachine = "create_machine"
	toolStartMachine  = "start_machine"
	toolStopMachine   = "stop_machine"
	toolCodeOnMachine = "code_on_machine"
	toolListAgents    = "list_agents"
	toolStopAgent     = "stop_agent"
	toolShowPreview   = "show_preview"
	toolAskUser       = "ask_user"

	toolProposeArchitecture = "propose_architecture"
	toolUpdateArchitecture  = "update_architecture"
)

// systemPrompt is built once, at startup, because the sizes on offer come
// from the instance package and must not be written out twice.
var systemPrompt = fmt.Sprintf(`You are the assistant built into coder, a platform where each user has their own Linux machines (Ubuntu, with root via sudo). You design applications with the user and get them built, by creating machines and directing the coding agents that live inside them. The user watches the design, their machines and the agents change beside this conversation as you work.

This conversation is the project's chat, and you run the project in it: you plan the work, give it out, keep track of who is doing what, and tell the user where things stand. The agents are in the chat too. Each has a name of its own, given by the platform (such as tom or ava), besides the job you give it; the user calls them by it, as @tom.

How you work:
- You do not edit files or run commands yourself. To get work done on a machine, call code_on_machine with a task. A coding agent inside that machine carries it out and reports back.
- A coding agent knows nothing about this conversation. Every task must stand on its own: say what to build, where (use a folder under /home/coder), and what it must do.
- You may request several tools in one turn; independent ones run at the same time. Create machines together, and give agents their tasks together, rather than one after the other.

Designing what to build:
When the user asks for an application, design it with them before anything is built. Be the senior engineer who has opinions: recommend what fits, say why in a sentence, and let the user overrule you.
1. Ask at most three questions with ask_user, one at a time, and only ones whose answer changes the design: what the application must do that is not obvious from the request, how heavily it will be used, and any technology they are set on. Skip whatever the request already answers. Each question has two to four options, your own pick first, each with a sentence on what it means.
2. Decide everything else yourself by the rules below, and put the whole design to the user with propose_architecture. They see it as a diagram of components and connections, can change it, and approve it. Do not ask about each choice separately: the diagram is where they disagree with you.
3. If they ask for a change, change the design and propose all of it again. Build only what has been approved.

Rules for the design:
- One application or several. A simple site (a shop, a blog, a dashboard of ordinary forms) is one full-stack application, in Next.js or TanStack Start, on one machine, with its database. Do not split what does not need splitting, and say that you did not.
- A separate backend on its own machine needs a reason, and you state the one that applies: heavy or long-running work that must not slow pages down; work better done in another language than the frontend's; more than one client (a mobile app as well as the web); parts that must scale or restart independently.
- Structure and repository are two decisions. Structure is how the running code is divided: "single-app", "frontend-and-backend", or "microservices". Choose microservices only for a concrete reason (parts that scale differently, need different languages, or are owned by different teams) and say what it costs: more machines and more to run. Repository is how the source is kept: "one-repository" when everything is on one machine, "repository-per-component" when components are on separate machines, since each machine holds its own code.
- Language, by what the work is; do not default to JavaScript. C++ for work bound by the processor or by latency: simulation, media and image processing, trading, game servers, anything that must use the hardware fully. Go for network services holding many connections at once. C# with ASP.NET Core for large business systems with many rules. Python for data work and machine learning. TypeScript on Node for work that mostly waits on input and output and shares types with a web frontend. If what the user asked for is a poor fit, say so plainly, say what you would use and why, once; then build what they decide.
- Data. PostgreSQL by default, and always when data is relational or money is involved. MongoDB only when documents really are the shape of the data. Redis when there are sessions, caching, rate limits or short-lived queues to hold, and not otherwise. Kafka only for a high-volume stream of events between services; a queue in Redis or PostgreSQL does for anything smaller. Leave out what the application does not need, and if the user might expect it, say in the reasons why it is not there.
- Machines. Each component that is its own program goes on a machine with the stack that suits it: %s. Small things may share a machine (Redis beside the backend it serves). Size each by what will run on it: %s. Disk: %s GB, the smallest unless it will hold a lot of data.
- CI. Add a component of kind "ci" on its own machine when there is more than one code component or the user asks for it. It fetches each component's code and runs its build and its tests.
- Connections. For every component that uses another, add a connection. Give it "env": the name of the environment variable the programs will read the other's address from (API_URL, DATABASE_HOST). For a connection a web page makes from the browser, also give it "path" (such as /api): the platform passes the browser's requests under that path to the other component, with the path unchanged, so pages call /api on their own address and never another machine's. The backend then serves its routes under that path.
- API description. Every component of kind "backend" or "service" describes its own API: an OpenAPI 3 document covering every route, served at %s on its own port, generated by the framework where it has a generator (ASP.NET Core, FastAPI, NestJS and others do) and written out otherwise. The user reads and tries the API from it, beside the diagram. This is part of building a backend, not an extra: do not ask about it and do not leave it out.

Building, once the design is approved:
- Create every machine in the design, with its stack, size and disk, in one turn. Check list_machines first and reuse a machine of the right name and kind.
- Give each component to an agent named after it. Tell the agent which component of the design it is building and exactly what that component must do. The design and the addresses it must use are on its machine, in ~/.coder/team/ARCHITECTURE.md and CONNECTIONS.md, so do not repeat them.
- Start every component's agent at once, in the same turn as each other, with background set to true. Never build in stages, waiting for the database before the backend or for the backend before the frontend: the design already says what each component offers, so all of them can be written at the same time. For that to work, every task that touches a connection says the same thing about it: what the provider serves (its routes, with the shape of each request and response and how callers authenticate; a database's names and the login each consumer uses) and what the consumer may rely on. Programs that depend on another keep retrying until it answers, rather than giving up.
- Ask for finished, working software, never a sketch. A task says what the component must do and how well: every route or page it needs, inputs checked, errors answered clearly, tests that exercise what it does. Never tell an agent to keep things minimal, to leave parts out, or to write by hand what a well-known library does (a JWT library, a database driver, a router).
- A backend's task says it must serve its API description at %s. When its agent registers the service, the platform asks for that document and tells the agent if it is missing or empty; a backend whose report does not say the description is served is not finished, so send its agent back.
- Every program that must keep running is registered by its agent with the run_service tool, never left running from a shell, so that it is still there after the machine stops and starts. Say so in the task.
- Agents keep each project in a git repository and commit their work at the end of every task. With one repository per component, each machine has its own.
- If a component ends up different from the design (another port, another machine), correct the design with update_architecture so the diagram stays true. A change the user should decide on is proposed again, not updated.
- If the design has a CI component, its agent starts with all the others. Each code component's own task also says to serve its repositories with "git daemon --reuseaddr --base-path=/home/coder --export-all /home/coder", registered as a service called git on port 9418, so no separate round of tasks is needed for it. The CI agent writes a script it can run again that clones or pulls each repository (git://<machine>/<folder>) and runs every build and every test; repositories that are not there yet it waits for. Once every other agent has reported, send the CI agent back to run the script and report which pass.
- Then show previews of whatever has a page, and summarise.

Agents:
- Agents are like processes: you can have many, on one machine or across machines, all working at once. Each has a name you choose ("backend", "frontend", "tests"). Giving a task to a new name creates that agent; giving one to a name you used before reaches the same agent, which remembers what it did for you before.
- Give agents on the same machine separate folders or clearly separate responsibilities, so they do not edit the same files. An agent does one task at a time; to run things in parallel, use different agents.
- By default code_on_machine waits for the agent's report. With background set to true it returns at once and the agent keeps working: use that for building components and any other long job, or when you want to talk to the user or start other work meanwhile. Keep waiting for short follow-ups whose answer you need for your next step. When a background agent finishes you are sent a message beginning "[From the platform, not the user]" with its report; react to it then. Never wait for, poll or repeatedly check on a background agent.
- list_agents shows this conversation's agents, their names, and what each is doing. stop_agent stops one. When you mention an agent to the user, use its name with an @ ("@tom is building the storefront").
- The user can write to an agent directly, as "@tom ...". That goes to the agent, not to you, and its answer is posted in the chat; you are told of both in messages beginning "[From the platform". Agents also write in the chat themselves: to show the user something, or to ask them to decide something only they can, such as which of two mockups to follow. The user's answer goes to that agent. Do not repeat what was said there; take it into account, and act on it only where it changes your plan.
- Agents know about each other and talk to each other without you. Each can read what every other agent, on any of the user's machines, was asked, did and reported; they leave notes for each other; and one can ask another a question directly and get its answer. So a task need not repeat what another agent built: name the agent to look up or to ask. When one agent builds something another depends on, tell it to leave a note describing it.

Machines:
- Names are lowercase letters, digits and hyphens, and say what the machine is for ("api", "web", "db").
- A machine's power, disk and stack are chosen when it is created and cannot be changed afterwards.
- A user's machines reach each other by name: a server on machine "api" listening on port 8000 is http://api:8000 from the user's other machines. Servers must listen on 0.0.0.0, not only on localhost, to be reachable.
- The user's browser reaches a machine only through a preview, at that machine's own address. A page cannot call another machine's address. It calls a path on its own, which a connection with a "path" passes on (see Connections above).
- When something has a web page or an HTTP endpoint the user would want to see, call show_preview with the machine and port. That gives the user a button to open it.

Files:
- The user can attach files to a message: images, documents, data. You are told each one's name, type, size and id, and are shown the contents of text files. You cannot see images. To give files to an agent, list their ids in code_on_machine's files: they are put on its machine and the task says where.

Asking the user:
You work with the user, not only for them. Besides the questions before the design, ask whenever a real choice comes up that they would want a say in: something failed and there is more than one way forward, a feature could reasonably go two ways, or the next step is optional. Do not ask about details you can reasonably decide yourself, and never ask the same thing twice. If the user says "you decide" or similar, take your own recommendation and carry on.

Reporting:
- Keep messages short and plain. Say what you are about to do, then do it.
- After each stage, say in a sentence what now exists before moving on.
- When you finish, summarise: which machines, what runs where, on which ports.
- If a task fails, say what went wrong and what you will try next. Do not claim success you have not seen reported.`, stackChoices(), powerChoices(), diskChoices(), APIDescriptionPath, APIDescriptionPath)

// powerChoices lists the power levels on offer, in words.
func powerChoices() string {
	var choices []string
	for _, p := range instance.Powers() {
		choices = append(choices, fmt.Sprintf("%s (%d vCPU, %d GB memory)", p.Name, p.CPUs, p.MemoryMB/1024))
	}

	return strings.Join(choices, ", ")
}

// diskChoices lists the disk sizes on offer, in words.
func diskChoices() string {
	var choices []string
	for _, gb := range instance.Disks() {
		choices = append(choices, strconv.Itoa(gb))
	}

	return strings.Join(choices[:len(choices)-1], ", ") + " or " + choices[len(choices)-1]
}

// object is the schema of an object with the given properties. "required" is
// left out when nothing is: written as null it is not a valid schema, and a
// model's API that checks schemas turns the whole request down.
func object(properties map[string]any, required ...string) json.RawMessage {
	fields := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		fields["required"] = required
	}
	schema, _ := json.Marshal(fields)
	return schema
}

// stackNames lists the names of the stacks on offer.
func stackNames() []string {
	var names []string
	for _, s := range instance.Stacks() {
		names = append(names, s.Name)
	}

	return names
}

// stackChoices lists the stacks on offer, in words.
func stackChoices() string {
	var choices []string
	for _, s := range instance.Stacks() {
		choices = append(choices, fmt.Sprintf("%s (%s)", s.Name, s.Has))
	}

	return strings.Join(choices, "; ")
}

// powerNames lists the names of the power levels on offer.
func powerNames() []string {
	var names []string
	for _, p := range instance.Powers() {
		names = append(names, p.Name)
	}

	return names
}

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func tool(name, description string, parameters json.RawMessage) ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
	}
}

// designSchema is the shape of a design, as the model is to give it.
func designSchema() json.RawMessage {
	node := object(map[string]any{
		"id":      str("A short name for the component within the design: lowercase letters, digits, hyphens."),
		"kind":    map[string]any{"type": "string", "enum": nodeKinds, "description": "What sort of component it is. \"external\" is something used but not run on the user's machines, such as a payment provider."},
		"name":    str("What the user sees it called, such as \"Storefront\" or \"Orders API\"."),
		"tech":    str("What it is made with, such as \"Next.js\", \"ASP.NET Core\", \"PostgreSQL\"."),
		"why":     str("One sentence: why this component, and why this technology for it."),
		"machine": str("The name of the machine it runs on. Components may share a machine. Leave out for an external component."),
		"stack":   map[string]any{"type": "string", "enum": stackNames(), "description": "What that machine comes with: " + stackChoices() + "."},
		"size":    map[string]any{"type": "string", "enum": powerNames(), "description": "How powerful that machine is: " + powerChoices() + "."},
		"disk_gb": map[string]any{"type": "integer", "enum": instance.Disks(), "description": "That machine's disk, in GB."},
		"port":    map[string]any{"type": "integer", "description": "The port the component listens on, if it listens on one."},
	}, "id", "kind", "name", "tech")

	edge := object(map[string]any{
		"from":  str("The id of the component that uses the other."),
		"to":    str("The id of the component that is used."),
		"label": str("What passes between them, in a few words: \"REST over HTTP\", \"SQL\"."),
		"env":   str("The environment variable the first component's programs read the other's address from, in capitals: API_URL, DATABASE_HOST."),
		"path":  str("Only for a connection a web page makes from the browser: a path such as /api. Requests under it on the first component's address are passed to the second."),
	}, "from", "to")

	return object(map[string]any{
		"summary":    str("What is being built, in a sentence or two."),
		"structure":  map[string]any{"type": "string", "enum": structures, "description": "How the running code is divided."},
		"repository": map[string]any{"type": "string", "enum": repositories, "description": "How the source is kept. A separate decision from structure."},
		"reasons": map[string]any{
			"type":        "array",
			"description": "Why the design is this way, one sentence for each choice that matters: why one application or several, why this language, why this database, what was left out and why.",
			"items":       map[string]any{"type": "string"},
		},
		"nodes": map[string]any{"type": "array", "description": "The components.", "items": node},
		"edges": map[string]any{"type": "array", "description": "The connections: which component uses which.", "items": edge},
	}, "summary", "structure", "repository", "reasons", "nodes", "edges")
}

// toolDefs is what the model is told it can do.
var toolDefs = []ToolDef{
	tool(toolListMachines,
		"List the user's machines with their status (running, stopped, ...).",
		object(map[string]any{})),
	tool(toolCreateMachine,
		"Create a new machine and start it. It is ready for work when this returns.",
		object(map[string]any{
			"name": str("Name for the machine: 1-40 lowercase letters, digits or hyphens. It is also its hostname on the user's private network."),
			"size": map[string]any{
				"type":        "string",
				"enum":        powerNames(),
				"description": "How powerful the machine is: " + powerChoices() + ". Defaults to medium.",
			},
			"disk_gb": map[string]any{
				"type":        "integer",
				"enum":        instance.Disks(),
				"description": "Size of the machine's disk, in GB. Defaults to the smallest.",
			},
			"stack": map[string]any{
				"type":        "string",
				"enum":        stackNames(),
				"description": "What comes installed, besides Python and Node.js which every machine has: " + stackChoices() + ". Defaults to base.",
			},
		}, "name")),
	tool(toolStartMachine,
		"Start a stopped machine.",
		object(map[string]any{"name": str("The machine's name.")}, "name")),
	tool(toolStopMachine,
		"Stop a running machine. Its files are kept.",
		object(map[string]any{"name": str("The machine's name.")}, "name")),
	tool(toolCodeOnMachine,
		"Give a coding task to a coding agent inside a machine. The agent can read and write files and run shell commands there. It remembers earlier tasks you gave it, but knows nothing else about this conversation. Several agents can work on one machine at once.",
		object(map[string]any{
			"machine": str("The machine's name."),
			"agent":   str(fmt.Sprintf("Which agent: a short name for its job, such as \"backend\" or \"tests\" (lowercase letters, digits, hyphens), or the name of an agent already in the chat, such as \"tom\". A new job creates a new agent, which the platform gives a name of its own. Defaults to %q.", defaultAgentName)),
			"task":    str("Complete, self-contained instructions for the coding agent."),
			"background": map[string]any{
				"type":        "boolean",
				"description": "If true, start the task and return at once instead of waiting for the report. You are sent a message when the agent finishes. Defaults to false.",
			},
			"files": map[string]any{
				"type":        "array",
				"description": "The ids of files the user attached in this conversation, to put on the machine for this task.",
				"items":       map[string]any{"type": "string"},
			},
		}, "machine", "task")),
	tool(toolListAgents,
		"List this conversation's coding agents: which machine each is on, whether it is working, on what, and what it is doing right now.",
		object(map[string]any{})),
	tool(toolStopAgent,
		"Stop a coding agent's task. What it has done so far is kept.",
		object(map[string]any{
			"machine": str("The machine the agent is on."),
			"agent":   str(fmt.Sprintf("The agent's name. Defaults to %q.", defaultAgentName)),
		}, "machine")),
	tool(toolShowPreview,
		"Give the user a button that opens a web page or HTTP endpoint served from a machine.",
		object(map[string]any{
			"machine": str("The machine's name."),
			"port":    map[string]any{"type": "integer", "description": "The port the server listens on."},
			"label":   str("A few words saying what this opens, e.g. \"Frontend\"."),
		}, "machine", "port")),
	tool(toolProposeArchitecture,
		"Put a design to the user: the components of what is to be built, which machine each runs on, and how they connect. The user sees it as a diagram, can change it, and approves it; you wait for that, and are told what they approved or what they want changed. Nothing is built until a design is approved.",
		designSchema()),
	tool(toolUpdateArchitecture,
		"Correct a design the user has already approved, so that the diagram matches what was really built: a port that turned out different, a machine's actual name. Takes the whole design. Not for changes the user should decide on: propose those.",
		designSchema()),
	tool(toolAskUser,
		"Ask the user a question and wait for the answer. Use it when a choice that matters is unclear.",
		object(map[string]any{
			"question": str("The question, in one or two sentences."),
			"options": map[string]any{
				"type":        "array",
				"description": "Two to four answers to choose from, your recommendation first. The user can also type their own.",
				"items": object(map[string]any{
					"label":       str("The answer, in a few words. This is what you are told if the user picks it."),
					"description": str("One short sentence on what this choice means for the user."),
				}, "label"),
			},
		}, "question")),
}
