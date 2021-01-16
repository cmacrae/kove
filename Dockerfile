FROM golang:alpine as builder
RUN apk add --no-cache git ca-certificates && update-ca-certificates
ENV UID=10001
RUN adduser \
	--disabled-password \
	--gecos "" \
	--home "/nonexistent" \
	--no-create-home \
	--shell "/sbin/nologin" \
	--uid 10001 \
	kube-opa-violation-exporter

WORKDIR /kube-opa-violation-exporter
COPY kube-opa-violation-exporter.go config.go go.mod go.sum /kube-opa-violation-exporter/
RUN go mod download
RUN go mod verify
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o kube-opa-violation-exporter
RUN chown -R kube-opa-violation-exporter:kube-opa-violation-exporter /kube-opa-violation-exporter

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /kube-opa-violation-exporter /kube-opa-violation-exporter
WORKDIR /kube-opa-violation-exporter
USER kube-opa-violation-exporter:kube-opa-violation-exporter
EXPOSE 3000
ENTRYPOINT ["/kube-opa-violation-exporter/kube-opa-violation-exporter"]
