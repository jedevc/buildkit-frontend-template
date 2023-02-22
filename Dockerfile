# syntax=docker/dockerfile:1

ARG GO_VERSION=1.20
ARG XX_VERSION=1.1.0

FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VERSION} AS xx

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-buster AS base
COPY --from=xx / /
RUN apt-get update -y && apt-get install --no-install-recommends -y file git clang lld
ENV CGO_ENABLED=1
WORKDIR /src

FROM base AS build
ARG BUILD_TAGS TARGETPLATFORM
RUN xx-apt-get install -y binutils gcc g++ pkg-config libc6-dev libgcc-8-dev libstdc++-8-dev
RUN XX_CC_PREFER_LINKER=ld xx-clang --setup-target-triple
RUN --mount=type=bind,target=. \
    --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 xx-go build -tags ${BUILD_TAGS} -trimpath -ldflags "-v -s -w -extldflags '-static'" -o /frontend ./cmd/frontend && \
    xx-verify --static /frontend

FROM scratch AS release
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /frontend /
LABEL moby.buildkit.frontend.network.none="false"
LABEL moby.buildkit.frontend.caps="moby.buildkit.frontend.inputs,moby.buildkit.frontend.contexts"
ENTRYPOINT ["/frontend"]

FROM release
