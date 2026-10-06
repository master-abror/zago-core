package testpg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	image         = "postgres:18"
	superUser     = "postgres"
	superPassword = "postgres" // HANYA container tes
)

// repoRoot menunjuk akar repo (testpg ada di backend/internal/testpg).
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// startInstance menyalakan postgres:18 dengan init script role yang sama dengan compose dev.
func startInstance(t *testing.T) (*instance, error) {
	t.Helper()
	if os.Getenv("REQUIRE_DOCKER") != "1" {
		testcontainers.SkipIfProviderIsNotHealthy(t)
	}
	ctx := context.Background()
	initDir := filepath.Join(repoRoot(), "deploy", "db", "init")

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        image,
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "platform",
				"POSTGRES_USER":     superUser,
				"POSTGRES_PASSWORD": superPassword,
			},
			Files: []testcontainers.ContainerFile{
				{HostFilePath: filepath.Join(initDir, "00-roles.sql"), ContainerFilePath: "/docker-entrypoint-initdb.d/00-roles.sql", FileMode: 0o644},
				{HostFilePath: filepath.Join(initDir, "01-dev-privileges.sql"), ContainerFilePath: "/docker-entrypoint-initdb.d/01-dev-privileges.sql", FileMode: 0o644},
			},
			// "ready to accept connections" muncul dua kali bila ada init script (server sementara, lalu final).
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, fmt.Errorf("gagal menyalakan %s (Docker berjalan?): %w", image, err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		_ = testcontainers.TerminateContainer(c)
		return nil, err
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		_ = testcontainers.TerminateContainer(c)
		return nil, err
	}
	return &instance{
		hostPort:  host + ":" + port.Port(),
		terminate: func() { _ = testcontainers.TerminateContainer(c) },
	}, nil
}
