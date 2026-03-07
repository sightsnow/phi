package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var supportedTargets = []string{
	"linux/amd64",
	"linux/arm64",
	"windows/amd64",
	"windows/arm64",
}

var supportedTargetSet = map[string]struct{}{
	"linux/amd64":   {},
	"linux/arm64":   {},
	"windows/amd64": {},
	"windows/arm64": {},
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "build error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	rootDir, err := repoRoot()
	if err != nil {
		return err
	}

	defaultOutputDir := filepath.Join("dist", "bin")

	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outputDir := fs.String("output", defaultOutputDir, "output directory")
	fs.Usage = func() {
		printUsage(fs, defaultOutputDir)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	targets := fs.Args()
	if len(targets) == 0 {
		targets = append([]string(nil), supportedTargets...)
	}

	resolvedOutputDir := *outputDir
	if !filepath.IsAbs(resolvedOutputDir) {
		resolvedOutputDir = filepath.Join(rootDir, resolvedOutputDir)
	}

	if err := os.MkdirAll(resolvedOutputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	version := detectVersion(rootDir)
	fmt.Printf("Building phi %s\n", version)
	fmt.Printf("Output: %s\n\n", displayPath(rootDir, resolvedOutputDir))

	for index, target := range targets {
		outputPath, err := buildTarget(rootDir, resolvedOutputDir, version, target)
		if err != nil {
			return err
		}
		fmt.Printf("[%d/%d] %s -> %s\n", index+1, len(targets), target, displayPath(rootDir, outputPath))
	}

	fmt.Printf("\nDone: %d target(s)\n", len(targets))
	return nil
}

func buildTarget(rootDir, outputDir, version, target string) (string, error) {
	target = strings.TrimSpace(target)
	if _, ok := supportedTargetSet[target]; !ok {
		return "", fmt.Errorf("unsupported target %q, supported: %s", target, strings.Join(supportedTargets, ", "))
	}

	parts := strings.Split(target, "/")
	goos, goarch := parts[0], parts[1]
	outputPath := filepath.Join(outputDir, outputName(goos, goarch))

	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-X phi/internal/buildinfo.Version="+version, "-o", outputPath, "./cmd/phi")
	cmd.Dir = rootDir
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+goos,
		"GOARCH="+goarch,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build %s: %w", target, err)
	}

	return outputPath, nil
}

func outputName(goos, goarch string) string {
	name := fmt.Sprintf("phi-%s-%s", goos, goarch)
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

func displayPath(rootDir, value string) string {
	rel, err := filepath.Rel(rootDir, value)
	if err != nil {
		return value
	}
	if rel == "." {
		return "."
	}
	if strings.HasPrefix(rel, "..") {
		return value
	}
	return rel
}

func detectVersion(rootDir string) string {
	if value := strings.TrimSpace(os.Getenv("PHI_VERSION")); value != "" {
		return value
	}
	cmd := exec.Command("git", "describe", "--tags", "--always", "--dirty")
	cmd.Dir = rootDir
	output, err := cmd.Output()
	if err != nil {
		return "dev"
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "dev"
	}
	return value
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve script path failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

func printUsage(fs *flag.FlagSet, defaultOutputDir string) {
	out := fs.Output()
	fmt.Fprintln(out, "Build phi binaries.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  go run scripts/build/build.go [flags] [target ...]")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Targets: %s (default: all)\n", strings.Join(supportedTargets, ", "))
	fmt.Fprintf(out, "Output:  %s\n", defaultOutputDir)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Examples:")
	fmt.Fprintln(out, "  go run scripts/build/build.go")
	fmt.Fprintln(out, "  go run scripts/build/build.go windows/amd64")
	fmt.Fprintln(out, "  go run scripts/build/build.go -output ./dist/custom windows/arm64")
	fmt.Fprintln(out)
	fs.PrintDefaults()
}
