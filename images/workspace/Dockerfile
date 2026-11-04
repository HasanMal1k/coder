# The machine images user instances boot from.
#
# There is one image for each "stack": the same machine, with a different
# toolchain already installed. Build from the repository root, so the guest
# daemon's source is in the build context:
#
#   docker build -f images/workspace/Dockerfile -t coder-workspace:dev .
#   docker build -f images/workspace/Dockerfile --build-arg STACK=go -t coder-workspace-go:dev .
#
# or all of them with `make images`. The stacks are: base (Python and
# Node.js), go, cpp, dotnet and data (PostgreSQL and Redis).
#
# The link daemon is not built here. `make link` builds it on the host, as a
# static binary, and leaves it at images/workspace/bin/linkd for this file to
# copy in; `make image` and `make images` do that first.

# Which stack to build. Declared before the first FROM so that the last
# stage can choose what it is built on.
ARG STACK=base

FROM golang:1.26-alpine AS guestd
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/guestd ./cmd/guestd
COPY internal/guest ./internal/guest
COPY internal/guestapi ./internal/guestapi
COPY internal/linkapi ./internal/linkapi
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/guestd ./cmd/guestd

# ---------- what every machine has

FROM ubuntu:24.04 AS os

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        bash-completion \
        build-essential \
        ca-certificates \
        curl \
        git \
        less \
        locales \
        nano \
        openssh-client \
        python3 \
        python3-pip \
        python3-venv \
        sudo \
        unzip \
        vim \
        wget \
        xz-utils \
    && rm -rf /var/lib/apt/lists/* \
    && locale-gen en_US.UTF-8

ENV LANG=en_US.UTF-8

# Ubuntu ships a default "ubuntu" user at uid 1000; replace it with ours so
# file ownership is predictable. Users get passwordless sudo: it's their machine.
RUN userdel -r ubuntu \
    && useradd --create-home --uid 1000 --shell /bin/bash coder \
    && echo "coder ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/coder \
    && chmod 0440 /etc/sudoers.d/coder

# The editor: VS Code in the browser. guestd starts it and the control plane
# proxies to it, so it needs no configuration of its own here.
ARG CODE_SERVER_VERSION=4.139.1
RUN arch="$(dpkg --print-architecture)" \
    && curl -fsSL -o /tmp/code-server.deb \
        "https://github.com/coder/code-server/releases/download/v${CODE_SERVER_VERSION}/code-server_${CODE_SERVER_VERSION}_${arch}.deb" \
    && dpkg -i /tmp/code-server.deb \
    && rm /tmp/code-server.deb

# Node.js, which with Python (above) covers most of what gets built here
# without every project having to start by installing a runtime.
ARG NODE_VERSION=24.21.0
RUN case "$(dpkg --print-architecture)" in \
        amd64) arch=x64 ;; \
        arm64) arch=arm64 ;; \
        *) echo "unsupported architecture" >&2; exit 1 ;; \
    esac \
    && curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${arch}.tar.xz" \
        | tar -xJ -C /usr/local --strip-components=1 --no-same-owner \
            --exclude='*.md' --exclude=LICENSE

# The coding agent: OpenCode. guestd runs it as a server and the platform's
# assistant gives it tasks. It gets its model from the control plane, so no
# API key is stored here or anywhere else on the machine.
ARG OPENCODE_VERSION=1.18.34
RUN case "$(dpkg --print-architecture)" in \
        amd64) arch=x64 ;; \
        arm64) arch=arm64 ;; \
        *) echo "unsupported architecture" >&2; exit 1 ;; \
    esac \
    && curl -fsSL "https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/opencode-linux-${arch}.tar.gz" \
        | tar -xz -C /usr/local/bin opencode \
    && chmod 0755 /usr/local/bin/opencode

# Commits need an author. Agents commit their work as they go, and the user
# can set their own name whenever they like.
RUN git config --system user.name "coder" \
    && git config --system user.email "coder@localhost" \
    && git config --system init.defaultBranch main

# ---------- one stage for each stack
#
# Each adds a toolchain to the common machine. They come before the guest
# daemon is copied in, so changing the daemon doesn't reinstall them.

FROM os AS stack-base

# Go, for networked services.
FROM os AS stack-go
ARG GO_VERSION=1.27.1
RUN arch="$(dpkg --print-architecture)" \
    && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz" | tar -xz -C /usr/local
ENV PATH=/usr/local/go/bin:/home/coder/go/bin:$PATH

# C++: two compilers, the usual build tools, a debugger, and a few libraries
# nearly every service ends up needing.
FROM os AS stack-cpp
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ccache \
        clang \
        clang-format \
        clang-tidy \
        clangd \
        cmake \
        gdb \
        libasio-dev \
        libfmt-dev \
        libgtest-dev \
        libssl-dev \
        ninja-build \
        nlohmann-json3-dev \
        pkg-config \
        valgrind \
    && rm -rf /var/lib/apt/lists/*

# .NET, for ASP.NET Core and C#.
FROM os AS stack-dotnet
RUN apt-get update \
    && apt-get install -y --no-install-recommends dotnet-sdk-10.0 \
    && rm -rf /var/lib/apt/lists/*
ENV DOTNET_CLI_TELEMETRY_OPTOUT=1 \
    DOTNET_NOLOGO=1

# Data stores: PostgreSQL and Redis, installed but not running. Whoever sets
# one up keeps its data under /home/coder, the part of the machine that is
# kept, and registers it as a service so that it starts with the machine.
FROM os AS stack-data
RUN apt-get update \
    && apt-get install -y --no-install-recommends postgresql postgresql-contrib redis-server \
    && rm -rf /var/lib/apt/lists/* \
    # PostgreSQL keeps its socket here. It will be run by the machine's
    # user, not by the "postgres" user the package assumes.
    && chown -R coder:coder /var/run/postgresql
ENV PATH=/usr/lib/postgresql/16/bin:$PATH

# ---------- the machine

FROM stack-${STACK}

COPY --from=guestd /out/guestd /usr/local/bin/guestd

# The link daemon: the machine's end of the distributed harness, kept
# running by guestd. Built by `make link`.
COPY --chmod=0755 images/workspace/bin/linkd /usr/local/bin/linkd

USER coder
WORKDIR /home/coder

# Editor defaults for a new machine. They live in the home folder, so each
# user's own changes to them persist. VS Code's built-in chat is turned off:
# it depends on a service that isn't available here. Its command centre and
# layout buttons are off because the app around it has a title bar of its own.
RUN mkdir -p /home/coder/.local/share/code-server/User \
    && printf '%s\n' \
        '{' \
        '  "workbench.startupEditor": "none",' \
        '  "workbench.colorTheme": "Default Dark Modern",' \
        '  "workbench.secondarySideBar.defaultVisibility": "hidden",' \
        '  "workbench.layoutControl.enabled": false,' \
        '  "window.commandCenter": false,' \
        '  "chat.disableAIFeatures": true,' \
        '  "telemetry.telemetryLevel": "off"' \
        '}' \
        > /home/coder/.local/share/code-server/User/settings.json

# The guest daemon is the machine's main process: it connects to the control
# plane, runs the user's terminals, and keeps the editor, the coding agent
# and the link daemon running.
CMD ["guestd"]
