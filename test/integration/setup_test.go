package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
)

var (
	client     *s3.Client
	serverCmd  *exec.Cmd
	serverAddr string
	dataDir    string
)

// TestMain sets up the integration test environment.
func TestMain(m *testing.M) {
	ctx := context.Background()

	// Find or build the binary
	binPath, err := findOrBuildBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to find or build binary: %v\n", err)
		os.Exit(1)
	}

	// Create a temp data directory
	dataDir, err = os.MkdirTemp("", "home-store-integration-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp data directory: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dataDir)

	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to find available port: %v\n", err)
		os.Exit(1)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	serverAddr = fmt.Sprintf("127.0.0.1:%d", port)

	// Start the server
	serverCmd = exec.Command(binPath, "-addr", serverAddr, "-data-dir", dataDir)
	serverCmd.Stdout = os.Stdout
	serverCmd.Stderr = os.Stderr
	if err := serverCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
		os.Exit(1)
	}

	// Wait for server to be ready
	if err := waitForServer(serverAddr, 10*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "Server failed to start: %v\n", err)
		serverCmd.Process.Kill()
		os.Exit(1)
	}

	// Create AWS S3 client
	client, err = createS3Client(ctx, serverAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create S3 client: %v\n", err)
		serverCmd.Process.Kill()
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Shut down the server
	if serverCmd.Process != nil {
		serverCmd.Process.Signal(os.Interrupt)
		serverCmd.Wait()
	}

	os.Exit(code)
}

// findOrBuildBinary finds the binary from HOME_STORE_BIN env var or builds it.
func findOrBuildBinary() (string, error) {
	// Check environment variable first
	if binPath := os.Getenv("HOME_STORE_BIN"); binPath != "" {
		if _, err := os.Stat(binPath); err == nil {
			return binPath, nil
		}
	}

	// Try to find binary in ../bin/
	workspaceRoot := findWorkspaceRoot()
	binPath := filepath.Join(workspaceRoot, "bin", "home-store")
	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}

	// Build the binary
	fmt.Println("Building home-store binary...")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/home-store")
	cmd.Dir = workspaceRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build binary: %w\nOutput: %s", err, output)
	}

	return binPath, nil
}

// findWorkspaceRoot finds the root of the workspace.
func findWorkspaceRoot() string {
	// Start from current directory and go up until we find go.mod
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			// Check if this is the main go.mod (not test/integration/go.mod)
			content, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err == nil && !contains(string(content), "test/integration") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fallback: assume we're in test/integration
	return filepath.Join("..", "..")
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// waitForServer waits for the server to be ready.
func waitForServer(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()
			// Give server a moment to fully initialize
			time.Sleep(100 * time.Millisecond)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for server at %s", addr)
}

// createS3Client creates an AWS S3 client configured for the local server.
func createS3Client(ctx context.Context, addr string) (*s3.Client, error) {
	// Parse address to get host and port
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	// Use path-style addressing for local server
	endpoint := fmt.Sprintf("http://%s:%s", host, port)

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			"test-access-key",
			"test-secret-key",
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
		o.EndpointOptions.DisableHTTPS = true
	})

	return client, nil
}

// generateBucketName generates a unique bucket name for testing.
func generateBucketName(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().Unix(), os.Getpid())
}

// cleanupBucket deletes a bucket and ignores errors (for test cleanup).
func cleanupBucket(ctx context.Context, bucketName string) {
	if client == nil {
		return
	}
	// Try to delete the bucket, ignore errors
	client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
}

// TestServer verifies that the server started and client is initialized.
func TestServer(t *testing.T) {
	require.NotNil(t, client, "client should be initialized")
	require.NotEmpty(t, serverAddr, "server address should be set")
}
