# kubectl-create-resource

A kubectl plugin that generalizes resource creation to **all** Kubernetes resource types, including Custom Resource Definitions (CRDs).

## Features

- **Universal Resource Creation**: Create any Kubernetes resource type, not just the limited set supported by `kubectl create`
- **Interactive Mode**: Wizard-style prompts guide you through required and optional fields
- **Flag Mode**: Scriptable with `--set` flags for automation
- **CRD Support**: Automatically discovers Custom Resource Definitions from your cluster
- **Schema-Aware**: Fetches OpenAPI schemas to provide field validation and helpful prompts
- **Dry-Run Support**: Preview generated manifests before applying

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/gshaibi/kubectl-create-resource.git
cd kubectl-create-resource

# Build and install
make install
```

This installs the binary to `$GOPATH/bin`. Ensure `$GOPATH/bin` is in your PATH.

### Verify Installation

```bash
kubectl create-resource --help
```

## Usage

### List Available Resource Types

Discover all resource types available in your cluster:

```bash
kubectl create-resource --list
```

### Interactive Mode

Create a resource interactively - you'll be prompted for required fields:

```bash
kubectl create-resource deployment
```

Example session:
```
Creating deployments in namespace default
? name (required) [Name of the resource]: my-app
? namespace [Namespace of the resource]: default
deployments/my-app created
```

### Flag Mode

Provide values via command-line flags for scripting:

```bash
kubectl create-resource deployment \
  --name=my-app \
  --namespace=production \
  --set=spec.replicas=3 \
  --set=spec.selector.matchLabels.app=my-app \
  --set=spec.template.metadata.labels.app=my-app \
  --set=spec.template.spec.containers[0].name=app \
  --set=spec.template.spec.containers[0].image=nginx:latest
```

### Mixed Mode

Use flags for known values and get prompted for the rest:

```bash
kubectl create-resource configmap --name=my-config
# You'll be prompted for data fields
```

### Dry-Run Mode

Preview the generated manifest without creating the resource:

```bash
kubectl create-resource deployment --name=my-app --dry-run -o yaml
```

Output:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: default
```

### Working with CRDs

Create custom resources the same way as built-in resources:

```bash
# List to see your CRDs
kubectl create-resource --list

# Create a custom resource
kubectl create-resource myresource.example.com --name=my-instance
```

## Command Reference

```
kubectl create-resource [resource-type] [flags]

Flags:
      --dry-run           Only print the resource manifest without creating it
  -h, --help              Help for kubectl-create-resource
      --kubeconfig string Path to the kubeconfig file
      --list              List all available resource types
      --name string       Name of the resource to create
  -n, --namespace string  Kubernetes namespace for the resource (default "default")
  -o, --output string     Output format (yaml or json) - implies dry-run
      --set stringArray   Set field values (e.g., --set=spec.replicas=3)
```

## Examples

### Create a ConfigMap

```bash
kubectl create-resource configmap \
  --name=app-config \
  --set=data.DATABASE_URL=postgres://localhost:5432/mydb \
  --set=data.LOG_LEVEL=info
```

### Create a Secret

```bash
kubectl create-resource secret \
  --name=app-secrets \
  --set=type=Opaque \
  --set=stringData.API_KEY=my-secret-key
```

### Create a Service

```bash
kubectl create-resource service \
  --name=my-service \
  --set=spec.selector.app=my-app \
  --set=spec.ports[0].port=80 \
  --set=spec.ports[0].targetPort=8080
```

### Create a Namespace

```bash
kubectl create-resource namespace --name=my-namespace
```

### Create a ServiceAccount

```bash
kubectl create-resource serviceaccount --name=my-sa --namespace=kube-system
```

## How It Works

1. **Discovery**: Queries the Kubernetes API to discover all available resource types, including CRDs
2. **Schema Fetching**: Retrieves the OpenAPI schema for the selected resource type
3. **Field Collection**: Collects values either from `--set` flags or interactive prompts
4. **Manifest Generation**: Builds an unstructured Kubernetes manifest
5. **Creation**: Applies the manifest to the cluster using the dynamic client

## Development

### Building

```bash
make build
```

### Running Tests

```bash
make test
```

### Cross-Platform Builds

```bash
make build-all
```

## Requirements

- Go 1.21 or later
- kubectl configured with cluster access
- Kubernetes cluster (for runtime functionality)

## License

MIT License

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
