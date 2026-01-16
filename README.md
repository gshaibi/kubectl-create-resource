# kubectl-create-resource

A kubectl plugin that generalizes resource creation to **all** Kubernetes resource types, including Custom Resource Definitions (CRDs).

## Features

- **Universal Resource Creation**: Create any Kubernetes resource type, not just the limited set supported by `kubectl create`
- **Template Mode**: Use existing resources as templates - opens in your editor for easy modification
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

### Template Mode (Recommended for Complex Resources)

Use an existing resource as a template - the manifest opens in your editor:

```bash
# Use existing queue as template
kubectl create-resource queue --from=existing-queue --name=my-new-queue

# Use existing deployment as template
kubectl create-resource deployment --from=my-deployment -n my-namespace --name=new-deployment

# Preview the template without opening editor
kubectl create-resource deployment --from=my-deployment --dry-run
```

The editor used is determined by (in order):
1. `$EDITOR` environment variable
2. `$VISUAL` environment variable  
3. `vim`, `vi`, or `nano` (whichever is available)

When using `--from`:
- Server-generated fields are automatically removed (uid, resourceVersion, status, etc.)
- The new name is set (or "-copy" is appended if no name provided)
- You can edit the full YAML before creation

### Interactive Mode

Create a resource interactively - you'll be prompted for fields:

```bash
kubectl create-resource deployment
```

Example session:
```
Creating deployments in namespace default
  Name of the resource
metadata.name *: my-app
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
  --set='spec.template.spec.containers[0].name=app' \
  --set='spec.template.spec.containers[0].image=nginx:latest'
```

**Note**: Quote values containing brackets to prevent shell glob expansion.

### Mixed Mode

Combine `--from` with `--set` to pre-modify specific fields:

```bash
kubectl create-resource queue \
  --from=existing-queue \
  --name=new-queue \
  --set='spec.resources.cpu.quota=500'
```

### Dry-Run Mode

Preview the generated manifest without creating the resource:

```bash
kubectl create-resource deployment --name=my-app --dry-run -o yaml
kubectl create-resource queue --from=existing-queue --dry-run
```

### Working with CRDs

Create custom resources the same way as built-in resources:

```bash
# List to see your CRDs
kubectl create-resource --list

# Create using template from existing resource (recommended)
kubectl create-resource queue --from=existing-queue --name=my-queue

# Create with flags
kubectl create-resource myresource.example.com \
  --name=my-instance \
  --set=spec.someField=value
```

**Note on CRDs**: Some CRDs have minimal OpenAPI schemas but strict admission webhooks. If interactive mode doesn't prompt for required fields, use `--from` (template mode) or `--set` flags.

## Command Reference

```
kubectl create-resource [resource-type] [flags]

Flags:
      --dry-run             Only print the resource manifest without creating it
      --from string         Use an existing resource as a template (opens in editor)
  -h, --help                Help for kubectl-create-resource
      --kubeconfig string   Path to the kubeconfig file
      --list                List all available resource types
      --name string         Name of the resource to create
  -n, --namespace string    Kubernetes namespace for the resource (default "default")
  -o, --output string       Output format (yaml or json) - implies dry-run
      --set stringArray     Set field values (e.g., --set=spec.replicas=3)
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
  --set='spec.ports[0].port=80' \
  --set='spec.ports[0].targetPort=8080'
```

### Clone a Deployment

```bash
# Clone with new name
kubectl create-resource deployment --from=nginx --name=nginx-clone

# Clone to different namespace
kubectl create-resource deployment --from=nginx -n production --name=nginx-prod
```

### Create a CRD Instance from Template

```bash
# Clone an existing Queue (Run:AI/Kueue)
kubectl create-resource queue --from=default-queue --name=my-team-queue

# Preview first
kubectl create-resource queue --from=default-queue --name=my-team-queue --dry-run
```

## How It Works

1. **Discovery**: Queries the Kubernetes API to discover all available resource types, including CRDs
2. **Template Fetch** (if `--from`): Fetches existing resource, cleans server-generated fields
3. **Schema Fetching**: Retrieves the OpenAPI schema for the selected resource type
4. **Editor/Prompts**: Opens editor for templates, or prompts for fields interactively
5. **Manifest Generation**: Builds an unstructured Kubernetes manifest
6. **Creation**: Applies the manifest to the cluster using the dynamic client

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
