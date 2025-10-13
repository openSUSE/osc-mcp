FROM registry.opensuse.org/opensuse/tumbleweed:latest AS builder
RUN zypper --non-interactive install -y go tar gzip
WORKDIR /build
COPY . .
RUN go build -o osc-mcp .

FROM registry.opensuse.org/opensuse/tumbleweed:latest

# Install osc and build tools
RUN zypper --non-interactive install -y \
    osc \
    build \
    ca-certificates \
    && zypper clean -a

COPY --from=builder /build/osc-mcp /usr/local/bin/

# Create workspace directory
WORKDIR /workspace

# Environment variables for authentication
ENV OSC_MCP_API="api.opensuse.org"
ENV OSC_MCP_USER=""
ENV OSC_MCP_PASSWORD=""
ENV OSC_MCP_WORKDIR="/workspace"

EXPOSE 8666

ENTRYPOINT ["/usr/local/bin/osc-mcp"]
CMD ["--http", "0.0.0.0:8666", "--workdir", "/workspace", "--clean-workdir", "-v"]
