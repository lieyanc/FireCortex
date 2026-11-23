package managedinfra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	managedports "github.com/lieyanc/FireCortex/backend/internal/ports/managedinfra"
)

// CoreCredentials belongs to the managed data directory, independently of the
// active database connection and optional Compose export.
type CoreCredentials struct {
	User     string
	Database string
	Password string
}

func nonemptyCredential(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// PrepareCoreCredentials records credentials before a container can initialize
// its data directory. Older installations can recover them from Docker or Compose.
func PrepareCoreCredentials(ctx context.Context, client managedports.DockerClient, root, service string, explicit CoreCredentials, compose map[string]string) (CoreCredentials, error) {
	var saved CoreCredentials
	pg := service == managedports.CoreServicePostgres
	if !pg && service != managedports.CoreServiceRedis {
		return saved, fmt.Errorf("unknown core service %q", service)
	}
	path := filepath.Join(root, "."+service+"-credentials.json")
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(raw, &saved); err != nil {
			return saved, fmt.Errorf("read managed %s credentials: %w", service, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return saved, err
	}

	name := managedports.RedisContainerName
	if pg {
		name = managedports.PostgresContainerName
	}
	ctr, err := client.InspectContainer(ctx, name)
	if err != nil {
		return saved, err
	}
	var env map[string]string
	if ctr != nil {
		env = ctr.Env
	}
	result := CoreCredentials{}
	if pg {
		result.User = nonemptyCredential(explicit.User, saved.User, env["POSTGRES_USER"], compose["FIRECORTEX_POSTGRES_USER"], defaultPGUser)
		result.Database = nonemptyCredential(explicit.Database, saved.Database, env["POSTGRES_DB"], compose["FIRECORTEX_POSTGRES_DB"], defaultPGDatabase)
		result.Password = nonemptyCredential(explicit.Password, saved.Password, env["POSTGRES_PASSWORD"], compose["FIRECORTEX_POSTGRES_PASSWORD"])
	} else {
		result.Password = nonemptyCredential(explicit.Password, saved.Password, env["REDISCLI_AUTH"], compose["FIRECORTEX_REDIS_PASSWORD"])
	}
	if result.Password == "" {
		result.Password, err = GenerateStrongPassword()
		if err != nil {
			return saved, err
		}
	}
	if result == saved {
		return result, nil
	}
	return result, saveCoreCredentials(path, result)
}

func saveCoreCredentials(path string, credentials CoreCredentials) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".core-credentials-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := json.NewEncoder(tmp).Encode(credentials); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
