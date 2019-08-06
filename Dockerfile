FROM golang:1.12 as builder

WORKDIR /go/src/github.com/rit-k8s-rdma/rit-k8s-rdma-scheduler-extender

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app

FROM scratch

WORKDIR /bin
COPY --from=builder /go/src/github.com/rit-k8s-rdma/rit-k8s-rdma-scheduler-extender/app .

CMD ["./app"]