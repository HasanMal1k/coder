# The machine image every user instance boots from.

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

USER coder
WORKDIR /home/coder

# Keeps the machine alive; terminals are opened as separate processes. This is
# replaced by the guest daemon once it exists.
CMD ["sleep", "infinity"]
