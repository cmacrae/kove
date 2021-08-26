FROM golang:1.17-alpine as builder
RUN apk add --no-cache git ca-certificates && update-ca-certificates
ENV UID=10001
RUN adduser \
	--disabled-password \
	--gecos "" \
	--home "/nonexistent" \
	--no-create-home \
	--shell "/sbin/nologin" \
	--uid 10001 \
	kove

WORKDIR /kove
COPY kove.go config.go go.mod go.sum /kove/
RUN go mod download
RUN go mod verify
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o kove
RUN chown -R kove:kove /kove

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /kove /kove
WORKDIR /kove
USER kove:kove
EXPOSE 3000
ENTRYPOINT ["/kove/kove"]
