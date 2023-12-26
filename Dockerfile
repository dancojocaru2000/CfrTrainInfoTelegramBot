FROM --platform=$BUILDPLATFORM golang:1.21-alpine AS build

LABEL authors="kbruen"
LABEL org.opencontainers.image.source=https://github.com/dancojocaru2000/CfrTrainInfoTelegramBot

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY main.go ./
COPY pkg ./pkg/
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o server

FROM scratch
COPY --from=build /etc/ssl/certs /etc/ssl/certs
WORKDIR /app
# COPY static ./static/
COPY --from=build /app/server ./

ENV DEBUG=false
ENTRYPOINT [ "/app/server" ]
