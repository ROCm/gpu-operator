# Release Helm Chart Repository

```bash
# Build Helm Charts from a release branch branch
cd helm-charts-k8s; helm dependency update; helm lint; cd ..; helm package helm-charts-k8s/
git checkout -- helm-charts-k8s/; git checkout gh-pages
mv gpu-operator*.tgz ./charts/

# Update the index.yml
helm repo index . --url https://rocm.github.io/gpu-operator

# Release
git add ./charts
git add index.yaml
git commit -m 'Release version XXX'

# deploy the new GitHub page
git push 
```
