# See docs/decisions/010-docker-image.md for why this base image and why
# openssh-client is here at all — it's the one non-obvious line below.
# Pinned by digest, not just the "3.20" tag, matching this project's
# pinning discipline everywhere else (SHA-pinned Actions, an exact
# GoReleaser version) — the tag stays alongside it so Dependabot's docker
# ecosystem (.github/dependabot.yml) can still propose readable bumps.
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

# ca-certificates: tcp+tls Docker hosts (docs/decisions/004) need a real
# root store to verify against, same as any other HTTPS/TLS client would.
# openssh-client: ssh:// Docker hosts (docs/decisions/009) work by exec'ing
# the system `ssh` binary — without it, that address scheme silently has
# no `ssh` to exec, only failing at connection time instead of at image
# build time, which is a worse place to discover it's missing.
RUN apk add --no-cache ca-certificates openssh-client

# GoReleaser's dockers_v2 builder lays the platform-matched binary its own
# build produced into this Dockerfile's build context under
# "$TARGETPLATFORM/dashsync" (buildx sets TARGETPLATFORM automatically per
# platform it builds for) — not a flat "dashsync" at the context root,
# since one dockers_v2 entry builds every platform in one buildx run.
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/dashsync /usr/local/bin/dashsync

# No USER directive — this image runs as root deliberately, not by
# oversight. Its one real use case, reading /var/run/docker.sock, needs
# access equivalent to root regardless of the container's own UID (socket
# ownership/permissions on the host, not anything a container-side USER
# line controls, decide that) — dropping to a non-root UID here would be
# a cosmetic gesture, not a real privilege reduction, for this specific
# image's actual job.
ENTRYPOINT ["dashsync"]
