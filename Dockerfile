FROM golang:1.21-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    alsa-utils \
    build-essential \
    ca-certificates \
    cmake \
    curl \
    git \
    libasound2-dev \
    pkg-config \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace

CMD ["sleep", "infinity"]
