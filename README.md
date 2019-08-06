# rit-k8s-rdma-scheduler-extender

This is a Kubernetes Scheduler Extension for use within an RDMA deployment.

## Docker

### Build
To build the scheduler extender as a docker

Run:

```
docker build -t <tag-name> .
```

### Running

You can run it from docker hub or a local build

#### Local
Run:
```
docker run -it --rm --name my-scheduler-extension -e PORT=5000 --network host <name>
```
This will do the following:
  - `-it` - runs in interactive mode
  - `--rm` - removes the container when stopped
  - `-e PORT=5000` - specifies the port for the server to run on, if none is specified it will default to port 8888
  - `--network host` - will share the network with your host OS, so you can access the api by going to localhost:5000


#### Dockerhub
Run:
```
docker run -it --rm --name my-scheduler-extension -e PORT=5000 --network host ritk8srdma/rit-k8s-rdma-scheduler-extender
```
This will do the following:
  - `-it` - runs in interactive mode
  - `--rm` - removes the container when stopped
  - `-e PORT=5000` - specifies the port for the server to run on, if none is specified it will default to port 8888
  - `--network host` - will share the network with your host OS, so you can access the api by going to localhost:5000