package app

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	authzapp "github.com/master-abror/zago-core/backend/internal/authorization/application"
	authzrepo "github.com/master-abror/zago-core/backend/internal/authorization/repository"
)

// Simulator dan middleware Authorize memakai Authorizer yang sama. Authorizer tanpa policy
// mengabaikan setiap policy tanpa suara, jadi perakitan menolaknya saat start (ADR-0022 butir 69h).

func bareAuthorizer(t *testing.T) *authzapp.Authorizer {
	t.Helper()
	a, err := authzapp.NewAuthorizer(authzrepo.NewEffective(nil), nil)
	require.NoError(t, err)
	require.False(t, a.PoliciesEnabled(), "prasyarat tes: authorizer ini sengaja tanpa policy")
	return a
}

func TestNewSimulatorRefusesAnUnassembledStack(t *testing.T) {
	sim, err := authzStack{}.newSimulator(nil, discardAudit{})

	require.Error(t, err)
	require.Nil(t, sim)
}

func TestNewSimulatorRefusesAnAuthorizerWithoutPolicies(t *testing.T) {
	s := authzStack{authorizer: bareAuthorizer(t)}

	sim, err := s.newSimulator(nil, discardAudit{})

	require.Error(t, err, "simulasi akan menjawab ALLOW untuk request yang ditolak policy di produksi")
	require.Nil(t, sim)
}

func TestNewSimulatorAcceptsTheAssembledStack(t *testing.T) {
	var buf bytes.Buffer
	s, err := newAuthzStack(nil, nil, jsonLogger(&buf))
	require.NoError(t, err)

	sim, err := s.newSimulator(nil, discardAudit{})

	require.NoError(t, err)
	require.NotNil(t, sim)
}

func TestNewSimulatorRefusesAMissingAuditRecorder(t *testing.T) {
	var buf bytes.Buffer
	s, err := newAuthzStack(nil, nil, jsonLogger(&buf))
	require.NoError(t, err)

	sim, err := s.newSimulator(nil, nil)

	require.Error(t, err)
	require.Nil(t, sim)
}

func TestHTTPAPIRefusesAnAuthorizerWithoutPolicies(t *testing.T) {
	var buf bytes.Buffer
	log := jsonLogger(&buf)
	s := authzStack{authorizer: bareAuthorizer(t)}

	api, err := s.httpAPI(nil, discardAudit{}, newTestKernel(t, log), log)

	require.Error(t, err, "rute otorisasi tanpa policy akan mengabaikan policy tanpa suara")
	require.Nil(t, api)
}
