ARG BASE_SCRATCH=registry.deckhouse.io/base_images/scratch@sha256:b054705fcc9f2205777d80a558d920c0b4209efdc3163c22b5bfcb5dda1db5fc
ARG BASE_GOLANG_ALPINE_BUILDER=registry.deckhouse.io/base_images/golang:1.22.3-alpine@sha256:dbf216b880b802c22e3f4f2ef0a78396b4a9a6983cb9b767c5efc351ebf946b0

FROM $BASE_GOLANG_ALPINE_BUILDER as builder

WORKDIR /go/src
ADD go.mod .
ADD go.sum .
RUN go mod download
COPY . .
WORKDIR /go/src/cmd
RUN GOOS=linux GOARCH=amd64 go build -o controller

FROM --platform=linux/amd64 $BASE_SCRATCH
COPY --from=builder /go/src/cmd/controller /go/src/cmd/controller

ENTRYPOINT ["/go/src/cmd/controller"]
