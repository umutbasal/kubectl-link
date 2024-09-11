# kubectl-link

## misc

```sh
sudo go run . -device utun123 -interface en9
sudo ifconfig utun123 198.18.0.1 198.18.0.1 up
sudo route add -net 208.79.209.138/24 198.18.0.1
kubectl get pods --all-namespaces --field-selector=status.podIP=172.0.0.1
```
