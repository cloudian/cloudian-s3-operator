# First stage - build the provisioner
FROM golang:1.20.5-alpine3.18 as build
RUN apk add --update build-base

WORKDIR /app

# Copy source tree
COPY go.mod go.sum /app/
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download
COPY cmd/ /app/cmd/

# Build
RUN go build -a -o cloudian-s3-operator ./cmd

# Final stage - distribution image
FROM alpine:3.18

COPY --from=build /app/cloudian-s3-operator /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/cloudian-s3-operator"]
CMD ["-v=2", "-alsologtostderr"]
