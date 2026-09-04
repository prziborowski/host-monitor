# syntax=docker/dockerfile:1

# ---- Builder stage ----
# Build a static (CGO disabled) Linux binary so it runs on a minimal runtime.
ARG BINARY=host-monitor

FROM golang:1.25-alpine AS builder
WORKDIR /src

# Cache dependencies: copy manifests first, download before source changes.
COPY go.mod go.sum ./
RUN go mod download

# Copy the application source and build.
COPY main.go ./
ARG BINARY=host-monitor
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /out/${BINARY} .

# ---- Runtime stage ----
# Minimal image. iputils provides the `ping` binary (with the -W flag the app
# shells out to); ca-certificates is required for outbound HTTPS to api.slack.com.
FROM alpine:3.20

RUN apk add --no-cache iputils ca-certificates \
    && addgroup -g 1000 -S app \
    && adduser -S app -u 1000 -G app

# The app runs as an unprivileged user.
USER app
WORKDIR /app

# NOTE: ICMP ping requires the NET_RAW capability at runtime. This is a
# deployment-level concern and is NOT set here; e.g. in k8s add:
#   securityContext: { capabilities: { add: ["NET_RAW"] } }

# NOTE: The app reads config from $CONFIG_PATH (default config/hosts.json).
# Do NOT bake config/hosts.json into the image (it holds a live Slack token).
# Mount it from a k8s ConfigMap, e.g. at /config/hosts.json and set
#   env: [ { name: CONFIG_PATH, value: /config/hosts.json } ]
ENV CONFIG_PATH=/config/hosts.json

# Copy the static binary built above.
COPY --from=builder /out/host-monitor /usr/local/bin/host-monitor

ENTRYPOINT ["/usr/local/bin/host-monitor"]
