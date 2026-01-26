
kubectl get pods -n demo-system -o jsonpath='{range .items[*]}{.spec.containers[*].image}{"\n"}{end}'