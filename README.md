# kubectl-link

kubectl-link is a kubectl plugin that allows you to access your pods and services without vpn or manually setting up port forwarding.

- It creates a tun device on your machine to route traffic to your kubernetes cluster with automatically setting port forwarding based your network connections.
- !! It's still new and experimental. Please use it with caution.
- !! Works only on MacOS for now.
<https://github.com/user-attachments/assets/fcdc04ce-b657-42d1-9036-f0d1db6647a3>

## Installation

```sh
go install github.com/umutbasal/kubectl-link@latest
```

## Usage

```sh
sudo kubectl link
```

## Visit your pods and services through your browser or curl

```sh
curl http://nginx.default.svc.cluster.local
curl http://172.17.1.1
```
