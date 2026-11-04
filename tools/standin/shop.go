package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The one thing the stand-in knows how to build: a small online shop. A
// storefront page on one machine, an orders API on another, PostgreSQL on a
// third, and a fourth that fetches the API's code and checks it.
//
// As the assistant it asks a question, proposes that design, proposes it
// again if the user wants a change, and once it is approved creates the
// machines and hands out the tasks. As a coding agent it does the work for
// real, with the agent's own tools, so what the platform does around an
// agent (services, the API description, agents asking each other) happens
// as it would with a model.

// Names the script and the checks that drive it must agree on.
const (
	machineWeb = "web"
	machineAPI = "api"
	machineDB  = "db"
	machineCI  = "ci"

	// Agents are named after the components they build.
	agentStorefront = "storefront"
	agentAPI        = "api"
	agentDatabase   = "database"
	agentCI         = "ci"

	portWeb = 3000
	portAPI = 8000
	portDB  = 5432
	portGit = 9418
)

// shopDesign is the design the assistant proposes. After the user has asked
// for a change it proposes it again with a larger machine for the API, which
// is a change that shows on the diagram.
func shopDesign(changed bool) map[string]any {
	apiSize := "small"
	reasons := []string{
		"The storefront and the API are separate so that orders keep being taken while the page is changed.",
		"Python for the API: it is mostly waiting on the database, and it needs nothing installed.",
		"PostgreSQL because orders are money and the data is relational.",
		"No Redis or queue: nothing here needs one yet.",
		"A CI machine, since there is more than one code component.",
	}
	if changed {
		apiSize = "medium"
		reasons = append(reasons, "The API's machine is medium, as you asked.")
	}

	return map[string]any{
		"summary":    "An online shop: a storefront page, an orders API behind it, and PostgreSQL holding the catalogue.",
		"structure":  "frontend-and-backend",
		"repository": "repository-per-component",
		"reasons":    reasons,
		"nodes": []map[string]any{
			{
				"id": "storefront", "kind": "frontend", "name": "Storefront", "tech": "HTML",
				"why":     "One page that lists the products and takes an order.",
				"machine": machineWeb, "stack": "base", "size": "small", "disk_gb": 10, "port": portWeb,
			},
			{
				"id": "api", "kind": "backend", "name": "Orders API", "tech": "Python",
				"why":     "Serves the catalogue and takes orders, with its API described for the owner to try.",
				"machine": machineAPI, "stack": "base", "size": apiSize, "disk_gb": 10, "port": portAPI,
			},
			{
				"id": "database", "kind": "database", "name": "Catalogue", "tech": "PostgreSQL",
				"why":     "Holds the products.",
				"machine": machineDB, "stack": "data", "size": "small", "disk_gb": 10, "port": portDB,
			},
			{
				"id": "ci", "kind": "ci", "name": "Checks", "tech": "Shell",
				"why":     "Fetches the API's code and checks that it builds.",
				"machine": machineCI, "stack": "base", "size": "small", "disk_gb": 10,
			},
		},
		"edges": []map[string]any{
			{"from": "storefront", "to": "api", "label": "REST over HTTP", "env": "API_URL", "path": "/api"},
			{"from": "api", "to": "database", "label": "SQL", "env": "DATABASE_HOST"},
			{"from": "ci", "to": "api", "label": "git"},
		},
	}
}

// What the platform says back when a design is put to the user.
const approvedPrefix = "The user approved the design"

// describePause is how long the API's agent takes to add the description it
// left out.
const describePause = 10 * time.Second

// assistantReply is the assistant's next message.
func assistantReply(c conversation) reply {
	// A tool that failed is read and reacted to, as a model would: here by
	// saying so and stopping, so that a failure is seen and not built over.
	if failure := c.failure(); failure != "" {
		return say("That did not work, so I have stopped here. " + failure)
	}

	if !c.asked("ask") && !c.asked("propose") {
		return ask("ask", "ask_user", map[string]any{
			"question": "How many people do you expect to use the shop at once?",
			"options": []map[string]any{
				{"label": "A few dozen", "description": "One small machine for each part is enough."},
				{"label": "Thousands", "description": "Larger machines, and the API sized to take the load."},
			},
		}).with("Before I design this, one question.")
	}

	if verdict, _ := c.result("propose"); !strings.HasPrefix(verdict, approvedPrefix) {
		// Not put to the user yet, or they asked for something else.
		changed := c.asked("propose")
		text := "Here is the design I would build. Change anything on the diagram, then approve it."
		if changed {
			text = "I have changed the design as you asked. Here it is again."
		}

		return ask("propose", "propose_architecture", shopDesign(changed)).with(text)
	}

	switch {
	case !c.asked("create-" + machineWeb):
		return reply{}.
			with("The design is approved. Creating its four machines.").
			and("create-"+machineWeb, "create_machine", map[string]any{"name": machineWeb, "size": "small", "disk_gb": 10, "stack": "base"}).
			and("create-"+machineAPI, "create_machine", map[string]any{"name": machineAPI, "size": "small", "disk_gb": 10, "stack": "base"}).
			and("create-"+machineDB, "create_machine", map[string]any{"name": machineDB, "size": "small", "disk_gb": 10, "stack": "data"}).
			and("create-"+machineCI, "create_machine", map[string]any{"name": machineCI, "size": "small", "disk_gb": 10, "stack": "base"})

	case !c.asked("task-" + agentDatabase):
		return reply{}.
			with("The machines are ready. Starting on the database and the API together.").
			and("task-"+agentDatabase, "code_on_machine", map[string]any{
				"machine": machineDB,
				"agent":   agentDatabase,
				"task":    "You are building the Catalogue component of the design: PostgreSQL. Create a database cluster under /home/coder/shop-db that accepts connections from the owner's other machines, register it with run_service on port 5432 so it keeps running, then create a database called shop with a products table (id, name, price_cents) holding three products. Leave a note saying how to connect to it.",
			}).
			and("task-"+agentAPI, "code_on_machine", map[string]any{
				"machine": machineAPI,
				"agent":   agentAPI,
				"task":    "You are building the Orders API component of the design, in /home/coder/api, with Python's standard library only. It listens on 0.0.0.0:8000 and serves, under /api: GET /api/health, GET /api/products (with an optional max_price), GET /api/products/{id}, POST /api/orders, and GET /api/orders for callers who send the admin token. It must serve its API description at /openapi.json. Register it with run_service, commit your work, serve the repository with git daemon as a service called git on port 9418, and leave a note describing the API.",
			})

	case !c.asked("task-" + agentStorefront):
		return ask("task-"+agentStorefront, "code_on_machine", map[string]any{
			"machine": machineWeb,
			"agent":   agentStorefront,
			"task":    "You are building the Storefront component of the design, in /home/coder/web: one HTML page that lists the products from the Orders API and lets a visitor order one. The page calls /api on its own address, never another machine's. Ask the agent \"api\" on machine \"api\" what its endpoints return before you write the page. Serve the folder on 0.0.0.0:3000, registered with run_service, and commit your work.",
		}).with("The database and the API are up. Now the storefront.")

	case !c.asked("task-" + agentCI):
		return ask("task-"+agentCI, "code_on_machine", map[string]any{
			"machine":    machineCI,
			"agent":      agentCI,
			"background": true,
			"task":       "You are the Checks component of the design. Clone the Orders API's repository from git://api/api into /home/coder/checks/api, check that its code compiles, write the steps as a script at /home/coder/checks/run.sh that can be run again, and report which checks pass.",
		}).with("The storefront is up. Last, the checks.")

	case c.last().Role == "tool" && !c.asked("preview-"+machineWeb):
		// The checks have only just been started, in the background.
		return say("The shop is running. The checks are fetching the API's code in the background; I will report when they have finished.")

	case !c.asked("preview-" + machineWeb):
		return reply{}.
			with("The checks have finished.").
			and("preview-"+machineWeb, "show_preview", map[string]any{"machine": machineWeb, "port": portWeb, "label": "Storefront"}).
			and("preview-"+machineAPI, "show_preview", map[string]any{"machine": machineAPI, "port": portAPI, "label": "Orders API"})
	}

	return say(shopSummary)
}

// shopSummary is the assistant's last word. It has a table and some code in
// it because the interface draws both.
const shopSummary = "The shop is built and running.\n\n" +
	"| Machine | What runs there | Port |\n" +
	"|---|---|---|\n" +
	"| web | Storefront, one HTML page | 3000 |\n" +
	"| api | Orders API, Python | 8000 |\n" +
	"| db | PostgreSQL, database `shop` | 5432 |\n" +
	"| ci | Checks for the API | |\n\n" +
	"The storefront calls the API at `/api` on its own address. To place an order by hand:\n\n" +
	"```bash\n" +
	"curl -X POST http://api:8000/api/orders \\\n" +
	"  -H 'Content-Type: application/json' \\\n" +
	"  -d '{\"product_id\": 1, \"quantity\": 2}'\n" +
	"```\n\n" +
	"The API's description is in the Backend tab, where each endpoint can be tried."

// failure returns what went wrong, if the tool results that have just come
// back include a failure.
func (c conversation) failure() string {
	var failed []string
	for i := len(c.messages) - 1; i >= 0 && c.messages[i].Role == "tool"; i-- {
		if text := string(c.messages[i].Content); strings.HasPrefix(text, "Error: ") {
			failed = append(failed, strings.TrimPrefix(text, "Error: "))
		}
	}

	return strings.Join(failed, " ")
}

// agentReply is a coding agent's next message.
func agentReply(c conversation) reply {
	t, ok := c.currentTask()
	if !ok {
		return say("I was given nothing to do.")
	}

	if t.Question {
		return say(answerOf(t.Agent))
	}
	if t.Direct {
		return directReply(t)
	}

	switch t.Agent {
	case agentDatabase:
		return databaseAgent(t)
	case agentAPI:
		return apiAgent(t)
	case agentStorefront:
		return storefrontAgent(t)
	case agentCI:
		return ciAgent(t)
	}

	return say(fmt.Sprintf("Nothing was done: the stand-in for the model has no script for an agent called %q.", t.Agent))
}

// attachedFiles finds the paths of files the owner gave with a task.
var attachedFiles = regexp.MustCompile(`/home/coder/\.coder/attachments/[^\s]+`)

// directReply is what an agent does when the owner writes to it in the chat.
// It shows what it was given: the files, listed from the machine's disk. And
// asked about a design, it draws a mockup and asks the owner to choose.
func directReply(t task) reply {
	c := t.Since
	files := attachedFiles.FindAllString(t.Text, -1)
	design := strings.Contains(strings.ToLower(t.Text), "mockup") || strings.Contains(strings.ToLower(t.Text), "design")

	switch {
	case len(files) > 0 && !c.asked("files"):
		return c.bash("files", "List the files the owner gave", "ls -l "+strings.Join(files, " "))

	case design && !c.asked("mockup"):
		return c.write("mockup", "/home/coder/mockups/minimal.svg", mockupSVG)

	case design && !c.asked("askowner"):
		return c.call("askowner", "ask_owner", map[string]any{
			"agent_name": t.Handle,
			"question":   "Which design language should I follow? I drew the minimal one.",
			"options":    []string{"Minimal", "Dense"},
			"files":      []string{"/home/coder/mockups/minimal.svg"},
		})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "I'm %s, the %s agent on %s.", t.Handle, t.Agent, t.Machine)
	if listed, ok := c.result("files"); ok {
		fmt.Fprintf(&b, " The files you gave me are here:\n\n```\n%s\n```\n\n", strings.TrimSpace(listed))
	}
	if asked, ok := c.result("askowner"); ok {
		fmt.Fprintf(&b, "I've put a mockup in the chat and asked you to choose. %s", firstSentence(asked))
	}
	return say(b.String())
}

const mockupSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="200" viewBox="0 0 320 200"><rect width="320" height="200" fill="#f4f4f5"/><rect x="16" y="16" width="288" height="40" rx="10" fill="#fff"/><rect x="16" y="72" width="136" height="112" rx="10" fill="#fff"/><rect x="168" y="72" width="136" height="112" rx="10" fill="#fff"/></svg>`

// answerOf is what an agent says when another agent asks it something.
func answerOf(agent string) string {
	if agent == agentAPI {
		return "The Orders API listens on port 8000 on machine \"api\", and every route is under /api. " +
			"GET /api/products returns a JSON array of {\"id\": integer, \"name\": string, \"price_cents\": integer}; max_price in the query keeps those costing at most that. " +
			"GET /api/products/{id} returns one of them, or 404. " +
			"POST /api/orders takes {\"product_id\": integer, \"quantity\": integer} and returns 201 with {\"id\", \"product_id\", \"quantity\", \"total_cents\"}. " +
			"The full description is at /openapi.json."
	}

	return "I am " + agent + ". I have nothing written down about that; look at my reports with get_agent."
}

// databaseAgent sets PostgreSQL up on the data machine and has the platform
// keep it running.
func databaseAgent(t task) reply {
	c := t.Since

	switch {
	case !c.asked("init"):
		return c.bash("init", "Create the database cluster", pgInit)

	case !c.asked("serve"):
		return c.call("serve", "run_service", map[string]any{
			"name":      "postgres",
			"command":   pgBin + "/postgres -D " + pgData,
			"directory": "/home/coder",
			"port":      fmt.Sprint(portDB),
		})

	case !c.asked("fill"):
		return c.bash("fill", "Create the shop database and its products", pgFill)

	case !c.asked("note"):
		return c.call("note", "share_note", map[string]any{
			"agent": t.Agent,
			"topic": "database",
			"text":  "PostgreSQL 16 on machine \"db\", port 5432. Database \"shop\", user \"shop\", no password from the owner's machines. Table products (id serial, name text, price_cents int), three rows.",
		})
	}

	served, _ := c.result("serve")
	filled, _ := c.result("fill")

	return say("PostgreSQL is set up on this machine.\n\n" +
		"- The cluster is in /home/coder/shop-db and is registered as the service \"postgres\" on port 5432.\n" +
		"- Database \"shop\", user \"shop\"; the products table has " + lastNumber(filled) + " rows.\n" +
		"- " + firstSentence(afterKept(served)))
}

const (
	pgBin  = "/usr/lib/postgresql/16/bin"
	pgData = "/home/coder/shop-db"

	pgInit = `set -e
if [ ! -f ` + pgData + `/PG_VERSION ]; then
  ` + pgBin + `/initdb -D ` + pgData + ` -U shop --auth=trust > /dev/null
  echo "listen_addresses = '*'" >> ` + pgData + `/postgresql.conf
  echo "host all all 0.0.0.0/0 trust" >> ` + pgData + `/pg_hba.conf
fi
echo "cluster ready in ` + pgData + `"`

	pgFill = `set -e
export PATH=` + pgBin + `:$PATH
psql -h 127.0.0.1 -U shop -d postgres -Atc "select 1 from pg_database where datname = 'shop'" | grep -q 1 || createdb -h 127.0.0.1 -U shop shop
psql -h 127.0.0.1 -U shop -d shop -q -c "create table if not exists products (id serial primary key, name text not null, price_cents int not null)"
psql -h 127.0.0.1 -U shop -d shop -q -c "insert into products (name, price_cents) select * from (values ('Mug', 1200), ('Notebook', 800), ('Pen', 300)) as v where not exists (select 1 from products)"
psql -h 127.0.0.1 -U shop -d shop -Atc "select count(*) from products"`
)

// apiAgent writes the orders API. It first writes it without the description
// of its API, as an agent that forgot would, is told so when it registers
// the service, and puts it right.
func apiAgent(t task) reply {
	c := t.Since

	switch {
	case !c.asked("write"):
		return c.write("write", "/home/coder/api/server.py", serverPy(false))

	case !c.asked("serve"):
		return apiService(c, "serve")
	}

	// The platform asks a backend for its API description when it comes up,
	// and says so in what it tells the agent.
	if first, _ := c.result("serve"); strings.Contains(first, "But ") {
		switch {
		case !c.asked("describe"):
			// A model takes a while to write a file. For that long the
			// backend is up without its description, which anything
			// watching it has to cope with.
			return c.write("describe", "/home/coder/api/server.py", serverPy(true)).
				with("The platform says the API has no description. Adding /openapi.json.").
				after(describePause)

		case !c.asked("serve-again"):
			return apiService(c, "serve-again")
		}
	}

	switch {
	case !c.asked("commit"):
		return c.bash("commit", "Commit the API", commit("/home/coder/api", "Orders API"))

	case !c.asked("git"):
		return c.call("git", "run_service", map[string]any{
			"name":      "git",
			"command":   "git daemon --reuseaddr --base-path=/home/coder --export-all /home/coder",
			"directory": "/home/coder",
			"port":      fmt.Sprint(portGit),
		})

	case !c.asked("note"):
		return c.call("note", "share_note", map[string]any{
			"agent": t.Agent,
			"topic": "orders api",
			"text":  answerOf(agentAPI),
		})
	}

	served, ok := c.result("serve-again")
	if !ok {
		served, _ = c.result("serve")
	}

	return say("The Orders API is written and running.\n\n" +
		"- Code: /home/coder/api/server.py, committed, and served with git daemon on port 9418.\n" +
		"- Service \"api\" on port 8000, every route under /api.\n" +
		"- " + firstSentence(afterKept(served)) + lastSentence(served))
}

func apiService(c conversation, step string) reply {
	return c.call(step, "run_service", map[string]any{
		"name":      "api",
		"command":   "python3 server.py",
		"directory": "/home/coder/api",
		"port":      fmt.Sprint(portAPI),
	})
}

// storefrontAgent asks the API's agent what it serves, then writes a page
// that calls it.
func storefrontAgent(t task) reply {
	c := t.Since

	switch {
	case !c.asked("ask"):
		return c.call("ask", "ask_agent", map[string]any{
			"agent_name": t.Agent,
			"machine":    machineAPI,
			"agent":      agentAPI,
			"question":   "What does GET /api/products return, and what does POST /api/orders take and return?",
		})

	case !c.asked("write"):
		return c.write("write", "/home/coder/web/index.html", indexHTML)

	case !c.asked("serve"):
		return c.call("serve", "run_service", map[string]any{
			"name":      "web",
			"command":   fmt.Sprintf("python3 -m http.server %d --bind 0.0.0.0", portWeb),
			"directory": "/home/coder/web",
			"port":      fmt.Sprint(portWeb),
		})

	case !c.asked("commit"):
		return c.bash("commit", "Commit the page", commit("/home/coder/web", "Storefront"))
	}

	served, _ := c.result("serve")

	return say("The storefront is written and running.\n\n" +
		"- Page: /home/coder/web/index.html, committed.\n" +
		"- It lists the products from GET /api/products and orders with POST /api/orders, both on its own address; the API's agent told me their shapes.\n" +
		"- Service \"web\" on port 3000. " + firstSentence(afterKept(served)))
}

// ciAgent fetches the API's code from its machine and checks it.
func ciAgent(t task) reply {
	c := t.Since

	switch {
	case !c.asked("clone"):
		return c.bash("clone", "Fetch the API's code and check it", ciRun)

	case !c.asked("script"):
		return c.write("script", "/home/coder/checks/run.sh", "#!/bin/bash\n"+ciRun+"\n")
	}

	checked, _ := c.result("clone")
	verdict := "api: FAIL"
	if strings.Contains(checked, "api: PASS") {
		verdict = "api: PASS"
	}

	return say("The checks have run.\n\n" +
		"- " + verdict + " (cloned from git://api/api, compiled with python3 -m py_compile).\n" +
		"- The steps are in /home/coder/checks/run.sh and can be run again.")
}

const ciRun = `set -e
mkdir -p /home/coder/checks
rm -rf /home/coder/checks/api
git clone -q git://api/api /home/coder/checks/api
cd /home/coder/checks/api
python3 -m py_compile server.py
echo "api: PASS"`

// commit is a command that commits everything in a folder, making it a
// repository first if it is not one.
func commit(folder, message string) string {
	return fmt.Sprintf(`set -e
cd %s
[ -d .git ] || git init -q
git add -A
git commit -q -m %q || true
git log --oneline | head -n 1`, folder, message)
}

// bash asks for a command to be run with the coding agent's own tool.
func (c conversation) bash(step, description, command string) reply {
	return c.call(step, "bash", map[string]any{
		"command":     command,
		"description": description,
	})
}

// write asks for a file to be written with the coding agent's own tool.
func (c conversation) write(step, path, text string) reply {
	return c.call(step, "write", map[string]any{
		"filePath": path,
		"content":  text,
	})
}

// afterKept drops the platform's standing first sentences about a service,
// leaving what it found when it looked.
func afterKept(result string) string {
	if _, found, ok := strings.Cut(result, ".log. "); ok {
		return found
	}

	return result
}

func firstSentence(s string) string {
	if first, _, ok := strings.Cut(s, ". "); ok {
		return first + "."
	}

	return s
}

// lastSentence returns " " and the sentence about the API description, if
// the result has one.
func lastSentence(s string) string {
	if at := strings.Index(s, "Its API description"); at >= 0 {
		return " " + s[at:]
	}

	return ""
}

// lastNumber returns the last line of s that is a whole number, which is how
// a command's count comes back whatever the agent's tool prints around it.
func lastNumber(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" && strings.Trim(line, "0123456789") == "" {
			return line
		}
	}

	return "no"
}
