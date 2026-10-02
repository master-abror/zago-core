package infra_test

import "testing"

// testWriter mengarahkan keluaran log ke t.Log.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}
