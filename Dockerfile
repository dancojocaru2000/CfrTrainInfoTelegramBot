FROM --platform=$BUILDPLATFORM tonistiigi/xx AS xx

FROM --platform=$BUILDPLATFORM golang:1.21-alpine AS build

LABEL authors="kbruen"
LABEL org.opencontainers.image.source=https://github.com/dancojocaru2000/CfrTrainInfoTelegramBot

RUN apk add tzdata

RUN echo "@testing https://dl-cdn.alpinelinux.org/alpine/edge/testing" >> /etc/apk/repositories
RUN apk add zig@testing
COPY --from=xx / /
ARG TARGETPLATFORM
ENV CGO_ENABLED=1

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY main.go ./
COPY pkg ./pkg/
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_CFLAGS="-D_LARGEFILE64_SOURCE" CC="zig cc -target $(xx-info march)-$(xx-info os)-$(xx-info libc)" CXX="zig c++ -target $(xx-info march)-$(xx-info os)-$(xx-info libc)" go build -o server && xx-verify server

FROM scratch
COPY --from=build /etc/ssl/certs /etc/ssl/certs
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
WORKDIR /app
# COPY static ./static/
COPY --from=build /app/server ./

ENV DEBUG=false
ENTRYPOINT [ "/app/server" ]
