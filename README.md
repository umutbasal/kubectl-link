# kubectl-link

kubectl-link is a kubectl plugin that allows you to access your pods and services without vpn or manually setting up port forwarding.

- It creates a tun device on your machine to route traffic to your kubernetes cluster with automatically setting port forwarding based your network connections.
- !! It's still new and experimental. Please use it with caution.
- !! Works only on MacOS for now.

https://github.com/user-attachments/assets/fcdc04ce-b657-42d1-9036-f0d1db6647a3

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

## refs
- https://github.com/xjasonlyu/tun2socks
- https://github.com/google/gvisor
- https://git.zx2c4.com/wireguard-go

## Known Issues
- Dns proxy server start problem udp 53 already used.
  - This is known isse for macs if you have a local dns server like `mDNSResponder` or using vpn clients like Cloudflare Warp.
  - You can try to solve by stopping `docker daemon`, or `virtual machine managers` or disabling `Internet Sharing` feature for macbooks.
  - Reference: https://developers.cloudflare.com/cloudflare-one/connections/connect-devices/warp/troubleshooting/client-errors/#cf_dns_proxy_failure
