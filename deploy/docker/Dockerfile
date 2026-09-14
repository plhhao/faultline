FROM golang:1.26.4-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -o /out/faultline ./cmd/faultline \
    && mkdir -p /out/tmp

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/faultline /faultline
COPY --from=build --chown=65532:65532 /out/tmp /tmp
USER 65532:65532
ENTRYPOINT ["/faultline"]
