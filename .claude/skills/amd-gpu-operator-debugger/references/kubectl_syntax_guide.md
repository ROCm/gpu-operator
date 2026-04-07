# kubectl Command Syntax — Safe Patterns

This guide shows how to write robust kubectl commands that handle errors gracefully.

## 1. kubectl + Python JSON parsing

When generating kubectl commands that pipe JSON to Python:

1. ALWAYS add `2>/dev/null` after kubectl to suppress error messages
2. ALWAYS use `data = sys.stdin.read()` followed by `json.loads(data)` instead of `json.load(sys.stdin)`
3. ALWAYS check `if data.strip():` before attempting JSON parsing
4. ALWAYS add `2>/dev/null || echo "Failed to..."` at the end for graceful error handling

Example safe pattern:

```bash
kubectl get nodes -o json 2>/dev/null | \
  python3 -c "
import sys, json
data = sys.stdin.read()
if data.strip():
    node = json.loads(data)
    # ... process node ...
else:
    print('No data or kubectl failed')
" 2>/dev/null || echo "Failed to retrieve data"
```

## 2. kubectl with jsonpath array indexing

When using jsonpath to extract pod names or other resources:

1. NEVER use direct command substitution like `kubectl logs $(kubectl get pods ... -o jsonpath='{.items[0].metadata.name}')`
2. ALWAYS assign to a variable first with `2>/dev/null` to suppress "array index out of bounds" errors
3. ALWAYS check if the variable is non-empty with `if [ -n "$VAR" ]` before using it
4. ALWAYS provide a helpful error message when the resource is not found

**UNSAFE** (will fail with "array index out of bounds" if no pods exist):

```bash
# DON'T DO THIS:
kubectl logs -n $NS $(kubectl get pods -n $NS -l app=foo -o jsonpath='{.items[0].metadata.name}')
```

**SAFE** pattern:

```bash
POD=$(kubectl get pods -n $NS -l app=foo -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [ -n "$POD" ]; then
  kubectl logs -n $NS $POD --tail=50
else
  echo "No pods found matching label app=foo"
fi
```

## 3. kubectl with grep/awk command substitution

When using `$(kubectl get pods | grep | awk)` patterns:

1. ALWAYS check if the variable is non-empty before using it
2. Use `|| true` to prevent pipeline failures from stopping execution

Example safe pattern:

```bash
POD=$(kubectl get pods -n $NS | grep controller-manager | awk '{print $1}' | head -1)
if [ -n "$POD" ]; then
  kubectl logs -n $NS $POD --tail=50
else
  echo "No controller-manager pod found"
fi
```
