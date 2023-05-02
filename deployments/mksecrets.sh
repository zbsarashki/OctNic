kubectl create secret docker-registry local-registry -n default \
    --docker-server=registry.local:9001 --docker-username=admin \
    --docker-password='Li69nux*' \
    --docker-email=noreply@registry.local
kubectl patch serviceaccount default -p '{"imagePullSecrets": [{"name": "local-registry"}]}'
