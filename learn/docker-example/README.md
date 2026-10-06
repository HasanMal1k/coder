# Docker and Compose, by example

A small Go program that counts visits in Postgres, a Dockerfile that packs it
into an image, and a Compose file that runs it with its database. The three
files are short and every line in them says why it is there:

- [main.go](main.go): the program.
- [Dockerfile](Dockerfile): how its image is built.
- [docker-compose.yml](docker-compose.yml): how it runs with a database.

Read those first, then work through this page with a terminal open in this
folder. It uses port 8090 and its own database, so the project's Postgres on
5432 and server on 8080 are left alone.

Parts 1 and 2 were run as written in October 2026, with Docker 29.1 and
Compose 2.40, and the output shown is what came back. The output shown in
Part 3 is real too; the changes it asks you to make (moving a line, swapping
a base image) are yours to try.

## The words

- **Image**: a package holding files and the command to run. Read-only.
  Built from a Dockerfile, in **layers**: one per instruction, each stored
  once and reused.
- **Container**: an image, running. It is an ordinary process on your
  computer that the kernel has given its own view of files, network and
  other processes. It is not a virtual machine; it shares your kernel.
- **Volume**: storage Docker looks after, which outlives containers.
- **Bind mount**: a folder of yours, shown inside a container.
- **Network**: a private network. Containers on the same one reach each
  other by name.
- **Registry**: where images are pulled from and pushed to (Docker Hub,
  `gcr.io`, and so on).
- **Compose**: one file that describes several containers and how they
  connect.

## Part 1: by hand

Doing it by hand once is what makes the Compose file readable afterwards.

**Build the image.**

```bash
docker build -t hits:dev .
docker images hits:dev
```

```
hits:dev 21.2MB
```

The image it was compiled in, `golang:1.26-alpine`, is 364 MB. What ships
is a seventeenth of that, because the second stage of the Dockerfile takes
only the finished program.

**Run it.**

```bash
docker run --rm hits:dev
```

```
DATABASE_URL is required
```

A container has only the environment it is given. `--rm` removes the
container when it exits.

**Give it a network and a database.**

```bash
docker network create hits-net

docker run -d --name hits-db --network hits-net \
  -e POSTGRES_USER=hits -e POSTGRES_PASSWORD=hits -e POSTGRES_DB=hits \
  -v hits-data:/var/lib/postgresql/data \
  postgres:17
```

`-d` runs it in the background, `-e` sets an environment variable, and `-v`
puts a volume named `hits-data` where Postgres keeps its files.

**Create the table**, at once, without waiting:

```bash
docker run --rm --network hits-net \
  -v "$PWD/migrations:/migrations:ro" \
  migrate/migrate:v4.18.3 \
  -path=/migrations \
  -database "postgres://hits:hits@hits-db:5432/hits?sslmode=disable" up
```

```
error: failed to open database: dial tcp 172.20.0.2:5432: connect: connection refused
```

This is the first real lesson. The container had *started*, and the database
inside it was not yet *ready*. Wait a few seconds and run the same command
again:

```
1/u create_counters (6.482945ms)
```

Notice the address in the command: `hits-db`. On a network you made, a
container's name is its hostname.

**Run the program.**

```bash
docker run -d --name hits-app --network hits-net \
  -e DATABASE_URL="postgres://hits:hits@hits-db:5432/hits?sslmode=disable" \
  -p 127.0.0.1:8090:8080 \
  hits:dev

curl localhost:8090
curl localhost:8090
```

```
{"served_by":"42f0e53efe90","visits":1}
{"served_by":"42f0e53efe90","visits":2}
```

`-p 127.0.0.1:8090:8080` reads "port 8090 on this computer, on the loopback
address only, goes to port 8080 in the container". `served_by` is the
container's hostname, which is its ID.

**Look around.**

```bash
docker ps                                   # running containers
docker logs hits-app                        # what the program printed
docker inspect --format '{{.State.Health.Status}}' hits-app
docker exec hits-app sh                     # try to get a shell inside it
```

```
healthy
exec: "sh": executable file not found in $PATH
```

There is no shell in the image. That is on purpose: nothing is in it but the
program. The database's image does have one, so
`docker exec -it hits-db psql -U hits` works.

**Clean up.**

```bash
docker rm -f hits-app hits-db
docker volume rm hits-data
docker network rm hits-net
```

## Part 2: the same, written down

Open [docker-compose.yml](docker-compose.yml) beside what you just typed.

| By hand | In Compose |
|---|---|
| `docker build -t hits:dev .` | `build: .` |
| `docker network create`, `--network` | nothing: every project gets a network |
| `--name hits-db` | the service's name, `db`, which is also its hostname |
| `-e NAME=value` | `environment:` |
| `-v hits-data:/var/lib/...` | `volumes:` on the service, and `volumes:` at the bottom |
| `-v "$PWD/migrations:/migrations:ro"` | `./migrations:/migrations:ro` |
| `-p 127.0.0.1:8090:8080` | `ports:` |
| waiting, then running the command again | `healthcheck:` and `depends_on:` with a `condition` |

```bash
docker compose up --build -d --wait
docker compose ps -a
curl localhost:8090
```

```
NAME             SERVICE   STATUS                     PORTS
hits-app-1       app       Up 5 seconds (healthy)     127.0.0.1:8090->8080/tcp
hits-db-1        db        Up 9 seconds (healthy)     5432/tcp
hits-migrate-1   migrate   Exited (0) 6 seconds ago
```

Compose started the database, waited until it was healthy, ran the migration,
waited for it to finish with exit code 0, and only then started the program.
That is the waiting you did by hand, written as `depends_on`.

Other commands worth knowing:

```bash
docker compose logs -f app        # follow one service's output
docker compose exec db psql -U hits -c 'SELECT * FROM counters'
docker compose stop app           # stop one service
docker compose down               # remove containers and network; keep data
docker compose down -v            # and delete the volume
```

## Part 3: break it on purpose

Each of these takes a minute. Predict what will happen before you run it.

**1. The layer cache.** Add a comment to `main.go` and build again:

```bash
docker compose build
```

```
[build 2/6] WORKDIR /src                 CACHED
[build 3/6] COPY go.mod go.sum ./        CACHED
[build 4/6] RUN go mod download          CACHED
[build 5/6] COPY . .                     DONE 0.0s
[build 6/6] RUN CGO_ENABLED=0 go build   DONE 15.4s
```

The dependencies were not downloaded again, because the files that step
depends on did not change. Now move `COPY . .` above `RUN go mod download`
in the Dockerfile, change the comment, and build twice more. Every source
edit now downloads everything. Put it back.

**2. What is in the image.** `docker history hits-app` lists its layers and
their sizes. Then change the second `FROM` to `golang:1.26-alpine` and see
what `docker images` says about the size.

**3. Data outlives containers.** Call it a few times, then:

```bash
docker compose down && docker compose up -d --wait && curl localhost:8090
```

The count carries on (`"visits":4` after three). The containers were
destroyed and made again; the volume was not. Now do the same with
`down -v` and it starts from 1.

**4. The database is not reachable from your computer.**

```bash
docker compose port db 5432       # prints ":0": no port is published
```

It publishes no port, so nothing outside the project's network can connect
to it, and the program still can. Compare the project's own
[docker-compose.yml](../../docker-compose.yml), which publishes Postgres as
`"5432:5432"`: with no address in front, that is every network interface.
It is the first open issue in [docs/STATUS.md](../../docs/STATUS.md), and
the fix is the `127.0.0.1:` you see in this folder's file.

**5. A dependency goes away.**

```bash
docker compose stop db
curl -i localhost:8090/healthz    # 503
curl -i localhost:8090            # 503 too, and a line in the log
docker compose logs app | tail -1
docker compose ps                 # after about 15 seconds: (unhealthy)
docker compose start db           # and it recovers by itself
```

```
count a visit: ... hostname resolving error: lookup db on 127.0.0.11:53: no such host
```

`127.0.0.11` is Docker's own name server, present in every container on a
network you made. A stopped container's name stops resolving. Note also
that nothing restarted the program: `unhealthy` is a label, and plain
Docker does not act on it. (Orchestrators such as Kubernetes do.)

**6. Stopping cleanly.**

```bash
time docker compose stop app      # about half a second
docker compose logs app | tail -1 # "shutting down"
docker inspect --format '{{.State.ExitCode}}' hits-app-1   # 0
```

`docker stop` sends SIGTERM, waits ten seconds, then sends SIGKILL. The
program catches SIGTERM (`signal.NotifyContext` in `main.go`), finishes the
requests it is in the middle of and exits with code 0.

A Go program that does *not* catch it was tried too: it died in 0.25
seconds with exit code 2, and a request it was answering got
`curl: (52) Empty reply from server`. To see that for yourself, add a
handler to `main.go` that sleeps for four seconds before answering, call
it, and stop the container while it waits; then replace the
`signal.NotifyContext` line with `ctx := context.Background()` and do it
again.

## Your turn

Do these without copying from this folder; look things up in the
[Dockerfile reference](https://docs.docker.com/reference/dockerfile/) and
the [Compose file reference](https://docs.docker.com/reference/compose-file/).

1. Write a Dockerfile and a Compose file for the API you build in project
   3 of [the curriculum](../README.md). Aim for: an image under 30 MB, not
   running as root, a health check, and `docker compose stop` returning in
   under a second.
2. Add a fourth service to this folder's Compose file: `adminer` (a web page
   for looking inside a database), reachable on `127.0.0.1:8091`.
3. Give `app` a memory limit of 64 MB and half a CPU. Check it with
   `docker stats`.
4. Run two copies of `app` (`docker compose up -d --scale app=2`). It
   fails. Work out why, and what would have to sit in front of them for it
   to work.
5. Make the build faster with a cache mount
   (`RUN --mount=type=cache,target=/go/pkg/mod ...`), and measure it.

## The same ideas in the real project

Once this folder makes sense, these will too:

- [Dockerfile](../../Dockerfile): the server's image. The same two stages
  as here.
- [docker-compose.yml](../../docker-compose.yml): Postgres and a `migrate`
  job, as here. The server itself is not in it, because it has to create
  containers, and so runs on the host where the Docker daemon is.
- [images/workspace/Dockerfile](../../images/workspace/Dockerfile): the
  image users' machines are made from. More stages, and an `ARG` that picks
  which stage the last one is built on, so one file makes five images.
  Look at where `guestd` is copied in, and why it is last.
- [internal/sandbox/docker/docker.go](../../internal/sandbox/docker/docker.go):
  everything you did in Part 1 (`docker volume create`, `network create`,
  `run` with limits and a restart policy, `stop`, `rm`), done from Go
  through Docker's API instead of the command line.
- [Makefile](../../Makefile), `db`: `up -d --wait postgres`, then
  `run --rm migrate`.

Questions an interviewer asks about all this, with answers, are in
[INTERVIEW.md](../INTERVIEW.md#docker).
