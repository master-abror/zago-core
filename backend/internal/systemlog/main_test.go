package systemlog_test

import (
	"os"
	"testing"

	"github.com/master-abror/zago-core/backend/internal/testkit"
)

func TestMain(m *testing.M) { os.Exit(testkit.Run(m)) }
