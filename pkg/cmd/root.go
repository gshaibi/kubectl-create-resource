package cmd

import (
	"fmt"
	"os"

	"github.com/gshaibi/kubectl-create-resource/pkg/client"
	"github.com/gshaibi/kubectl-create-resource/pkg/discovery"
	"github.com/gshaibi/kubectl-create-resource/pkg/generator"
	"github.com/gshaibi/kubectl-create-resource/pkg/prompt"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/homedir"
)

var (
	kubeconfig string
	namespace  string
	listTypes  bool
	dryRun     bool
	output     string
	setValues  []string
	name       string
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
  kubectl create-resource deployment --name=my-app --dry-run -o yaml`,
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

	// Get the schema for the resource
	schema, err := k8sClient.GetResourceSchema(gvr)
	if err != nil {
		// Continue with basic schema if we can't get the full one
		fmt.Fprintf(os.Stderr, "Warning: Could not fetch full schema, using basic fields\n")
	}

	// Collect field values (from flags and/or prompts)
	values, err := prompt.CollectFieldValues(schema, name, setValues)
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
