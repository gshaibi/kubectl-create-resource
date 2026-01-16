package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/gshaibi/kubectl-create-resource/pkg/client"
	"github.com/gshaibi/kubectl-create-resource/pkg/discovery"
	"github.com/gshaibi/kubectl-create-resource/pkg/generator"
	"github.com/gshaibi/kubectl-create-resource/pkg/prompt"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/yaml"
)

var (
	kubeconfig   string
	namespace    string
	listTypes    bool
	dryRun       bool
	output       string
	setValues    []string
	name         string
	fromResource string
)

var rootCmd = &cobra.Command{
	Use:   "kubectl-create-resource [resource-type]",
	Short: "Create any Kubernetes resource interactively or via flags",
	Long: `kubectl-create-resource is a kubectl plugin that generalizes resource creation
to all Kubernetes resource types, including Custom Resource Definitions (CRDs).

It discovers available resource types from your cluster, fetches their schemas,
and guides you through creating resources either interactively or via command-line flags.

Examples:
  # List all available resource types
  kubectl create-resource --list

  # Create a deployment interactively
  kubectl create-resource deployment

  # Create a resource with flags
  kubectl create-resource deployment --name=my-app --set=spec.replicas=3

  # Dry-run to see the generated YAML
  kubectl create-resource deployment --name=my-app --dry-run -o yaml

  # Use an existing resource as a template
  kubectl create-resource queue --from=existing-queue --name=new-queue`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCreateResource,
}

func init() {
	// Kubeconfig flag
	if home := homedir.HomeDir(); home != "" {
		rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "",
			fmt.Sprintf("path to the kubeconfig file (default: %s/.kube/config)", home))
	} else {
		rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "",
			"path to the kubeconfig file")
	}

	// Namespace flag
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default",
		"kubernetes namespace for the resource")

	// List available resource types
	rootCmd.Flags().BoolVar(&listTypes, "list", false,
		"list all available resource types")

	// Dry-run mode
	rootCmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"only print the resource manifest without creating it")

	// Output format
	rootCmd.Flags().StringVarP(&output, "output", "o", "",
		"output format (yaml or json) - implies dry-run")

	// Set values via flags
	rootCmd.Flags().StringArrayVar(&setValues, "set", []string{},
		"set field values (e.g., --set=spec.replicas=3)")

	// Name flag for convenience
	rootCmd.Flags().StringVar(&name, "name", "",
		"name of the resource to create")

	// Template from existing resource
	rootCmd.Flags().StringVar(&fromResource, "from", "",
		"use an existing resource as a template (e.g., --from=existing-queue)")
}

func Execute() error {
	return rootCmd.Execute()
}

func runCreateResource(cmd *cobra.Command, args []string) error {
	// If output format is specified, enable dry-run
	if output != "" {
		dryRun = true
	}

	// Handle --list flag
	if listTypes {
		return listResourceTypes()
	}

	// Require a resource type argument if not listing
	if len(args) == 0 {
		return fmt.Errorf("resource type is required. Use --list to see available types")
	}

	resourceType := args[0]
	return createResource(resourceType)
}

func listResourceTypes() error {
	fmt.Fprintln(os.Stderr, "Discovering available resource types...")

	resources, err := discovery.DiscoverResourceTypes(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to discover resource types: %w", err)
	}

	fmt.Println("\nAvailable resource types:")
	fmt.Println("-------------------------")

	// Group by API group
	currentGroup := ""
	for _, r := range resources {
		if r.Group != currentGroup {
			currentGroup = r.Group
			if currentGroup == "" {
				fmt.Println("\nCore API (v1):")
			} else {
				fmt.Printf("\n%s:\n", currentGroup)
			}
		}
		fmt.Printf("  %s\n", r.Name)
	}
	return nil
}

func createResource(resourceType string) error {
	// Initialize the Kubernetes client
	k8sClient, err := client.NewK8sClient(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Resolve the resource type to GVR
	gvr, err := k8sClient.ResolveResourceType(resourceType)
	if err != nil {
		return fmt.Errorf("failed to resolve resource type %q: %w", resourceType, err)
	}

	fmt.Fprintf(os.Stderr, "Creating %s in namespace %s\n", gvr.Resource, namespace)

	// If --from is specified, use existing resource as template and open in editor
	if fromResource != "" {
		return createFromTemplate(k8sClient, gvr)
	}

	// Get the schema for the resource
	resourceSchema, err := k8sClient.GetResourceSchema(gvr)
	if err != nil {
		// Continue with basic schema if we can't get the full one
		fmt.Fprintf(os.Stderr, "Warning: Could not fetch full schema, using basic fields\n")
	}

	// Collect field values (from flags and/or prompts)
	values, err := prompt.CollectFieldValues(resourceSchema, name, setValues)
	if err != nil {
		return fmt.Errorf("failed to collect field values: %w", err)
	}

	// Generate the manifest
	manifest, err := generator.GenerateManifest(gvr, namespace, values)
	if err != nil {
		return fmt.Errorf("failed to generate manifest: %w", err)
	}

	// If dry-run, print the manifest and exit
	if dryRun {
		return generator.PrintManifest(manifest, output)
	}

	// Create the resource
	created, err := k8sClient.CreateResource(gvr, namespace, manifest)
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	fmt.Printf("%s/%s created\n", gvr.Resource, created.GetName())
	return nil
}

// createFromTemplate fetches an existing resource, opens it in an editor, and creates a new one
func createFromTemplate(k8sClient *client.K8sClient, gvr schema.GroupVersionResource) error {
	fmt.Fprintf(os.Stderr, "Using %s as template...\n", fromResource)

	// Get the full resource
	templateObj, err := k8sClient.GetResource(gvr, namespace, fromResource)
	if err != nil {
		return fmt.Errorf("failed to get template resource %q: %w", fromResource, err)
	}

	// Clean up the template for creating a new resource
	cleanedObj := cleanTemplateForCreation(templateObj, name, namespace)

	// Apply any --set values
	if len(setValues) > 0 {
		flagValues, err := prompt.ParseSetValues(setValues)
		if err != nil {
			return fmt.Errorf("failed to parse --set values: %w", err)
		}
		applySetValues(cleanedObj, flagValues)
	}

	// Convert to YAML
	yamlBytes, err := yaml.Marshal(cleanedObj.Object)
	if err != nil {
		return fmt.Errorf("failed to marshal template: %w", err)
	}

	// If dry-run, just print and exit
	if dryRun {
		fmt.Print(string(yamlBytes))
		return nil
	}

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "kubectl-create-resource-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(yamlBytes); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	tmpFile.Close()

	// Open in editor
	editor := getEditor()
	fmt.Fprintf(os.Stderr, "Opening %s in %s...\n", tmpPath, editor)

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	// Read back the edited file
	editedBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to read edited file: %w", err)
	}

	// Parse the edited YAML
	var editedObj unstructured.Unstructured
	if err := yaml.Unmarshal(editedBytes, &editedObj.Object); err != nil {
		return fmt.Errorf("failed to parse edited YAML: %w", err)
	}

	// Create the resource
	created, err := k8sClient.CreateResource(gvr, namespace, &editedObj)
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	fmt.Printf("%s/%s created\n", gvr.Resource, created.GetName())
	return nil
}

// cleanTemplateForCreation removes fields that shouldn't be copied to a new resource
func cleanTemplateForCreation(obj *unstructured.Unstructured, newName, newNamespace string) *unstructured.Unstructured {
	// Deep copy the object
	newObj := obj.DeepCopy()

	// Remove status
	delete(newObj.Object, "status")

	// Clean up metadata
	if metadata, ok := newObj.Object["metadata"].(map[string]interface{}); ok {
		// Remove server-generated fields
		delete(metadata, "uid")
		delete(metadata, "resourceVersion")
		delete(metadata, "generation")
		delete(metadata, "creationTimestamp")
		delete(metadata, "managedFields")
		delete(metadata, "selfLink")
		delete(metadata, "ownerReferences")
		delete(metadata, "finalizers")

		// Set new name if provided
		if newName != "" {
			metadata["name"] = newName
		} else {
			// Append "-copy" to the name
			if currentName, ok := metadata["name"].(string); ok {
				metadata["name"] = currentName + "-copy"
			}
		}

		// Set namespace if provided
		if newNamespace != "" {
			metadata["namespace"] = newNamespace
		}
	}

	return newObj
}

// applySetValues applies --set flag values to an unstructured object
func applySetValues(obj *unstructured.Unstructured, values map[string]interface{}) {
	for path, value := range values {
		unstructured.SetNestedField(obj.Object, value, splitPath(path)...)
	}
}

// splitPath splits a dot-notation path into parts
func splitPath(path string) []string {
	return splitPathParts(path)
}

// splitPathParts handles dot-separated paths
func splitPathParts(path string) []string {
	var parts []string
	current := ""
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(path[i])
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// getEditor returns the editor to use, from $EDITOR or defaults
func getEditor() string {
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	// Try common editors
	for _, editor := range []string{"vim", "vi", "nano"} {
		if _, err := exec.LookPath(editor); err == nil {
			return editor
		}
	}
	return "vi"
}

// The following are kept for interface compatibility but delegate to packages

func discoverResourceTypes() ([]discovery.ResourceType, error) {
	return discovery.DiscoverResourceTypes(kubeconfig)
}

func newK8sClient(kubeconfigPath string) (*client.K8sClient, error) {
	return client.NewK8sClient(kubeconfigPath)
}

func collectFieldValues(schema *client.ResourceSchema, resourceName string, setVals []string) (*prompt.CollectedValues, error) {
	return prompt.CollectFieldValues(schema, resourceName, setVals)
}

func generateManifest(gvr schema.GroupVersionResource, ns string, values *prompt.CollectedValues) (*unstructured.Unstructured, error) {
	return generator.GenerateManifest(gvr, ns, values)
}

func printManifest(manifest *unstructured.Unstructured, format string) error {
	return generator.PrintManifest(manifest, format)
}
