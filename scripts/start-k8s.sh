docker build -t kraken-backend:latest ../backend
docker build -t kraken-frontend:latest ../frontend

kubectl apply -k ../k8s
kubectl rollout status deployment kraken-backend -n kraken
kubectl rollout status deployment kraken-frontend -n kraken
kubectl -n kraken get pods -w

