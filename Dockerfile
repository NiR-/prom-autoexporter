FROM golang:1.11-alpine as builder

ENV SRC_DIR ${GOPATH}/src/github.com/NiR-/prom-autoexporter/

RUN apk add --no-cache ca-certificates git && \
    go get github.com/golang/dep/cmd/dep

COPY Gopkg.* ${SRC_DIR}/
WORKDIR ${SRC_DIR}/

RUN dep ensure -v -vendor-only

COPY . ${SRC_DIR}/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /go/bin/prom-autoexporter

################################################################################

FROM scratch

COPY --from=builder /go/bin/prom-autoexporter /go/bin/prom-autoexporter

ENTRYPOINT ["/go/bin/prom-autoexporter"]
