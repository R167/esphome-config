FROM golang:1.22
WORKDIR /src
COPY go.* *.go /src/
COPY cmd /src/cmd

RUN go build -o /bin/serve ./cmd/serve

FROM gcr.io/distroless/static
COPY --from=0 /bin/serve /bin/serve

ENTRYPOINT [ "/bin/serve" ]
