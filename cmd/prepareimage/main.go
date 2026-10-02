package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	workflowv1alpha1 "github.com/argoproj/argo-workflows/v4/pkg/apis/workflow/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	kmmv1beta1 "github.com/rh-ecosystem-edge/kernel-module-management/api/v1beta1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	gpuev1alpha1 "github.com/ROCm/gpu-operator/api/v1alpha1"
	utils "github.com/ROCm/gpu-operator/internal"
)

const (
	DefaultRepoURL      = "https://repo.radeon.com"
	DefaultOutputFormat = OutputFormatDockerfile
)

var (
	GitCommit = "undefined"
	Version   = "undefined"
	BuildTag  = "undefined"
	scheme    = runtime.NewScheme()
)

func init() {
	// Initialize the scheme
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(gpuev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kmmv1beta1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(workflowv1alpha1.AddToScheme(scheme))
}

type Config struct {
	OutputFormat OutputFormat
	driverType   string
	osPrettyName string
	imageName    string
	buildArgs    BuildArgs
}

type BuildArgs struct {
	kernelFullVersion string
	driversVersion    string
	repoURL           string
	packageRepoURL    string
	gpgKeyURL         string
}

type OutputFormat string

const (
	OutputFormatDockerfile    OutputFormat = "dockerfile"
	OutputFormatDockerScript  OutputFormat = "docker-script"
	OutputFormatBuildahScript OutputFormat = "buildah-script"
	OutputFormatPodmanScript  OutputFormat = "podman-script"
)

func (f *OutputFormat) String() string {
	return string(*f)
}

func (f *OutputFormat) Set(val string) error {
	switch OutputFormat(val) {
	case OutputFormatDockerfile, OutputFormatDockerScript, OutputFormatBuildahScript, OutputFormatPodmanScript:
		*f = OutputFormat(val)
		return nil
	default:
		return fmt.Errorf("invalid format %q (must be dockerfile, docker-script, buildah-script, or podman-script)", val)
	}
}

func main() {
	config := deriveConfigFromFlags()

	output, err := createOutput(config)
	if err != nil {
		log.Fatalf("Failed to create output: %v", err)
	}

	_, err = fmt.Fprint(os.Stdout, output)
	if err != nil {
		log.Fatalf("Failed to print output: %v", err)
	}
}

func deriveConfigFromFlags() *Config {
	config := &Config{}

	config.OutputFormat = DefaultOutputFormat // Set the default value, because flag.Var() does not.
	flag.Var(&config.OutputFormat, "format", "The format of the output to generate. Scripts embed the Dockerfile and execute the command to build the image using the correct name. Valid values are: dockerfile, docker-script, buildah-script, podman-script.")
	flag.StringVar(&config.driverType, "driver-type", utils.DriverTypeContainer, "The type of the driver. Valid values are: container, vf-passthrough, pf-passthrough.")
	flag.StringVar(&config.osPrettyName, "os-pretty-name", "", "The 'pretty name' of the OS. See the PRETTY_NAME variable defined in /etc/os-release. Example: 'Ubuntu 24.04.5 LTS'.")
	flag.StringVar(&config.buildArgs.kernelFullVersion, "kernel-full-version", "", "The full version of the kernel. See the output of the 'uname -r' command. Example: '6.8.0-139-generic'.")
	flag.StringVar(&config.buildArgs.driversVersion, "drivers-version", "", "The version of the drivers. For supported versions, see AMD documentation.")
	flag.StringVar(&config.buildArgs.repoURL, "repo-url", DefaultRepoURL, "The URL for fetching the amdgpu installer.")
	flag.StringVar(&config.buildArgs.packageRepoURL, "package-repo-url", "", "The full URL to the package repository for driver packages. When specified, this overrides the --repo-url flag. This is useful when using custom mirrors or when repo.radeon.com changes structure. Example: 'https://custom-mirror.example.com/amdgpu/30.20.1/ubuntu jammy main'.")
	flag.StringVar(&config.buildArgs.gpgKeyURL, "gpg-key-url", "", "The full URL to the GPG key for package verification. When specified, this overrides the constrult GPG key URL. Example: 'https://custom-mirror.example.com/rocm/rocm.gpg.key'.")
	flag.StringVar(&config.imageName, "image-name", "", "The name of the image with the registry and repository path, but without the tag. Example: 'registry.example.com/project/amdgpu'. Required for docker-script, buildah-script, and podman-script output formats.")
	flag.Parse()

	return config
}
