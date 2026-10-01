# The machine image every user instance boots from.
#
# Build from the repository root, so the guest daemon's source is in the
# build context:
#
#   docker build -f images/workspace/Dockerfile -t coder-workspace:dev .

FROM golang:1.26-alpine AS guestd
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/guestd ./cmd/guestd
COPY internal/guest ./internal/guest
COPY internal/guestapi ./internal/guestapi
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/guestd ./cmd/guestd

FROM ubuntu:24.04

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
        sudo \
        unzip \
        vim \
        wget \
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

COPY --from=guestd /out/guestd /usr/local/bin/guestd

USER coder
WORKDIR /home/coder

# Editor defaults for a new machine. They live in the home folder, so each
# user's own changes to them persist. VS Code's built-in chat is turned off:
# it depends on a service that isn't available here.
RUN mkdir -p /home/coder/.local/share/code-server/User \
    && printf '%s\n' \
        '{' \
        '  "workbench.startupEditor": "none",' \
        '  "workbench.colorTheme": "Default Dark Modern",' \
        '  "workbench.secondarySideBar.defaultVisibility": "hidden",' \
        '  "chat.disableAIFeatures": true,' \
        '  "telemetry.telemetryLevel": "off"' \
        '}' \
        > /home/coder/.local/share/code-server/User/settings.json

# The guest daemon is the machine's main process: it connects to the control
# plane and runs the user's terminals.
CMD ["guestd"]
