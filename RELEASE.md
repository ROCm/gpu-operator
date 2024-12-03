# Release Helm Chart Repository

```bash
# Build Helm Charts from main/master branch
cd helm-charts; helm dependency update; helm lint; cd ..; helm package helm-charts/
git checkout gh-pages
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
