FROM golang:1.11

WORKDIR /front
COPY main.go .
RUN go build --ldflags '-linkmode "external" -extldflags "-static"' -o front main.go

FROM scratch
COPY --from=0 /front/front .
EXPOSE 8080/tcp
CMD ["./front"]
